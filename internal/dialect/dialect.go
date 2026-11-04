// Package dialect compiles the documented SELECT subset to a target database.
// It deliberately does not accept arbitrary vendor SQL. All output is generated
// from a fully parsed AST; comments and original SQL are never sent to a server.
package dialect

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	parser "github.com/rqlite/sql"
)

type Engine struct {
	Driver string
	Schema string
	// Logical names are case-insensitive and must be unique within one schema.
	Tables map[string]string
}

func (e Engine) Quote(name string) string {
	q := `"`
	if e.Driver == "mysql" {
		q = "`"
	}
	return q + strings.ReplaceAll(name, q, q+q) + q
}

func (e Engine) Table(name string) string { return e.Quote(e.Schema) + "." + e.Quote(name) }

func (e Engine) fold(name string) string {
	switch e.Driver {
	case "oracle", "dameng":
		return strings.ToUpper(name)
	case "postgresql":
		return strings.ToLower(name)
	default:
		return name
	}
}

var safeFunctions = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true,
	"ABS": true, "ROUND": true, "LOWER": true, "UPPER": true, "LENGTH": true,
	"COALESCE": true, "NULLIF": true, "SUBSTR": true,
}

// Compile also checks raw queries. Named bindings are resolved in SQL occurrence
// order (including repeats), independently of Go map or sql.Named argument order.
func (e Engine) Compile(query string, args ...any) (string, []any, error) {
	return e.compile(query, false, args...)
}

// CompileWrite accepts the portable INSERT/UPDATE/DELETE subset for actions.
func (e Engine) CompileWrite(query string, args ...any) (string, []any, error) {
	return e.compile(query, true, args...)
}

func (e Engine) compile(query string, write bool, args ...any) (string, []any, error) {
	p := parser.NewParser(strings.NewReader(query))
	n, err := p.ParseStatement()
	if err != nil {
		return "", nil, fmt.Errorf("多库 SELECT 子集解析失败: %w", err)
	}
	if next, end := p.ParseStatement(); next != nil || !errors.Is(end, io.EOF) {
		return "", nil, errors.New("只允许单条 SELECT 语句")
	}
	if write {
		switch x := n.(type) {
		case *parser.InsertStatement:
			if x.WithClause != nil || x.Replace.IsValid() || x.InsertOr.IsValid() || len(x.UpsertClauses) > 0 || x.ReturningClause != nil || x.Alias != nil {
				return "", nil, errors.New("不支持该 INSERT 形式")
			}
			if x.Schema != nil && x.Schema.Name != e.Schema {
				return "", nil, errors.New("不允许跨 schema 写入")
			}
			physical, ok := e.Tables[strings.ToLower(x.Table.Name)]
			if !ok {
				return "", nil, errors.New("未登记的写入表")
			}
			x.Schema = &parser.Ident{Name: e.Schema, Quoted: true}
			x.Table = &parser.Ident{Name: physical, Quoted: true}
		case *parser.UpdateStatement:
			if x.WhereExpr == nil || x.WithClause != nil || x.UpdateOr.IsValid() || x.Source != nil || x.ReturningClause != nil {
				return "", nil, errors.New("UPDATE 需要 WHERE，不支持 FROM、WITH、RETURNING 或冲突处理")
			}
		case *parser.DeleteStatement:
			if x.WhereExpr == nil || x.WithClause != nil || x.LimitExpr != nil || len(x.OrderingTerms) > 0 || x.ReturningClause != nil {
				return "", nil, errors.New("DELETE 需要 WHERE，不支持 WITH、LIMIT、ORDER 或 RETURNING")
			}
		default:
			return "", nil, errors.New("只允许 INSERT/UPDATE/DELETE")
		}
	} else if _, ok := n.(*parser.SelectStatement); !ok {
		return "", nil, errors.New("只允许 SELECT 查询")
	}
	ctes := map[string]bool{}
	generatedDual := map[*parser.QualifiedTableName]bool{}
	_, err = parser.Walk(parser.VisitFunc(func(n parser.Node) (parser.Node, error) {
		if c, ok := n.(*parser.CTE); ok {
			key := strings.ToLower(c.TableName.Name)
			if _, real := e.Tables[key]; real {
				return nil, errors.New("CTE 不能与数据表同名")
			}
			ctes[key] = true
		}
		return n, nil
	}), n)
	if err != nil {
		return "", nil, err
	}
	_, err = parser.Walk(parser.VisitFunc(func(n parser.Node) (parser.Node, error) {
		switch x := n.(type) {
		case *parser.InsertStatement, *parser.UpdateStatement, *parser.DeleteStatement, *parser.Assignment:
			if !write {
				return nil, errors.New("只允许 SELECT")
			}
		case *parser.SelectStatement:
			if len(x.ValueLists) > 0 || len(x.Windows) > 0 {
				return nil, errors.New("暂不支持 VALUES 或 WINDOW")
			}
			if e.Driver == "oracle" && x.Except.IsValid() {
				return nil, errors.New("Oracle 暂不支持 EXCEPT，请改写为其他受支持的查询")
			}
			for _, limit := range []parser.Expr{x.LimitExpr, x.OffsetExpr} {
				if limit != nil {
					v, ok := limit.(*parser.NumberLit)
					if !ok {
						return nil, errors.New("LIMIT/OFFSET 必须为非负整数字面量")
					}
					if _, err := strconv.ParseUint(v.Value, 10, 32); err != nil {
						return nil, errors.New("LIMIT/OFFSET 必须为非负整数")
					}
				}
			}
			if x.Source == nil && e.Driver == "oracle" {
				dual := &parser.QualifiedTableName{Schema: &parser.Ident{Name: "SYS", Quoted: true}, Name: &parser.Ident{Name: "DUAL", Quoted: true}}
				generatedDual[dual] = true
				x.Source = dual
			}
		case *parser.QualifiedTableName:
			name := x.Name.Name
			if x.Index != nil || x.NotIndexed.IsValid() {
				return nil, errors.New("暂不支持索引提示")
			}
			if x.Schema == nil && ctes[strings.ToLower(name)] {
				break
			}
			if generatedDual[x] {
				break
			}
			if x.Schema != nil && !strings.EqualFold(x.Schema.Name, "main") && x.Schema.Name != e.Schema {
				return nil, errors.New("不允许访问其他 schema")
			}
			physical, ok := e.Tables[strings.ToLower(name)]
			if !ok {
				return nil, fmt.Errorf("未登记的数据表 %s", name)
			}
			x.Schema = &parser.Ident{Name: e.Schema, Quoted: true}
			x.Name = &parser.Ident{Name: physical, Quoted: true}
		case *parser.QualifiedRef:
			if x.Schema != nil {
				return nil, errors.New("请使用表别名引用字段，不能使用 schema.table.column")
			}
		case *parser.Call:
			if x.Schema != nil || x.Name.Quoted || !safeFunctions[strings.ToUpper(x.Name.Name)] || x.Over != nil || x.Filter != nil {
				return nil, fmt.Errorf("暂不支持函数 %s 或其窗口/过滤形式", x.Name.Name)
			}
		case *parser.BinaryExpr:
			switch x.Op {
			case parser.EQ, parser.NE, parser.LT, parser.LE, parser.GT, parser.GE,
				parser.PLUS, parser.MINUS, parser.STAR, parser.SLASH,
				parser.AND, parser.OR, parser.IN, parser.NOTIN, parser.IS, parser.ISNOT,
				parser.LIKE, parser.NOTLIKE, parser.BETWEEN, parser.NOTBETWEEN:
			default:
				return nil, fmt.Errorf("暂不支持运算符 %s", x.Op)
			}
		case *parser.WithClause:
			if x.Recursive.IsValid() {
				return nil, errors.New("多库查询暂不支持递归 CTE")
			}
		case *parser.CTE:
			if x.Materialized.IsValid() {
				return nil, errors.New("多库查询暂不支持 MATERIALIZED 提示")
			}
		case *parser.JoinOperator:
			if x.Natural.IsValid() || (e.Driver == "mysql" && x.Full.IsValid()) {
				return nil, errors.New("不支持该 JOIN 形式，请显式使用 JOIN ON")
			}
		case *parser.UnaryExpr:
			if x.Op != parser.NOT && x.Op != parser.PLUS && x.Op != parser.MINUS {
				return nil, errors.New("不支持该一元运算")
			}
		case *parser.BoolLit:
			if e.Driver == "oracle" {
				return nil, errors.New("Oracle 查询请使用数值或比较表达式代替布尔字面量")
			}
		case *parser.Null:
			if x.Op != parser.ISNULL {
				return nil, errors.New("请使用 IS NOT NULL，不能使用 SQLite 的 NOT NULL 表达式")
			}
		case *parser.CastExpr:
			// Keep the first release's expression semantics explicit. Browsing
			// uses separately generated trusted casts for each engine.
			return nil, errors.New("多库查询暂不支持 CAST，请使用数据源中的类型字段")
		case *parser.CollateExpr, *parser.BlobLit, *parser.Raise, *parser.QualifiedTableFunctionName:
			return nil, errors.New("不支持该多库 SELECT 表达式")
		case *parser.Ident:
			if !x.Quoted {
				x.Name = e.fold(x.Name)
			}
		case *parser.ResultColumn, *parser.ParenSource, *parser.JoinClause,
			*parser.OnConstraint, *parser.UsingConstraint, *parser.OrderingTerm,
			*parser.ParenExpr, *parser.CaseExpr, *parser.CaseBlock, *parser.Exists,
			*parser.Range, *parser.ExprList, *parser.StringLit, *parser.NumberLit,
			*parser.NullLit, *parser.TimestampLit, *parser.BindExpr:
		default:
			return nil, fmt.Errorf("未经支持的 SQL 节点 %T", n)
		}
		return n, nil
	}), n)
	if err != nil {
		return "", nil, err
	}
	return e.emit(n.String(), args)
}

type token struct {
	kind  parser.Token
	value string
}

func (e Engine) emit(canonical string, args []any) (string, []any, error) {
	scan := parser.NewScanner(strings.NewReader(canonical))
	var tokens []token
	for {
		_, k, v := scan.Scan()
		if k == parser.EOF {
			break
		}
		if k == parser.ILLEGAL {
			return "", nil, errors.New("SQL 输出校验失败")
		}
		tokens = append(tokens, token{k, v})
	}
	named := map[string]any{}
	positional := []any{}
	for _, a := range args {
		if n, ok := a.(sql.NamedArg); ok {
			named[n.Name] = n.Value
		} else {
			positional = append(positional, a)
		}
	}
	bound := []any{}
	pos := 0
	parts := []string{}
	for i, t := range tokens {
		v := t.value
		switch t.kind {
		case parser.QIDENT, parser.BIDENT:
			v = e.Quote(v)
		case parser.STRING:
			v = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		case parser.BIND:
			var value any
			if v == "?" {
				if pos >= len(positional) {
					return "", nil, errors.New("缺少位置参数")
				}
				value = positional[pos]
				pos++
			} else {
				var ok bool
				value, ok = named[strings.TrimPrefix(v, ":")]
				if !ok || !strings.HasPrefix(v, ":") {
					return "", nil, fmt.Errorf("缺少或不支持的参数 %s", v)
				}
			}
			bound = append(bound, value)
			switch e.Driver {
			case "postgresql":
				v = fmt.Sprintf("$%d", len(bound))
			case "oracle", "dameng":
				v = fmt.Sprintf(":%d", len(bound))
			default:
				v = "?"
			}
		case parser.AS:
			// Oracle permits column aliases without AS, requires this for table
			// aliases, and still requires AS before a CTE's parenthesized body.
			if e.Driver == "oracle" && i+1 < len(tokens) && tokens[i+1].kind != parser.LP {
				continue
			}
		}
		parts = append(parts, v)
	}
	if e.Driver == "oracle" {
		parts = oracleLimits(parts)
	}
	return strings.Join(parts, " "), bound, nil
}

func oracleLimits(parts []string) []string {
	out := []string{}
	for i := 0; i < len(parts); i++ {
		if parts[i] != "LIMIT" {
			out = append(out, parts[i])
			continue
		}
		limit := parts[i+1]
		i++
		if i+2 < len(parts) && parts[i+1] == "OFFSET" {
			out = append(out, "OFFSET", parts[i+2], "ROWS")
			i += 2
		}
		out = append(out, "FETCH", "NEXT", limit, "ROWS", "ONLY")
	}
	return out
}
