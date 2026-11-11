package ddl

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	parser "github.com/rqlite/sql"
	sqlite "modernc.org/sqlite"
)

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func Open(ctx context.Context, path string, write bool) (*sql.DB, error) {
	mode := "ro"
	if write {
		mode = "rw"
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=" + mode + "&_dqs=0&_pragma=busy_timeout(3000)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// Snapshot uses SQLite's online backup API, so committed WAL pages are included.
func Snapshot(ctx context.Context, path, dest string) error {
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		return errors.New("备份目标已存在或无法访问")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	f.Close()
	db, err := Open(ctx, path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	err = conn.Raw(func(raw any) (err error) {
		b, err := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		}).NewBackup(dest)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, b.Finish()) }()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := b.Step(256)
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		}
	})
	if err != nil {
		_ = os.Remove(dest)
	}
	return err
}

func Objects(ctx context.Context, q querier) ([]Object, error) {
	rows, err := q.QueryContext(ctx, `SELECT type,name,tbl_name,coalesce(sql,'') FROM main.sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		return nil, err
	}
	out := []Object{}
	for rows.Next() {
		var o Object
		if err = rows.Scan(&o.ObjectType, &o.Name, &o.ParentName, &o.SQL); err != nil {
			rows.Close()
			return nil, err
		}
		o.Dependencies = []string{}
		o.Definition.SQL = o.SQL
		out = append(out, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		o := &out[i]
		if o.ObjectType == "table" || o.ObjectType == "view" {
			rows, err = q.QueryContext(ctx, `SELECT name,type,"notnull",dflt_value,pk,hidden FROM pragma_table_xinfo(?) ORDER BY cid`, o.Name)
			if err != nil {
				if o.ObjectType == "view" {
					continue
				}
				return nil, err
			}
			o.Definition.Columns = []Column{}
			for rows.Next() {
				var c Column
				if err = rows.Scan(&c.Name, &c.Type, &c.NotNull, &c.Default, &c.PrimaryKey, &c.Hidden); err != nil {
					rows.Close()
					return nil, err
				}
				o.Definition.Columns = append(o.Definition.Columns, c)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			if n, e := oneStatement(o.SQL); e == nil {
				if t, ok := n.(*parser.CreateTableStatement); ok {
					o.Definition.Strict = t.Strict.IsValid()
					o.Definition.WithoutRowID = t.Without.IsValid()
					for _, c := range t.Constraints {
						o.Definition.Constraints = append(o.Definition.Constraints, c.String())
					}
					for j := range o.Definition.Columns {
						for _, c := range t.Columns {
							if strings.EqualFold(c.Name.Name, o.Definition.Columns[j].Name) {
								o.Definition.Columns[j].SQL = c.String()
							}
						}
					}
				}
			}
		}
		for _, other := range out {
			if other.Name == o.Name && other.ObjectType == o.ObjectType {
				continue
			}
			// Include direct ownership and parsed references; do not guess by substring.
			if (other.ParentName == o.Name && other.Name != o.Name) || references(other.SQL, o.Name) {
				o.Dependencies = append(o.Dependencies, other.ObjectType+":"+other.Name)
			}
		}
	}
	return out, nil
}

func references(query, name string) bool {
	n, err := oneStatement(query)
	if err != nil {
		return false
	}
	found := false
	_, _ = parser.Walk(parser.VisitFunc(func(n parser.Node) (parser.Node, error) {
		switch x := n.(type) {
		case *parser.QualifiedTableName:
			found = found || strings.EqualFold(x.Name.Name, name)
		case *parser.ForeignKeyConstraint:
			found = found || strings.EqualFold(x.ForeignTable.Name, name)
		case *parser.CreateTriggerStatement:
			found = found || strings.EqualFold(x.Table.Name, name)
		}
		return n, nil
	}), n)
	return found
}

func fingerprint(objects []Object) string {
	type item struct{ Type, Name, SQL string }
	items := []item{}
	for _, o := range objects {
		items = append(items, item{o.ObjectType, o.Name, o.SQL})
	}
	b, _ := json.Marshal(items)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func difference(before, after []Object) []Difference {
	old := map[string]Object{}
	next := map[string]Object{}
	for _, o := range before {
		old[o.ObjectType+":"+o.Name] = o
	}
	for _, o := range after {
		next[o.ObjectType+":"+o.Name] = o
	}
	out := []Difference{}
	for _, o := range before {
		n := next[o.ObjectType+":"+o.Name]
		if o.SQL != n.SQL {
			out = append(out, Difference{o.ObjectType, o.Name, o.SQL, n.SQL})
		}
	}
	for _, o := range after {
		if _, ok := old[o.ObjectType+":"+o.Name]; !ok {
			out = append(out, Difference{o.ObjectType, o.Name, "", o.SQL})
		}
	}
	return out
}

func validateDatabase(ctx context.Context, q querier) error {
	rows, err := q.QueryContext(ctx, "PRAGMA main.foreign_key_check")
	if err != nil {
		return err
	}
	bad := rows.Next()
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return rowErr
	}
	if bad {
		return errors.New("变更导致外键约束不满足")
	}
	var check string
	if err = q.QueryRowContext(ctx, "PRAGMA main.quick_check").Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return fmt.Errorf("结构完整性检查失败: %s", check)
	}
	// SQLite permits invalid views at creation; compile each one explicitly.
	rows, err = q.QueryContext(ctx, `SELECT name FROM main.sqlite_schema WHERE type='view'`)
	if err != nil {
		return err
	}
	views := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		views = append(views, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range views {
		r, e := q.QueryContext(ctx, "SELECT * FROM "+Quote(name)+" LIMIT 0")
		if e != nil {
			return fmt.Errorf("视图 %s 失效: %w", name, e)
		}
		r.Close()
	}
	// Compile trigger events without running DML, catching references SQLite
	// accepts at CREATE TRIGGER time but would reject on the first business write.
	objects, err := Objects(ctx, q)
	if err != nil {
		return err
	}
	for _, o := range objects {
		if o.ObjectType != "trigger" {
			continue
		}
		n, e := oneStatement(o.SQL)
		if e != nil {
			return e
		}
		tr, ok := n.(*parser.CreateTriggerStatement)
		if !ok {
			continue
		}
		statement := ""
		switch {
		case tr.Insert.IsValid():
			statement = "INSERT INTO " + Quote(tr.Table.Name) + " DEFAULT VALUES"
		case tr.Delete.IsValid():
			statement = "DELETE FROM " + Quote(tr.Table.Name) + " WHERE 0"
		case tr.Update.IsValid():
			for _, table := range objects {
				if table.Name == tr.Table.Name && (table.ObjectType == "table" || table.ObjectType == "view") {
					fields := []string{}
					for _, col := range table.Definition.Columns {
						if col.Hidden == 0 {
							fields = append(fields, Quote(col.Name)+"="+Quote(col.Name))
						}
					}
					statement = "UPDATE " + Quote(table.Name) + " SET " + strings.Join(fields, ",") + " WHERE 0"
				}
			}
		}
		if statement != "" {
			r, e := q.QueryContext(ctx, "EXPLAIN "+statement)
			if e != nil {
				return fmt.Errorf("触发器 %s 失效: %w", o.Name, e)
			}
			r.Close()
		}
	}
	return nil
}
