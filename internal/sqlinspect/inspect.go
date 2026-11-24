// Package sqlinspect recognizes the deliberately small diagnostic SQL contract.
package sqlinspect

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	parser "github.com/rqlite/sql"
)

type Command struct{ Kind, Table, Query, Prefix string }

var metadata = regexp.MustCompile(`(?i)^\s*(SHOW\s+TABLES|SHOW\s+(?:COLUMNS|FIELDS|INDEX|INDEXES|KEYS)\s+(?:FROM|IN)\s+(.+)|(?:DESCRIBE|DESC)\s+(.+))\s*$`)
var identifier = regexp.MustCompile("^(?:[A-Za-z_][A-Za-z0-9_$]*|\"(?:[^\"]|\"\")+\"|`(?:[^`]|``)+`)(?:\\.(?:[A-Za-z_][A-Za-z0-9_$]*|\"(?:[^\"]|\"\")+\"|`(?:[^`]|``)+`))?$")
var explainHead = regexp.MustCompile(`(?i)^EXPLAIN\s+(?:QUERY\s+PLAN\s+)?`)

// Parse returns nil for ordinary SQL. Diagnostic options outside this contract
// (including ANALYZE, FORMAT, WHERE and LIKE) are rejected rather than forwarded.
func Parse(query string) (*Command, error) {
	s := strings.TrimSpace(query)
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, nil
	}
	switch strings.ToUpper(fields[0]) {
	case "EXPLAIN":
		p := parser.NewParser(strings.NewReader(s))
		stmt, err := p.ParseStatement()
		if err != nil {
			return nil, err
		}
		if next, end := p.ParseStatement(); next != nil || end != io.EOF {
			return nil, fmt.Errorf("只允许单条诊断语句")
		}
		x, ok := stmt.(*parser.ExplainStatement)
		if !ok {
			return nil, fmt.Errorf("不支持的 EXPLAIN")
		}
		if _, ok := x.Stmt.(*parser.SelectStatement); !ok {
			return nil, fmt.Errorf("EXPLAIN 只支持 SELECT，不支持 ANALYZE 或写操作")
		}
		prefix := "EXPLAIN "
		if x.QueryPlan.IsValid() {
			prefix = "EXPLAIN QUERY PLAN "
		}
		head := explainHead.FindStringIndex(s)
		if head == nil {
			return nil, fmt.Errorf("不支持的 EXPLAIN 前缀")
		}
		return &Command{Kind: "explain", Query: s[head[1]:], Prefix: prefix}, nil
	case "SHOW", "DESCRIBE", "DESC":
		m := metadata.FindStringSubmatch(s)
		if m == nil {
			return nil, fmt.Errorf("仅支持 SHOW TABLES、SHOW COLUMNS/INDEX FROM 表名和 DESCRIBE 表名")
		}
		if len(fields) == 2 && strings.EqualFold(fields[1], "TABLES") {
			return &Command{Kind: "tables"}, nil
		}
		name := strings.TrimSpace(m[2])
		if name == "" {
			name = strings.TrimSpace(m[3])
		}
		if !identifier.MatchString(name) {
			return nil, fmt.Errorf("结构查看需要单个表名，不支持额外选项")
		}
		node, err := parser.NewParser(strings.NewReader("SELECT * FROM " + name)).ParseStatement()
		if err != nil {
			return nil, err
		}
		table := node.(*parser.SelectStatement).Source.(*parser.QualifiedTableName)
		if strings.Contains(table.Name.Name, ".") || (table.Schema != nil && strings.Contains(table.Schema.Name, ".")) {
			return nil, fmt.Errorf("表名或 Schema 标识符不能包含点号")
		}
		name = table.Name.Name
		if table.Schema != nil {
			name = table.Schema.Name + "." + name
		}
		kind := "columns"
		if strings.EqualFold(fields[0], "SHOW") && (strings.EqualFold(fields[1], "INDEX") || strings.EqualFold(fields[1], "INDEXES") || strings.EqualFold(fields[1], "KEYS")) {
			kind = "indexes"
		}
		return &Command{Kind: kind, Table: name}, nil
	}
	return nil, nil
}
