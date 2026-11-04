package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"datascope-server/internal/audit"
	parser "github.com/rqlite/sql"
)

// OpenWriter opens an existing SQLite file separately; normal query pools stay
// mode=ro and query_only. Catalog validates the file against internal stores.
func OpenWriter(ctx context.Context, path string, timeout time.Duration) (*Store, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	// Reject SQLite's legacy fallback from missing quoted columns to string
	// literals: a hidden field must fail instead of becoming a fabricated value.
	dsn := (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=rw&_dqs=0&_pragma=foreign_keys(1)&_pragma=busy_timeout(1000)&_txlock=immediate"}).String()
	db, e := sql.Open("sqlite", dsn)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db: db, path: abs, queryTimeout: timeout}, nil
}
func (s *Store) BeginAction(ctx context.Context) (*ActionTx, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	return &ActionTx{s: s, tx: tx}, nil
}

type ActionTx struct {
	s     *Store
	tx    *sql.Tx
	Count int
}

func (t *ActionTx) Commit() error   { return t.tx.Commit() }
func (t *ActionTx) Rollback() error { return t.tx.Rollback() }
func actionStatement(query string, write bool) (string, error) {
	p := parser.NewParser(strings.NewReader(query))
	n, e := p.ParseStatement()
	if e != nil {
		return "", e
	}
	if next, end := p.ParseStatement(); next != nil || !errors.Is(end, io.EOF) {
		return "", errors.New("只允许单条 SQL")
	}
	if write {
		switch x := n.(type) {
		case *parser.InsertStatement:
			if x.Replace.IsValid() || x.InsertOr.IsValid() || x.ReturningClause != nil {
				return "", errors.New("不支持 REPLACE、冲突处理或 RETURNING")
			}
		case *parser.UpdateStatement:
			if x.WhereExpr == nil || x.UpdateOr.IsValid() || x.ReturningClause != nil {
				return "", errors.New("UPDATE 需要 WHERE，禁止冲突处理和 RETURNING")
			}
		case *parser.DeleteStatement:
			if x.WhereExpr == nil || x.ReturningClause != nil {
				return "", errors.New("DELETE 需要 WHERE，禁止 RETURNING")
			}
		default:
			return "", errors.New("db.exec 只允许 INSERT/UPDATE/DELETE")
		}
	} else {
		if _, ok := n.(*parser.SelectStatement); !ok {
			return "", errors.New("one/all 只允许 SELECT")
		}
	}
	_, e = parser.Walk(parser.VisitFunc(func(n parser.Node) (parser.Node, error) {
		switch x := n.(type) {
		case *parser.QualifiedTableName:
			if x.Schema != nil && !strings.EqualFold(x.Schema.Name, "main") {
				return nil, errors.New("禁止跨库 SQL")
			}
			if strings.HasPrefix(strings.ToLower(x.Name.Name), "sqlite_") {
				return nil, errors.New("不能访问 SQLite 内部表")
			}
		case *parser.InsertStatement:
			if x.Schema != nil && !strings.EqualFold(x.Schema.Name, "main") {
				return nil, errors.New("禁止跨库 INSERT")
			}
		case *parser.QualifiedTableFunctionName:
			return nil, errors.New("不允许表值函数")
		}
		return n, nil
	}), n)
	if e != nil {
		return "", e
	}
	return n.String(), nil
}

// ValidateActionTemplate checks that every placeholder is a SQL bind expression,
// never a quoted string/identifier/comment, and no unprovided bind is present.
func ValidateActionTemplate(query string, count int, write bool) error {
	body, e := actionStatement(query, write)
	if e != nil {
		return e
	}
	p := parser.NewParser(strings.NewReader(body))
	n, e := p.ParseStatement()
	if e != nil {
		return e
	}
	seen := map[string]int{}
	_, e = parser.Walk(parser.VisitFunc(func(n parser.Node) (parser.Node, error) {
		if b, ok := n.(*parser.BindExpr); ok {
			seen[b.Name]++
		}
		return n, nil
	}), n)
	if e != nil {
		return e
	}
	if len(seen) != count {
		return errors.New("SQL 插值必须直接用作绑定值，不能放在引号、注释或标识符中")
	}
	for i := 0; i < count; i++ {
		if seen[":arg_action"+strconv.Itoa(i)] != 1 {
			return errors.New("SQL 中只允许模板自动生成的绑定参数")
		}
	}
	return nil
}
func (t *ActionTx) prepare(ctx context.Context, query string, write bool, args []any) (string, []any, *audit.Event, error) {
	t.Count++
	ev, e := t.s.Audit.Start(ctx, "action.sql", query)
	if e != nil {
		return "", nil, nil, e
	}
	if ev != nil {
		ev.Binds = args
		ev.ExecutedSQL = query
	}
	if t.Count > 50 {
		e = errors.New("一次 action 最多 50 条 SQL")
	} else {
		query, e = actionStatement(query, write)
	}
	if e == nil && t.s.engine != nil {
		if write {
			query, args, e = t.s.engine.CompileWrite(query, args...)
		} else {
			query, args, e = t.s.Prepare(query, args...)
		}
	}
	if ev != nil {
		ev.ExecutedSQL = query
		ev.Binds = args
	}
	if e == nil && ev != nil {
		e = t.s.Audit.Put(ctx, *ev)
	}
	if e != nil {
		e = errors.Join(e, t.s.Audit.Finish(ev, e))
		return "", nil, nil, e
	}
	return query, args, ev, nil
}
func (t *ActionTx) Exec(ctx context.Context, query string, args ...any) (count int64, err error) {
	query, args, ev, err := t.prepare(ctx, query, true, args)
	if err != nil {
		return 0, err
	}
	defer func() {
		if ev != nil {
			ev.Rows = int(count)
		}
		err = errors.Join(err, t.s.Audit.Finish(ev, err))
	}()
	r, e := t.tx.ExecContext(ctx, query, args...)
	if e != nil {
		return 0, e
	}
	return r.RowsAffected()
}
func (t *ActionTx) Query(ctx context.Context, query string, maxRows int, args ...any) (out *QueryResult, err error) {
	query, args, ev, err := t.prepare(ctx, query, false, args)
	if err != nil {
		return nil, err
	}
	defer func() {
		if ev != nil && out != nil {
			ev.Rows = out.RowCount
			ev.Truncated = out.Truncated
		}
		err = errors.Join(err, t.s.Audit.Finish(ev, err))
	}()
	rows, e := t.tx.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	types, e := rows.ColumnTypes()
	if e != nil {
		return nil, e
	}
	out = &QueryResult{Columns: cols, Rows: []map[string]any{}}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	maxRows = min(max(1, maxRows), 1000)
	for rows.Next() {
		if len(out.Rows) == maxRows {
			out.Truncated = true
			break
		}
		if e = rows.Scan(ptrs...); e != nil {
			return nil, e
		}
		row := map[string]any{}
		for i, n := range cols {
			v, e := jsonValue(values[i])
			if e != nil {
				return nil, e
			}
			typ := strings.ToUpper(types[i].DatabaseTypeName())
			if v != nil && (strings.Contains(typ, "BIGINT") || strings.Contains(typ, "DECIMAL") || strings.Contains(typ, "NUMERIC")) {
				if _, lossy := v.(float64); lossy {
					return nil, fmt.Errorf("字段 %s 的精确数值被驱动转为浮点，请在 SQL 中转为文本", n)
				}
				v = fmt.Sprint(v)
			} else if x, ok := v.(int64); ok && (x > 9007199254740991 || x < -9007199254740991) {
				v = strconv.FormatInt(x, 10)
			}
			row[n] = v
		}
		out.Rows = append(out.Rows, row)
	}
	out.RowCount = len(out.Rows)
	return out, rows.Err()
}
