package store

import (
	"context"
	"errors"
	parser "github.com/rqlite/sql"
	"io"
	"strings"
)

// ExecuteSchema accepts one structural statement; query and action pools retain
// their existing read-only and DML-only contracts.
func (s *Store) ExecuteSchema(ctx context.Context, query string) error {
	p := parser.NewParser(strings.NewReader(query))
	n, err := p.ParseStatement()
	if err != nil {
		return err
	}
	if next, end := p.ParseStatement(); next != nil || !errors.Is(end, io.EOF) {
		return errors.New("每次只允许一条结构 SQL")
	}
	var schema *parser.Ident
	switch x := n.(type) {
	case *parser.CreateTableStatement:
		schema = x.Schema
		if x.Temp.IsValid() || x.Select != nil {
			return errors.New("仅支持普通 CREATE TABLE 字段定义")
		}
	case *parser.AlterTableStatement:
		schema = x.Schema
	case *parser.DropTableStatement:
		schema = x.Schema
	case *parser.CreateIndexStatement:
		schema = x.Schema
	case *parser.DropIndexStatement:
		schema = x.Schema
	default:
		return errors.New("仅支持 CREATE/ALTER/DROP TABLE 和 CREATE/DROP INDEX")
	}
	if schema != nil && !strings.EqualFold(schema.Name, "main") {
		return errors.New("仅允许修改 main 中的表和索引")
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultQueryTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, n.String()); err != nil {
		return err
	}
	return tx.Commit()
}
