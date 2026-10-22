package rewrite

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	sql "github.com/rqlite/sql"
)

// DeniedError is a statement rejected by policy, as opposed to a parse error.
type DeniedError struct{ Reason string }

func (e *DeniedError) Error() string { return "SQL 改写拒绝：" + e.Reason }

func deny(format string, args ...any) error {
	return &DeniedError{Reason: fmt.Sprintf(format, args...)}
}

// IsDenied reports whether err is a policy rejection.
func IsDenied(err error) bool {
	var d *DeniedError
	return errors.As(err, &d)
}

// Rewriter rewrites SELECT statements against a known set of tables.
type Rewriter struct {
	cfg    *Config
	tables map[string]bool // every real table name, lower case
	quoted map[string]bool // identifiers quoted in the policy SQL, lower case
	subSQL map[string]string // one-line subquery text per table
}

// New returns a rewriter for the database's tables. The config must cover
// exactly the real tables: an unconfigured table would be a silent hole, and
// a configured table that does not exist is almost certainly a typo.
func New(cfg *Config, tableNames []string) (*Rewriter, error) {
	r := &Rewriter{cfg: cfg, tables: map[string]bool{}, quoted: map[string]bool{}, subSQL: map[string]string{}}
	for _, pol := range cfg.Tables {
		quotedNames(pol.CompiledSQL, r.quoted)
	}
	for _, pol := range cfg.Tables {
		if !pol.Passthrough {
			r.subSQL[pol.Table] = unquoteIdents(pol.subquery.String(), r.quoted)
		}
	}
	var missing, unknown []string
	for _, name := range tableNames {
		name = strings.ToLower(name)
		r.tables[name] = true
		if _, ok := cfg.Lookup(name); !ok {
			missing = append(missing, name)
		}
	}
	for _, pol := range cfg.Tables {
		if !r.tables[pol.Table] {
			unknown = append(unknown, pol.Table)
		}
	}
	var errs []error
	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("以下表没有配置改写规则：%s", strings.Join(missing, ", ")))
	}
	if len(unknown) > 0 {
		errs = append(errs, fmt.Errorf("改写配置中的表在数据库中不存在：%s", strings.Join(unknown, ", ")))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return r, nil
}

// Config returns the rewrite configuration in use.
func (r *Rewriter) Config() *Config { return r.cfg }

// TableRef is one FROM / JOIN table that was rewritten.
type TableRef struct {
	Table  string `json:"table"`
	Alias  string `json:"alias"`
	Scheme Scheme `json:"scheme"`
}

// Guard is a deny_unless condition the caller must evaluate (with the same
// bind values) before executing the rewritten statement.
type Guard struct {
	Table   string   `json:"table"`
	SQL     string   `json:"sql"`
	Params  []string `json:"params"`
	Message string   `json:"message"`
}

// Result is a rewritten statement. It does not depend on the user: user
// context enters only through the named parameters listed in Params.
type Result struct {
	RewrittenSQL string     `json:"rewritten_sql"`
	Tables       []TableRef `json:"tables"`
	Params       []string   `json:"params"`
	Guards       []Guard    `json:"guards"`
}

// Rewrite parses query, replaces every table in FROM / JOIN (at any depth:
// subqueries, CTE bodies, compound selects) with the table's configured
// subquery, and returns the result. The input text is kept as written, with
// each table reference replaced in place by a one-line subquery.
func (r *Rewriter) Rewrite(query string) (*Result, error) {
	if err := r.checkNoSchemaQualifiedTables(query); err != nil {
		return nil, err
	}
	parser := sql.NewParser(strings.NewReader(query))
	stmt, err := parser.ParseStatement()
	if err != nil {
		return nil, fmt.Errorf("SQL 解析失败：%w", err)
	}
	if next, err := parser.ParseStatement(); next != nil || !errors.Is(err, io.EOF) {
		return nil, deny("只允许单条语句")
	}
	sel, ok := stmt.(*sql.SelectStatement)
	if !ok {
		return nil, deny("只允许 SELECT 查询")
	}

	src := []rune(query)
	v := &visitor{rewriter: r, ctes: map[string]bool{}, src: src}
	// CTE names must not shadow real tables; otherwise whether a reference
	// means the CTE or the table would depend on SQLite's scoping rules.
	if _, err := sql.Walk(sql.VisitFunc(func(n sql.Node) (sql.Node, error) {
		if cte, ok := n.(*sql.CTE); ok {
			name := strings.ToLower(sql.IdentName(cte.TableName))
			if r.tables[name] {
				return nil, deny("CTE 名称 %s 与数据表同名", name)
			}
			v.ctes[name] = true
		}
		return n, nil
	}), sel); err != nil {
		return nil, err
	}

	out, err := sql.Walk(v, sel)
	if err != nil {
		return nil, err
	}
	printed := out.String()
	if err := r.checkAllTablesRewritten(printed); err != nil {
		return nil, err
	}
	rewritten, ok := spliced(src, v.splices, printed)
	if !ok {
		keep := maps.Clone(r.quoted)
		quotedNames(query, keep)
		rewritten = unquoteIdents(printed, keep)
	}
	res := &Result{RewrittenSQL: rewritten, Tables: v.refs, Params: []string{}, Guards: []Guard{}}
	for _, name := range res.TableNames() {
		pol, _ := r.cfg.Lookup(name)
		for _, p := range pol.Params {
			if !slices.Contains(res.Params, p) {
				res.Params = append(res.Params, p)
			}
		}
		if pol.CompiledGuard != "" {
			res.Guards = append(res.Guards, Guard{Table: name, SQL: pol.CompiledGuard, Params: pol.GuardParams, Message: pol.DenyMessage})
		}
	}
	slices.Sort(res.Params)
	return res, nil
}

type visitor struct {
	rewriter *Rewriter
	ctes     map[string]bool
	refs     []TableRef
	src      []rune   // the input, for splicing
	splices  []splice // in input order
}

func (v *visitor) Visit(n sql.Node) (sql.Visitor, sql.Node, error) {
	switch x := n.(type) {
	case *sql.QualifiedTableName:
		replacement, err := v.rewriteTable(x)
		// Never descend into the generated subquery.
		return nil, replacement, err
	case *sql.QualifiedTableFunctionName:
		return nil, nil, deny("不支持表值函数 %s", sql.IdentName(x.Name))
	case *sql.BindExpr:
		// Bind parameters are reserved for trusted context variables.
		return nil, nil, deny("不支持绑定参数 %s", x.Name)
	case *sql.BinaryExpr:
		if x.Op == sql.IN || x.Op == sql.NOTIN {
			switch y := x.Y.(type) {
			case *sql.Ident:
				if !v.ctes[strings.ToLower(y.Name)] {
					return nil, nil, deny("不支持 “IN 表名” 写法，请改用 IN (SELECT ...)")
				}
			case *sql.QualifiedRef:
				return nil, nil, deny("不支持 “IN schema.表名” 写法")
			case *sql.Call:
				return nil, nil, deny("不支持 IN 表值函数")
			}
		}
	}
	return v, n, nil
}

func (v *visitor) VisitEnd(n sql.Node) (sql.Node, error) { return n, nil }

func (v *visitor) rewriteTable(x *sql.QualifiedTableName) (sql.Source, error) {
	name := strings.ToLower(sql.IdentName(x.Name))
	if x.Schema != nil {
		return nil, deny("不允许 schema 限定的表名 %s.%s", sql.IdentName(x.Schema), name)
	}
	if !v.rewriter.tables[name] {
		if v.ctes[name] {
			return x, nil
		}
		return nil, deny("未知表 %s", name)
	}
	pol, ok := v.rewriter.cfg.Lookup(name)
	if !ok {
		return nil, deny("表 %s 没有配置改写规则", name)
	}
	alias := x.Alias
	if alias == nil {
		alias = &sql.Ident{Name: sql.IdentName(x.Name), Quoted: true}
	}
	v.refs = append(v.refs, TableRef{Table: name, Alias: alias.Name, Scheme: pol.Scheme})
	v.addSplice(x, name)
	if pol.Passthrough {
		return &sql.QualifiedTableName{Schema: &sql.Ident{Name: "main"}, Name: &sql.Ident{Name: name}, Alias: alias}, nil
	}
	return &sql.ParenSource{X: pol.subquery.Clone(), Alias: alias}, nil
}

type token struct {
	tok sql.Token
	lit string
}

func scanTokens(query string) []token {
	scanner := sql.NewScanner(strings.NewReader(query))
	var toks []token
	for {
		_, tok, lit := scanner.Scan()
		if tok == sql.EOF || tok == sql.ILLEGAL {
			return toks
		}
		if tok != sql.COMMENT {
			toks = append(toks, token{tok, lit})
		}
	}
}

func isIdent(t token) bool {
	return t.tok == sql.IDENT || t.tok == sql.QIDENT || t.tok == sql.BIDENT
}

// checkNoSchemaQualifiedTables rejects "<schema>.<table>" in the input, so a
// "main.<table>" in the output can only have come from a generated subquery.
func (r *Rewriter) checkNoSchemaQualifiedTables(query string) error {
	toks := scanTokens(query)
	for i := 1; i+1 < len(toks); i++ {
		if toks[i].tok == sql.DOT && isIdent(toks[i-1]) && isIdent(toks[i+1]) && r.tables[strings.ToLower(toks[i+1].lit)] {
			return deny("不允许 schema 限定的表名 %s.%s", toks[i-1].lit, toks[i+1].lit)
		}
	}
	return nil
}

// checkAllTablesRewritten is an independent, token-level check of the output.
// Every identifier naming a real table must be a generated "main.<table>", an
// alias after AS, or a qualifier before ".". Anything else is a table
// reference the AST rewrite missed, and the statement is rejected.
func (r *Rewriter) checkAllTablesRewritten(rewritten string) error {
	toks := scanTokens(rewritten)
	for i, t := range toks {
		if !isIdent(t) || !r.tables[strings.ToLower(t.lit)] {
			continue
		}
		generated := i >= 2 && toks[i-1].tok == sql.DOT && isIdent(toks[i-2]) && strings.EqualFold(toks[i-2].lit, "main")
		alias := i >= 1 && toks[i-1].tok == sql.AS
		qualifier := i+1 < len(toks) && toks[i+1].tok == sql.DOT
		if !generated && !alias && !qualifier {
			return deny("改写结果校验失败：表 %s 未被改写", t.lit)
		}
	}
	return nil
}

// TableNames lists the tables referenced by a result, de-duplicated.
func (res *Result) TableNames() []string {
	var names []string
	for _, ref := range res.Tables {
		if !slices.Contains(names, ref.Table) {
			names = append(names, ref.Table)
		}
	}
	return names
}
