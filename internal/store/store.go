// Package store provides read-only access to the finance demo SQLite database.
package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

const (
	DefaultMaxRows      = 200
	HardMaxRows         = 1000
	DefaultQueryTimeout = 10 * time.Second
)

// Store wraps a read-only SQLite connection pool.
type Store struct {
	db           *sql.DB
	path         string
	queryTimeout time.Duration
}

// Open opens path in read-only mode with query_only enforced on every
// connection. The file must already exist.
func Open(path string, queryTimeout time.Duration) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     abs,
		RawQuery: "mode=ro&_pragma=query_only(1)&_pragma=foreign_keys(1)",
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("打开 SQLite 数据库 %s 失败: %w", abs, err)
	}
	if queryTimeout <= 0 {
		queryTimeout = DefaultQueryTimeout
	}
	return &Store{db: db, path: abs, queryTimeout: queryTimeout}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Path() string { return s.path }

// QueryResult is a bounded result set.
type QueryResult struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"row_count"`
	Truncated bool             `json:"truncated"`
}

// Query runs a single read-only statement and returns at most maxRows rows.
// args are bind values (e.g. sql.Named) for trusted, server-built SQL.
func (s *Store) Query(ctx context.Context, rawSQL string, maxRows int, args ...any) (*QueryResult, error) {
	stmt, err := NormalizeReadOnlySQL(rawSQL)
	if err != nil {
		return nil, err
	}
	if maxRows <= 0 {
		maxRows = DefaultMaxRows
	}
	maxRows = min(maxRows, HardMaxRows)
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	result, err := s.read(ctx, maxRows, stmt, args...)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("查询超过 %s 被中断", s.queryTimeout)
	}
	return result, err
}

// QueryBool runs trusted SQL returning one value and reports whether it is
// non-zero. Used for configured deny_unless checks.
func (s *Store) QueryBool(ctx context.Context, query string, args ...any) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	var v int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&v); err != nil {
		return false, err
	}
	return v != 0, nil
}

// RawQuery runs any single statement exactly as given, without rewriting or
// statement-type checks. The connection is still read-only. Debug use only.
func (s *Store) RawQuery(ctx context.Context, rawSQL string, maxRows int) (*QueryResult, error) {
	body, rest, err := splitFirstStatement(rawSQL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stripComments(rest)) != "" {
		return nil, errors.New("不支持多条语句")
	}
	if strings.TrimSpace(stripComments(body)) == "" {
		return nil, errors.New("SQL 为空")
	}
	if maxRows <= 0 {
		maxRows = DefaultMaxRows
	}
	maxRows = min(maxRows, HardMaxRows)
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	result, err := s.read(ctx, maxRows, body)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("查询超过 %s 被中断", s.queryTimeout)
	}
	return result, err
}

// TableNames lists user tables in name order.
func (s *Store) TableNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// read executes trusted SQL with bound args. Callers outside this package must
// go through Query.
func (s *Store) read(ctx context.Context, maxRows int, query string, args ...any) (*QueryResult, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := &QueryResult{Columns: columns, Rows: []map[string]any{}}
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	for rows.Next() {
		if len(result.Rows) == maxRows {
			result.Truncated = true
			break
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(columns))
		for i, name := range columns {
			row[name] = jsonValue(values[i])
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.RowCount = len(result.Rows)
	return result, nil
}

func jsonValue(v any) any {
	switch x := v.(type) {
	case []byte:
		if utf8.Valid(x) {
			return string(x)
		}
		return "base64:" + base64.StdEncoding.EncodeToString(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return x
	}
}

// Table describes one user table.
type Table struct {
	Name     string `json:"name"`
	Comment  string `json:"comment"`
	RowCount int64  `json:"row_count"`
}

// Tables lists user tables with their Chinese comments and row counts.
func (s *Store) Tables(ctx context.Context) ([]Table, error) {
	query := `
		SELECT m.name, COALESCE(c.comment_zh, '')
		FROM sqlite_schema m
		LEFT JOIN schema_comments c ON c.table_name = m.name AND c.column_name = ''
		WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
		ORDER BY m.name`
	var hasComments int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='schema_comments'").Scan(&hasComments); err != nil {
		return nil, err
	}
	if hasComments == 0 {
		query = "SELECT name, '' FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	var tables []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Comment); err != nil {
			rows.Close()
			return nil, err
		}
		tables = append(tables, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range tables {
		// Table names come from sqlite_schema, so quoting them is safe.
		q := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(tables[i].Name))
		if err := s.db.QueryRowContext(ctx, q).Scan(&tables[i].RowCount); err != nil {
			return nil, err
		}
	}
	return tables, nil
}

// Column describes one column of a table.
type Column struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	NotNull    bool    `json:"not_null"`
	PrimaryKey bool    `json:"primary_key"`
	Default    *string `json:"default,omitempty"`
	References string  `json:"references,omitempty"`
	Comment    string  `json:"comment"`
}

// TableSchema is the full description of a table.
type TableSchema struct {
	Name    string   `json:"name"`
	Comment string   `json:"comment"`
	Columns []Column `json:"columns"`
	Indexes []string `json:"indexes"`
	DDL     string   `json:"ddl"`
}

var ErrTableNotFound = errors.New("表不存在")

// Describe returns columns, foreign keys, indexes and comments for table.
func (s *Store) Describe(ctx context.Context, table string) (*TableSchema, error) {
	out := &TableSchema{Name: table, Columns: []Column{}, Indexes: []string{}}
	err := s.db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = ? AND name NOT LIKE 'sqlite_%'`,
		table).Scan(&out.DDL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, table)
	}
	if err != nil {
		return nil, err
	}

	comments := map[string]string{}
	rows, err := s.db.QueryContext(ctx, `SELECT column_name, comment_zh FROM schema_comments WHERE table_name = ?`, table)
	if err != nil {
		var count int
		if check := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE name='schema_comments'").Scan(&count); check != nil || count != 0 {
			return nil, err
		}
		rows, err = s.db.QueryContext(ctx, "SELECT '' AS column_name, '' AS comment_zh WHERE 0")
		if err != nil {
			return nil, err
		}
	}
	for rows.Next() {
		var col, comment string
		if err := rows.Scan(&col, &comment); err != nil {
			rows.Close()
			return nil, err
		}
		comments[col] = comment
	}
	rows.Close()
	out.Comment = comments[""]

	refs := map[string]string{}
	rows, err = s.db.QueryContext(ctx, `SELECT "from", "table", "to" FROM pragma_foreign_key_list(?)`, table)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var from, target string
		var to sql.NullString
		if err := rows.Scan(&from, &target, &to); err != nil {
			rows.Close()
			return nil, err
		}
		refs[from] = target + "." + cmp.Or(to.String, "id")
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Column
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&c.Name, &c.Type, &c.NotNull, &dflt, &pk); err != nil {
			rows.Close()
			return nil, err
		}
		if dflt.Valid {
			c.Default = &dflt.String
		}
		c.PrimaryKey = pk > 0
		c.References = refs[c.Name]
		c.Comment = comments[c.Name]
		out.Columns = append(out.Columns, c)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL ORDER BY name`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out.Indexes = append(out.Indexes, name)
	}
	return out, rows.Err()
}

func quoteIdent(name string) string {
	out := []byte{'"'}
	for i := 0; i < len(name); i++ {
		if name[i] == '"' {
			out = append(out, '"')
		}
		out = append(out, name[i])
	}
	return string(append(out, '"'))
}
