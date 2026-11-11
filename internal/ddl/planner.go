package ddl

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	parser "github.com/rqlite/sql"
)

func oneStatement(query string) (parser.Statement, error) {
	p := parser.NewParser(strings.NewReader(query))
	n, err := p.ParseStatement()
	if err != nil {
		return nil, err
	}
	if next, end := p.ParseStatement(); next != nil || !errors.Is(end, io.EOF) {
		return nil, errors.New("对象定义必须为单条 SQL")
	}
	return n, nil
}

func safeName(name string) error {
	if strings.TrimSpace(name) == "" || strings.ContainsRune(name, 0) || strings.HasPrefix(strings.ToLower(name), "sqlite_") || strings.HasPrefix(strings.ToLower(name), "_datascope_ddl_") {
		return errors.New("对象名为空或属于保留名称")
	}
	return nil
}

func mainSchema(schema *parser.Ident) error {
	if schema != nil && !strings.EqualFold(schema.Name, "main") {
		return errors.New("DDL 仅允许 main 数据库")
	}
	return nil
}

func validateStatement(n parser.Statement) error {
	var schema, name *parser.Ident
	switch x := n.(type) {
	case *parser.CreateTableStatement:
		schema, name = x.Schema, x.Name
		if x.Temp.IsValid() || x.Select != nil {
			return errors.New("不支持临时表和 CREATE TABLE AS SELECT")
		}
	case *parser.AlterTableStatement:
		schema, name = x.Schema, x.Name
		if x.NewName != nil {
			if err := safeName(x.NewName.Name); err != nil {
				return err
			}
		}
	case *parser.DropTableStatement:
		schema, name = x.Schema, x.Name
	case *parser.CreateIndexStatement:
		schema, name = x.Schema, x.Name
		if err := safeName(x.Table.Name); err != nil {
			return err
		}
	case *parser.DropIndexStatement:
		schema, name = x.Schema, x.Name
	case *parser.CreateViewStatement:
		schema, name = x.Schema, x.Name
		if x.Temp.IsValid() {
			return errors.New("不支持临时视图")
		}
	case *parser.DropViewStatement:
		schema, name = x.Schema, x.Name
	case *parser.CreateTriggerStatement:
		schema, name = x.Schema, x.Name
		if x.Temp.IsValid() {
			return errors.New("不支持临时触发器")
		}
		if err := mainSchema(x.TableSchema); err != nil {
			return err
		}
		if err := safeName(x.Table.Name); err != nil {
			return err
		}
	case *parser.DropTriggerStatement:
		schema, name = x.Schema, x.Name
	default:
		return errors.New("仅允许表、索引、视图和触发器的 DDL；数据写入、PRAGMA、ATTACH 和事务控制不允许")
	}
	if err := mainSchema(schema); err != nil {
		return err
	}
	if err := safeName(name.Name); err != nil {
		return err
	}
	_, err := parser.Walk(parser.VisitFunc(func(n parser.Node) (parser.Node, error) {
		switch x := n.(type) {
		case *parser.QualifiedTableName:
			if e := mainSchema(x.Schema); e != nil {
				return nil, e
			}
			if e := safeName(x.Name.Name); e != nil {
				return nil, e
			}
		case *parser.QualifiedTableFunctionName:
			if e := mainSchema(x.Schema); e != nil {
				return nil, e
			}
		case *parser.InsertStatement:
			if e := mainSchema(x.Schema); e != nil {
				return nil, e
			}
			if e := safeName(x.Table.Name); e != nil {
				return nil, e
			}
		}
		return n, nil
	}), n)
	return err
}

// The upstream parser predates native ALTER COLUMN ... NOT NULL (SQLite 3.53).
const identPattern = `(?:"(?:[^"]|"")+"|\x60[^\x60]+\x60|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_]*)`

var notNullSQL = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+(` + identPattern + `(?:\s*\.\s*` + identPattern + `)?)\s+ALTER\s+(?:COLUMN\s+)?(` + identPattern + `)\s+(SET\s+NOT\s+NULL|DROP\s+NOT\s+NULL)\s*;?\s*$`)

// Split only at top-level semicolons. Trigger bodies contain both semicolons
// and CASE ... END expressions; quoted strings and comments are scanner tokens.
func splitSQL(query string) []string {
	runes := []rune(query)
	clean := append([]rune{}, runes...)
	scanner := parser.NewScanner(strings.NewReader(query))
	start, count, comment, caseDepth := 0, 0, -1, 0
	trigger, body := false, false
	out := []string{}
	for {
		pos, token, _ := scanner.Scan()
		end := pos.Offset
		if token == parser.EOF {
			end = len(runes)
		}
		if comment >= 0 {
			for i := comment; i < end && i < len(clean); i++ {
				clean[i] = ' '
			}
			comment = -1
		}
		if token == parser.COMMENT {
			comment = pos.Offset
			continue
		}
		if token == parser.EOF {
			if count > 0 {
				out = append(out, strings.TrimSpace(string(clean[start:])))
			}
			break
		}
		if token == parser.TRIGGER && (count == 1 || count == 2) {
			prefix := strings.ToUpper(strings.TrimSpace(string(clean[start:pos.Offset])))
			trigger = prefix == "CREATE" || prefix == "CREATE TEMP" || prefix == "CREATE TEMPORARY"
		}
		if trigger && token == parser.BEGIN {
			body = true
		}
		if body && token == parser.CASE {
			caseDepth++
		}
		if body && token == parser.END {
			if caseDepth > 0 {
				caseDepth--
			} else {
				body = false
			}
		}
		if token == parser.SEMI && !body {
			if count > 0 {
				out = append(out, strings.TrimSpace(string(clean[start:pos.Offset])))
			}
			start = pos.Offset + 1
			count = 0
			trigger = false
			caseDepth = 0
			continue
		}
		count++
	}
	return out
}

func parseSQL(query string) ([]string, error) {
	statements := splitSQL(query)
	if len(statements) == 0 {
		return nil, errors.New("需要 DDL SQL")
	}
	if len(statements) > 100 {
		return nil, errors.New("一次最多 100 条 DDL")
	}
	out := []string{}
	for _, statement := range statements {
		result, err := parseSingleSQL(statement)
		if err != nil {
			return nil, err
		}
		out = append(out, result...)
	}
	return out, nil
}

func parseSingleSQL(query string) ([]string, error) {
	if m := notNullSQL.FindStringSubmatch(query); m != nil {
		n, e := oneStatement("ALTER TABLE " + m[1] + " RENAME COLUMN " + m[2] + " TO _ddl_check")
		if e != nil {
			return nil, e
		}
		if e = validateStatement(n); e != nil {
			return nil, e
		}
		return []string{strings.TrimSpace(query)}, nil
	}
	p := parser.NewParser(strings.NewReader(query))
	out := []string{}
	for {
		n, err := p.ParseStatement()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if err = validateStatement(n); err != nil {
			return nil, err
		}
		out = append(out, n.String())
		if len(out) > 100 {
			return nil, errors.New("一次最多 100 条 DDL")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("需要 DDL SQL")
	}
	return out, nil
}

func columnSQL(c Column) (string, error) {
	if c.SQL != "" {
		return c.SQL, nil
	}
	if err := safeName(c.Name); err != nil {
		return "", err
	}
	s := Quote(c.Name)
	if c.Type != "" {
		s += " " + c.Type
	}
	if c.NotNull {
		s += " NOT NULL"
	}
	if c.Default != nil {
		s += " DEFAULT " + *c.Default
	}
	if c.PrimaryKey > 0 {
		s += " PRIMARY KEY"
	}
	return s, nil
}

func tableDefinition(name string, d Definition) (*parser.CreateTableStatement, error) {
	query := d.SQL
	if query == "" {
		parts := []string{}
		for _, c := range d.Columns {
			s, e := columnSQL(c)
			if e != nil {
				return nil, e
			}
			parts = append(parts, s)
		}
		parts = append(parts, d.Constraints...)
		query = "CREATE TABLE " + Quote(name) + " (" + strings.Join(parts, ", ") + ")"
		if d.WithoutRowID {
			query += " WITHOUT ROWID"
		}
		if d.Strict {
			if d.WithoutRowID {
				query += ", STRICT"
			} else {
				query += " STRICT"
			}
		}
	}
	n, err := oneStatement(query)
	if err != nil {
		return nil, err
	}
	t, ok := n.(*parser.CreateTableStatement)
	if !ok {
		return nil, errors.New("需要 CREATE TABLE 目标定义")
	}
	if err = validateStatement(t); err != nil {
		return nil, err
	}
	if !strings.EqualFold(t.Name.Name, name) {
		return nil, errors.New("定义中的表名与目标不一致")
	}
	return t, nil
}

func findObject(objects []Object, typ, name string) (Object, error) {
	for _, o := range objects {
		if o.ObjectType == typ && strings.EqualFold(o.Name, name) {
			return o, nil
		}
	}
	return Object{}, fmt.Errorf("%s %s 不存在", typ, name)
}

func compileChange(ctx context.Context, q querier, c Change) ([]string, bool, error) {
	if err := safeName(c.Name); err != nil {
		return nil, false, err
	}
	if c.ParentName != "" {
		if err := safeName(c.ParentName); err != nil {
			return nil, false, err
		}
	}
	if c.NewName != "" {
		if err := safeName(c.NewName); err != nil {
			return nil, false, err
		}
	}
	objects, err := Objects(ctx, q)
	if err != nil {
		return nil, false, err
	}
	if c.ObjectType == "column" || c.ObjectType == "constraint" {
		o, e := findObject(objects, "table", c.ParentName)
		if e != nil {
			return nil, false, e
		}
		old, e := tableDefinition(o.Name, Definition{SQL: o.SQL})
		if e != nil {
			return nil, false, e
		}
		if c.ObjectType == "column" && c.Operation == "rename" {
			if c.NewName == "" {
				return nil, false, errors.New("需要 new_name")
			}
			return []string{"ALTER TABLE " + Quote(o.Name) + " RENAME COLUMN " + Quote(c.Name) + " TO " + Quote(c.NewName)}, false, nil
		}
		target := old.Clone()
		found := -1
		if c.ObjectType == "column" {
			for i, col := range target.Columns {
				if strings.EqualFold(col.Name.Name, c.Name) {
					found = i
				}
			}
			if c.Operation != "create" && found < 0 {
				return nil, false, errors.New("字段不存在")
			}
			if c.Operation == "create" && found >= 0 {
				return nil, false, errors.New("字段已存在")
			}
			if c.Operation == "drop" {
				target.Columns = append(target.Columns[:found], target.Columns[found+1:]...)
			} else if c.Operation == "update" || c.Operation == "create" {
				var def Column
				if len(c.Definition.Columns) == 1 {
					def = c.Definition.Columns[0]
				} else if c.Definition.SQL != "" {
					def = Column{SQL: c.Definition.SQL}
				} else {
					return nil, false, errors.New("字段定义需要 columns 中的一项或 sql")
				}
				if def.Name == "" {
					def.Name = c.Name
				}
				s, e := columnSQL(def)
				if e != nil {
					return nil, false, e
				}
				n, e := oneStatement("CREATE TABLE _column (" + s + ")")
				if e != nil {
					return nil, false, e
				}
				t := n.(*parser.CreateTableStatement)
				if len(t.Columns) != 1 || len(t.Constraints) != 0 || !strings.EqualFold(t.Columns[0].Name.Name, c.Name) {
					return nil, false, errors.New("字段定义与目标不一致")
				}
				if found < 0 {
					target.Columns = append(target.Columns, t.Columns[0])
				} else {
					target.Columns[found] = t.Columns[0]
				}
			} else {
				return nil, false, errors.New("不支持的字段操作")
			}
		} else {
			for i, con := range target.Constraints {
				if constraintName(con, i) == c.Name {
					found = i
				}
			}
			if c.Operation != "create" && found < 0 {
				return nil, false, errors.New("约束不存在；未命名约束使用 constraint_序号")
			}
			if c.Operation == "drop" {
				target.Constraints = append(target.Constraints[:found], target.Constraints[found+1:]...)
			} else if c.Operation == "update" || c.Operation == "create" {
				n, e := oneStatement("CREATE TABLE _constraint (_placeholder INTEGER, " + c.Definition.SQL + ")")
				if e != nil {
					return nil, false, e
				}
				t := n.(*parser.CreateTableStatement)
				if len(t.Columns) != 1 || len(t.Constraints) != 1 {
					return nil, false, errors.New("需要单个表约束定义")
				}
				if found < 0 {
					target.Constraints = append(target.Constraints, t.Constraints[0])
				} else {
					target.Constraints[found] = t.Constraints[0]
				}
			} else {
				return nil, false, errors.New("不支持的约束操作")
			}
		}
		s, e := rebuild(ctx, q, objects, old, target)
		return s, true, e
	}
	if c.ObjectType != "table" && c.ObjectType != "index" && c.ObjectType != "view" && c.ObjectType != "trigger" {
		return nil, false, errors.New("对象类型无效")
	}
	if c.Operation == "drop" {
		if _, e := findObject(objects, c.ObjectType, c.Name); e != nil {
			return nil, false, e
		}
		return []string{"DROP " + strings.ToUpper(c.ObjectType) + " " + Quote(c.Name)}, false, nil
	}
	if c.Operation == "rename" {
		if c.NewName == "" {
			return nil, false, errors.New("需要 new_name")
		}
		o, e := findObject(objects, c.ObjectType, c.Name)
		if e != nil {
			return nil, false, e
		}
		if c.ObjectType == "table" {
			return []string{"ALTER TABLE " + Quote(c.Name) + " RENAME TO " + Quote(c.NewName)}, false, nil
		}
		n, e := oneStatement(o.SQL)
		if e != nil {
			return nil, false, e
		}
		switch x := n.(type) {
		case *parser.CreateIndexStatement:
			x.Name.Name = c.NewName
		case *parser.CreateViewStatement:
			x.Name.Name = c.NewName
		case *parser.CreateTriggerStatement:
			x.Name.Name = c.NewName
		}
		statements := []string{"DROP " + strings.ToUpper(c.ObjectType) + " " + Quote(c.Name), n.String()}
		if c.ObjectType == "view" {
			for _, dependent := range objects {
				if dependent.ObjectType == "trigger" && dependent.ParentName == o.Name {
					n, e := oneStatement(dependent.SQL)
					if e != nil {
						return nil, false, e
					}
					trigger := n.(*parser.CreateTriggerStatement)
					trigger.Table.Name = c.NewName
					statements = append(statements, trigger.String())
				}
			}
		}
		return statements, false, nil
	}
	if c.Operation != "create" && c.Operation != "update" {
		return nil, false, errors.New("操作类型无效")
	}
	query := c.Definition.SQL
	if c.ObjectType == "table" {
		t, e := tableDefinition(c.Name, c.Definition)
		if e != nil {
			return nil, false, e
		}
		if c.Operation == "create" {
			return []string{t.String()}, false, nil
		}
		o, e := findObject(objects, "table", c.Name)
		if e != nil {
			return nil, false, e
		}
		old, e := tableDefinition(c.Name, Definition{SQL: o.SQL})
		if e != nil {
			return nil, false, e
		}
		s, e := rebuild(ctx, q, objects, old, t)
		return s, true, e
	}
	if c.ObjectType == "index" && query == "" {
		query = "CREATE "
		if c.Definition.Unique {
			query += "UNIQUE "
		}
		query += "INDEX " + Quote(c.Name) + " ON " + Quote(c.ParentName) + " ("
		parts := []string{}
		for _, col := range c.Definition.Columns {
			parts = append(parts, Quote(col.Name))
		}
		query += strings.Join(parts, ",") + ")"
		if c.Definition.Where != "" {
			query += " WHERE " + c.Definition.Where
		}
	}
	n, e := oneStatement(query)
	if e != nil {
		return nil, false, e
	}
	if e = validateStatement(n); e != nil {
		return nil, false, e
	}
	valid := false
	switch x := n.(type) {
	case *parser.CreateIndexStatement:
		valid = c.ObjectType == "index" && strings.EqualFold(x.Name.Name, c.Name)
	case *parser.CreateViewStatement:
		valid = c.ObjectType == "view" && strings.EqualFold(x.Name.Name, c.Name)
	case *parser.CreateTriggerStatement:
		valid = c.ObjectType == "trigger" && strings.EqualFold(x.Name.Name, c.Name)
	}
	if !valid {
		return nil, false, errors.New("SQL 定义与对象类型或名称不一致")
	}
	out := []string{}
	if c.Operation == "update" {
		if _, e = findObject(objects, c.ObjectType, c.Name); e != nil {
			return nil, false, e
		}
		out = append(out, "DROP "+strings.ToUpper(c.ObjectType)+" "+Quote(c.Name))
	}
	out = append(out, n.String())
	if c.ObjectType == "view" && c.Operation == "update" {
		for _, o := range objects {
			if o.ObjectType == "trigger" && strings.EqualFold(o.ParentName, c.Name) {
				out = append(out, o.SQL)
			}
		}
	}
	return out, false, nil
}

func constraintName(c parser.Constraint, i int) string {
	var n *parser.Ident
	switch x := c.(type) {
	case *parser.PrimaryKeyConstraint:
		n = x.Name
	case *parser.UniqueConstraint:
		n = x.Name
	case *parser.CheckConstraint:
		n = x.Name
	case *parser.ForeignKeyConstraint:
		n = x.Name
	}
	if n != nil {
		return n.Name
	}
	return fmt.Sprintf("constraint_%d", i+1)
}

func rebuild(ctx context.Context, q querier, objects []Object, old, target *parser.CreateTableStatement) ([]string, error) {
	if len(target.Columns) == 0 {
		return nil, errors.New("表至少需要一个字段")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	tmp := fmt.Sprintf("_datascope_ddl_%x", random)
	newTable := target.Clone()
	newTable.Schema = nil
	newTable.Name = &parser.Ident{Name: tmp}
	newTable.IfNotExists = parser.Pos{}
	cols, source := []string{}, []string{}
	oldCols := map[string]string{}
	for _, c := range old.Columns {
		oldCols[strings.ToLower(c.Name.Name)] = c.Name.Name
	}
	for _, c := range target.Columns {
		generated := false
		for _, con := range c.Constraints {
			if _, ok := con.(*parser.GeneratedConstraint); ok {
				generated = true
			}
		}
		if from, ok := oldCols[strings.ToLower(c.Name.Name)]; ok && !generated {
			cols = append(cols, Quote(c.Name.Name))
			source = append(source, Quote(from))
		}
	}
	// Preserve hidden row identifiers on ordinary rowid tables without an INTEGER PK alias.
	integerPK := false
	for _, c := range target.Columns {
		if c.Type != nil && strings.EqualFold(c.Type.String(), "INTEGER") {
			for _, con := range c.Constraints {
				if pk, ok := con.(*parser.PrimaryKeyConstraint); ok && !pk.Desc.IsValid() {
					integerPK = true
				}
			}
			for _, con := range target.Constraints {
				if pk, ok := con.(*parser.PrimaryKeyConstraint); ok && len(pk.Columns) == 1 {
					if ident, ok := pk.Columns[0].X.(*parser.Ident); ok && strings.EqualFold(ident.Name, c.Name.Name) {
						integerPK = true
					}
				}
			}
		}
	}
	if !old.Without.IsValid() && !target.Without.IsValid() && !integerPK {
		for _, alias := range []string{"rowid", "_rowid_", "oid"} {
			shadow := false
			for _, c := range append(append([]*parser.ColumnDefinition{}, old.Columns...), target.Columns...) {
				if strings.EqualFold(c.Name.Name, alias) {
					shadow = true
				}
			}
			if !shadow {
				cols = append(cols, Quote(alias))
				source = append(source, Quote(alias))
				break
			}
		}
	}
	if len(cols) == 0 {
		return nil, errors.New("没有可保留的数据字段；请显式设计迁移映射")
	}
	// Temporarily remove views, avoiding invalid-schema failures during the rename.
	out := []string{}
	for _, o := range objects {
		if o.ObjectType == "view" {
			out = append(out, "DROP VIEW "+Quote(o.Name))
		}
	}
	out = append(out, newTable.String(), "INSERT INTO "+Quote(tmp)+" ("+strings.Join(cols, ",")+") SELECT "+strings.Join(source, ",")+" FROM "+Quote(old.Name.Name), "DROP TABLE "+Quote(old.Name.Name), "ALTER TABLE "+Quote(tmp)+" RENAME TO "+Quote(target.Name.Name))
	// Preserve the AUTOINCREMENT high-water mark, even if its highest row was deleted.
	if strings.Contains(strings.ToUpper(target.String()), "AUTOINCREMENT") {
		var seq sql.NullInt64
		var exists int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name='sqlite_sequence'").Scan(&exists); err != nil {
			return nil, err
		}
		var err error
		if exists > 0 {
			err = q.QueryRowContext(ctx, "SELECT seq FROM sqlite_sequence WHERE name=?", old.Name.Name).Scan(&seq)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if seq.Valid {
			literal := "'" + strings.ReplaceAll(target.Name.Name, "'", "''") + "'"
			out = append(out, fmt.Sprintf("UPDATE sqlite_sequence SET seq=MAX(seq,%d) WHERE name=%s", seq.Int64, literal), fmt.Sprintf("INSERT INTO sqlite_sequence(name,seq) SELECT %s,%d WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name=%s)", literal, seq.Int64, literal))
		}
	}
	for _, o := range objects {
		if o.ObjectType == "index" && o.ParentName == old.Name.Name && o.SQL != "" {
			out = append(out, o.SQL)
		}
	}
	for _, o := range objects {
		if o.ObjectType == "view" {
			out = append(out, o.SQL)
		}
	}
	for _, o := range objects {
		if o.ObjectType == "trigger" {
			ownerView := false
			for _, v := range objects {
				if v.ObjectType == "view" && v.Name == o.ParentName {
					ownerView = true
				}
			}
			if o.ParentName == old.Name.Name || ownerView {
				out = append(out, o.SQL)
			}
		}
	}
	return out, nil
}
