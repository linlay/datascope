package store

import (
	"fmt"
	"io"
	"strings"

	sql "github.com/rqlite/sql"
)

type maskingSource struct {
	alias   string
	columns []maskedColumn
}
type maskingAnalyzer struct{ store *Store }

// prepareMasking validates lineage before execution and expands wildcards from
// the registered snapshot. Unsupported SQL never falls back to unmasked output.
func (s *Store) prepareMasking(query string) (string, []maskedColumn, error) {
	if !s.hasMasking() {
		return query, nil, nil
	}
	p := sql.NewParser(strings.NewReader(query))
	stmt, err := p.ParseStatement()
	if err != nil {
		return "", nil, fmt.Errorf("无法分析查询脱敏：%w", err)
	}
	if next, end := p.ParseStatement(); next != nil || end != io.EOF {
		return "", nil, fmt.Errorf("脱敏查询只支持单条 SELECT")
	}
	sel, ok := stmt.(*sql.SelectStatement)
	if !ok {
		return "", nil, fmt.Errorf("脱敏查询只支持 SELECT")
	}
	plan, err := (maskingAnalyzer{s}).selectColumns(sel, nil)
	if err != nil {
		return "", nil, fmt.Errorf("查询脱敏拒绝：%w", err)
	}
	for _, c := range plan {
		if err := c.rule.Validate(); err != nil {
			return "", nil, err
		}
		if c.rule != nil && c.rule.Type == "hmac" && len(s.MaskingKey) < 32 {
			return "", nil, fmt.Errorf("哈希脱敏密钥不可用")
		}
	}
	return sel.String(), plan, nil
}

func (a maskingAnalyzer) selectColumns(sel *sql.SelectStatement, inherited map[string][]maskedColumn) ([]maskedColumn, error) {
	if sel.Compound != nil || sel.Values.IsValid() {
		return nil, fmt.Errorf("暂不支持 UNION / INTERSECT / EXCEPT / VALUES，请拆分查询")
	}
	ctes := map[string][]maskedColumn{}
	for k, v := range inherited {
		ctes[k] = v
	}
	if sel.WithClause != nil {
		if sel.WithClause.Recursive.IsValid() {
			return nil, fmt.Errorf("暂不支持递归 CTE")
		}
		for _, cte := range sel.WithClause.CTEs {
			cols, err := a.selectColumns(cte.Select, ctes)
			if err != nil {
				return nil, err
			}
			if len(cte.Columns) > 0 {
				if len(cols) != len(cte.Columns) {
					return nil, fmt.Errorf("CTE 字段数量不一致")
				}
				for i := range cols {
					cols[i].name = sql.IdentName(cte.Columns[i])
				}
			}
			ctes[strings.ToLower(sql.IdentName(cte.TableName))] = cols
		}
	}
	sources, err := a.sources(sel.Source, ctes)
	if err != nil {
		return nil, err
	}
	out := []maskedColumn{}
	expanded := []*sql.ResultColumn{}
	for _, col := range sel.Columns {
		qualifier, star := "", col.Star.IsValid()
		if ref, ok := col.Expr.(*sql.QualifiedRef); ok && ref.Star.IsValid() {
			qualifier = sql.IdentName(ref.Table)
			star = true
		}
		if star {
			found := false
			for _, source := range sources {
				if qualifier != "" && !strings.EqualFold(qualifier, source.alias) {
					continue
				}
				found = true
				for _, c := range source.columns {
					out = append(out, c)
					var ref sql.Expr = &sql.QualifiedRef{Table: quoted(source.alias), Column: quoted(c.name)}
					if source.alias == "" {
						ref = quoted(c.name)
					}
					expanded = append(expanded, &sql.ResultColumn{Expr: ref, Alias: quoted(c.name)})
				}
			}
			if !found {
				return nil, fmt.Errorf("无法解析通配符来源")
			}
			continue
		}
		c, direct, err := resolveMaskingRef(col.Expr, sources)
		if err != nil {
			return nil, err
		}
		if !direct {
			if _, err := sql.Walk(maskingExprVisitor{sources}, col.Expr); err != nil {
				return nil, err
			}
			c = maskedColumn{name: col.Expr.String()}
		}
		if col.Alias != nil {
			c.name = sql.IdentName(col.Alias)
		}
		out = append(out, c)
		expanded = append(expanded, col)
	}
	seen := map[string]bool{}
	for _, c := range out {
		k := strings.ToLower(c.name)
		if seen[k] {
			return nil, fmt.Errorf("结果字段 %s 重复，请使用不同别名", c.name)
		}
		seen[k] = true
	}
	sel.Columns = expanded
	return out, nil
}

func quoted(name string) *sql.Ident { return &sql.Ident{Name: name, Quoted: true} }

func (a maskingAnalyzer) sources(node sql.Source, ctes map[string][]maskedColumn) ([]maskingSource, error) {
	switch n := node.(type) {
	case nil:
		return nil, nil
	case *sql.QualifiedTableName:
		name := sql.IdentName(n.Name)
		if n.Schema == nil {
			if cols, ok := ctes[strings.ToLower(name)]; ok {
				return []maskingSource{{n.TableName(), cols}}, nil
			}
		}
		if n.Schema != nil && !strings.EqualFold(sql.IdentName(n.Schema), "main") && (a.store.engine == nil || !strings.EqualFold(sql.IdentName(n.Schema), a.store.engine.Schema)) {
			return nil, fmt.Errorf("不能分析跨 schema 字段")
		}
		for _, t := range a.store.RegisteredSchema {
			if strings.EqualFold(t.Name, name) {
				cols := []maskedColumn{}
				for _, c := range t.Columns {
					cols = append(cols, maskedColumn{c.Name, c.Masking, t.Name + "\x00" + c.Name})
				}
				return []maskingSource{{n.TableName(), cols}}, nil
			}
		}
		return nil, fmt.Errorf("表 %s 未登记，请先确认结构", name)
	case *sql.SelectStatement:
		cols, err := a.selectColumns(n, ctes)
		return []maskingSource{{"", cols}}, err
	case *sql.ParenSource:
		sources, err := a.sources(n.X, ctes)
		if err != nil {
			return nil, err
		}
		if n.Alias != nil {
			if len(sources) != 1 {
				return nil, fmt.Errorf("暂不支持带别名的复合 JOIN")
			}
			sources[0].alias = sql.IdentName(n.Alias)
		}
		return sources, nil
	case *sql.JoinClause:
		if n.Operator.Natural.IsValid() {
			return nil, fmt.Errorf("请使用显式 JOIN ON，暂不支持 NATURAL JOIN")
		}
		if _, ok := n.Constraint.(*sql.UsingConstraint); ok {
			return nil, fmt.Errorf("请使用显式 JOIN ON，暂不支持 JOIN USING")
		}
		left, err := a.sources(n.X, ctes)
		if err != nil {
			return nil, err
		}
		right, err := a.sources(n.Y, ctes)
		return append(left, right...), err
	default:
		return nil, fmt.Errorf("暂不支持此查询来源")
	}
}

func resolveMaskingRef(expr sql.Expr, sources []maskingSource) (maskedColumn, bool, error) {
	var table, name string
	switch e := expr.(type) {
	case *sql.Ident:
		name = e.Name
	case *sql.QualifiedRef:
		if e.Star.IsValid() {
			return maskedColumn{}, false, fmt.Errorf("表达式不支持字段通配符")
		}
		table = sql.IdentName(e.Table)
		name = sql.IdentName(e.Column)
	default:
		return maskedColumn{}, false, nil
	}
	var found *maskedColumn
	for _, s := range sources {
		if table != "" && !strings.EqualFold(table, s.alias) {
			continue
		}
		for _, c := range s.columns {
			if strings.EqualFold(name, c.name) {
				if found != nil {
					return maskedColumn{}, false, fmt.Errorf("字段 %s 来源不明确", name)
				}
				copy := c
				found = &copy
			}
		}
	}
	if found == nil {
		return maskedColumn{}, false, fmt.Errorf("无法解析字段 %s 的来源", name)
	}
	return *found, true, nil
}

// Only expression references are visited; function/type/collation names are
// identifiers too, but must never be mistaken for result-column references.
type maskingExprVisitor struct{ sources []maskingSource }

func (v maskingExprVisitor) Visit(n sql.Node) (sql.Visitor, sql.Node, error) {
	switch x := n.(type) {
	case *sql.Ident, *sql.QualifiedRef:
		c, _, err := resolveMaskingRef(n.(sql.Expr), v.sources)
		if err == nil && c.rule.Active() {
			err = fmt.Errorf("表达式涉及脱敏字段 %s，请直接查询该字段", c.name)
		}
		return nil, n, err
	case *sql.SelectStatement:
		return nil, n, fmt.Errorf("暂不支持结果表达式中的子查询，请改用 FROM 子查询或 CTE")
	case *sql.Type:
		return nil, n, nil
	case *sql.CollateExpr:
		_, err := sql.Walk(v, x.X)
		return nil, n, err
	case *sql.Call:
		// Named windows can refer to sensitive fields outside this expression.
		if x.Over != nil {
			return nil, n, fmt.Errorf("暂不支持窗口函数")
		}
		for _, arg := range x.Args {
			if _, err := sql.Walk(v, arg); err != nil {
				return nil, n, err
			}
		}
		if x.Filter != nil {
			if _, err := sql.Walk(v, x.Filter); err != nil {
				return nil, n, err
			}
		}
		return nil, n, nil
	}
	return v, n, nil
}
func (v maskingExprVisitor) VisitEnd(n sql.Node) (sql.Node, error) { return n, nil }
