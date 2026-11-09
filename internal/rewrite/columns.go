package rewrite

import (
	"fmt"
	sql "github.com/rqlite/sql"
	"maps"
	"strings"
)

type columnSource struct {
	alias   string
	columns []string
}
type columnChecker struct{ r *Rewriter }

// validateColumns checks every expression, including predicates and correlated
// subqueries, against the policy's projected columns before any driver call.
func (r *Rewriter) validateColumns(sel *sql.SelectStatement) error {
	if r.Columns == nil {
		return nil
	}
	_, err := (columnChecker{r}).selectColumns(sel, nil, nil)
	return err
}
func (c columnChecker) selectColumns(sel *sql.SelectStatement, inherited map[string][]string, outer []columnSource) ([]string, error) {
	ctes := maps.Clone(inherited)
	if ctes == nil {
		ctes = map[string][]string{}
	}
	if sel.WithClause != nil {
		if sel.WithClause.Recursive.IsValid() {
			return nil, deny("暂不支持递归 CTE")
		}
		for _, cte := range sel.WithClause.CTEs {
			cols, err := c.selectColumns(cte.Select, ctes, outer)
			if err != nil {
				return nil, err
			}
			if len(cte.Columns) > 0 {
				if len(cols) != len(cte.Columns) {
					return nil, deny("CTE 字段数量不匹配")
				}
				cols = nil
				for _, col := range cte.Columns {
					cols = append(cols, col.Name)
				}
			}
			ctes[strings.ToLower(cte.TableName.Name)] = cols
		}
	}
	sources, err := c.sources(sel.Source, ctes, outer)
	if err != nil {
		return nil, err
	}
	local := sources
	sources = append(append([]columnSource{}, sources...), outer...)
	out := []string{}
	for _, col := range sel.Columns {
		qualifier := ""
		star := col.Star.IsValid()
		if ref, ok := col.Expr.(*sql.QualifiedRef); ok && ref.Star.IsValid() {
			star = true
			qualifier = sql.IdentName(ref.Table)
		}
		if star {
			found := false
			for _, s := range local {
				if qualifier == "" || strings.EqualFold(s.alias, qualifier) {
					out = append(out, s.columns...)
					found = true
				}
			}
			if !found {
				return nil, deny("未知通配符来源 %s", qualifier)
			}
			continue
		}
		if err := c.expression(col.Expr, sources, ctes); err != nil {
			return nil, err
		}
		name := col.Expr.String()
		switch x := col.Expr.(type) {
		case *sql.Ident:
			name = x.Name
		case *sql.QualifiedRef:
			name = sql.IdentName(x.Column)
		}
		if col.Alias != nil {
			name = col.Alias.Name
		}
		out = append(out, name)
	}
	if err := c.expression(sel.WhereExpr, sources, ctes); err != nil {
		return nil, err
	}
	// SQL output aliases are valid in GROUP BY, HAVING and ORDER BY. Real source
	// columns remain subject to the same per-table field whitelist.
	named := append([]columnSource{{columns: out}}, sources...)
	for _, expr := range sel.GroupByExprs {
		if err := c.expression(expr, named, ctes); err != nil {
			return nil, err
		}
	}
	if err := c.expression(sel.HavingExpr, named, ctes); err != nil {
		return nil, err
	}
	for _, term := range sel.OrderingTerms {
		if err := c.expression(term.X, named, ctes); err != nil {
			return nil, err
		}
	}
	if sel.Compound != nil {
		cols, err := c.selectColumns(sel.Compound, ctes, outer)
		if err != nil {
			return nil, err
		}
		if len(cols) != len(out) {
			return nil, deny("UNION 字段数量不匹配")
		}
	}
	return out, nil
}
func (c columnChecker) sources(node sql.Source, ctes map[string][]string, outer []columnSource) ([]columnSource, error) {
	switch x := node.(type) {
	case nil:
		return nil, nil
	case *sql.QualifiedTableName:
		name := strings.ToLower(x.Name.Name)
		if x.Schema == nil {
			if cols, ok := ctes[name]; ok {
				return []columnSource{{x.TableName(), cols}}, nil
			}
		}
		if x.Schema != nil {
			name = strings.ToLower(x.Schema.Name) + "." + name
		} else if c.r.AllowQualified {
			name = strings.ToLower(c.r.DefaultSchema) + "." + name
		}
		cols, ok := c.r.Columns[name]
		if !ok {
			return nil, deny("未知或未登记表 %s", name)
		}
		return []columnSource{{x.TableName(), cols}}, nil
	case *sql.SelectStatement:
		cols, err := c.selectColumns(x, ctes, outer)
		return []columnSource{{columns: cols}}, err
	case *sql.ParenSource:
		sources, err := c.sources(x.X, ctes, outer)
		if err != nil {
			return nil, err
		}
		if x.Alias != nil {
			if len(sources) != 1 {
				return nil, deny("不支持带别名的复合 JOIN")
			}
			sources[0].alias = x.Alias.Name
		}
		return sources, nil
	case *sql.JoinClause:
		left, err := c.sources(x.X, ctes, outer)
		if err != nil {
			return nil, err
		}
		right, err := c.sources(x.Y, ctes, outer)
		if err != nil {
			return nil, err
		}
		all := append(left, right...)
		switch constraint := x.Constraint.(type) {
		case *sql.OnConstraint:
			if err := c.expression(constraint.X, append(append([]columnSource{}, all...), outer...), ctes); err != nil {
				return nil, err
			}
		case *sql.UsingConstraint:
			for _, name := range constraint.Columns {
				for _, side := range [][]columnSource{left, right} {
					if err := c.expression(name, side, ctes); err != nil {
						return nil, err
					}
				}
			}
		}
		return all, nil
	default:
		return nil, deny("不支持的查询来源 %T", node)
	}
}
func (c columnChecker) expression(expr sql.Expr, sources []columnSource, ctes map[string][]string) error {
	if expr == nil {
		return nil
	}
	_, err := sql.Walk(columnExprVisitor{c, sources, ctes}, expr)
	return err
}

type columnExprVisitor struct {
	checker columnChecker
	sources []columnSource
	ctes    map[string][]string
}

func (v columnExprVisitor) Visit(n sql.Node) (sql.Visitor, sql.Node, error) {
	switch x := n.(type) {
	case *sql.Ident, *sql.QualifiedRef:
		name, alias := "", ""
		switch ref := n.(type) {
		case *sql.Ident:
			name = ref.Name
		case *sql.QualifiedRef:
			name = sql.IdentName(ref.Column)
			alias = sql.IdentName(ref.Table)
			if ref.Schema != nil {
				return nil, n, deny("请使用表别名引用字段")
			}
		}
		for _, source := range v.sources {
			if alias != "" && !strings.EqualFold(source.alias, alias) {
				continue
			}
			for _, col := range source.columns {
				if strings.EqualFold(name, col) {
					return nil, n, nil
				}
			}
		}
		return nil, n, deny("字段 %s 不存在或未开放", n.String())
	case *sql.SelectStatement:
		_, err := v.checker.selectColumns(x, v.ctes, v.sources)
		return nil, n, err
	case *sql.Call:
		for _, arg := range x.Args {
			if err := v.checker.expression(arg, v.sources, v.ctes); err != nil {
				return nil, n, err
			}
		}
		if x.Filter != nil {
			if _, err := sql.Walk(v, x.Filter); err != nil {
				return nil, n, err
			}
		}
		if x.Over != nil {
			return nil, n, fmt.Errorf("暂不支持窗口函数")
		}
		return nil, n, nil
	case *sql.Type:
		return nil, n, nil
	case *sql.CollateExpr:
		return nil, n, v.checker.expression(x.X, v.sources, v.ctes)
	}
	return v, n, nil
}
func (v columnExprVisitor) VisitEnd(n sql.Node) (sql.Node, error) { return n, nil }
