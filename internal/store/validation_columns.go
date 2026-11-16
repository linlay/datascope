package store

import (
	"context"
	"fmt"
	"strings"

	"datascope-server/internal/dialect"
	"datascope-server/internal/requesttiming"
)

// ValidationColumns checks live readability and column names without loading
// comments, indexes, foreign keys or DDL that policy validation does not use.
func (s *Store) ValidationColumns(ctx context.Context, table string) ([]string, error) {
	defer requesttiming.Start(ctx, "validation_columns")()
	if s.engine == nil {
		schema, err := s.Describe(ctx, table)
		if err != nil {
			return nil, err
		}
		columns := make([]string, len(schema.Columns))
		for i, col := range schema.Columns {
			columns[i] = col.Name
		}
		return columns, nil
	}
	owner, name := "", table
	if strings.Contains(table, ".") {
		owner, name = dialect.SplitTable(table, "")
	}
	physical, err := s.engine.Resolve(owner, name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, table)
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	tx, err := s.beginReadOnly(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT * FROM "+s.engine.Table(physical)+" WHERE 1=0")
	if err != nil {
		return nil, fmt.Errorf("表 %s 无读取权限: %w", physical, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}
