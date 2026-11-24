package store

import (
	"context"
	"datascope-server/internal/audit"
	"datascope-server/internal/dialect"
	"datascope-server/internal/sqlinspect"
	"fmt"
	"slices"
	"strings"
)

// InspectMetadata synthesizes a stable tabular result; vendor SHOW SQL is never
// executed. A nil visibility map means full access within the registered scope.
// Non-nil column lists hide indexes, which can mention non-public columns.
func (s *Store) InspectMetadata(ctx context.Context, query string, c *sqlinspect.Command, visible map[string][]string, maxRows int) (out *QueryResult, err error) {
	event, err := s.Audit.Start(ctx, "sql.execute", query)
	if err != nil {
		return nil, err
	}
	if event != nil {
		event.RowsKind = "returned"
	}
	defer func() {
		if event != nil && out != nil {
			event.Rows = out.RowCount
			event.Truncated = out.Truncated
		}
		err = audit.Join(err, s.Audit.Finish(event, err))
	}()
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	defer func() { err = OperationError(ctx, "结构元数据读取", s.queryTimeout, err) }()
	out = &QueryResult{Rows: []map[string]any{}}
	out.SetRowLimit(maxRows)
	add := func(row map[string]any) {
		if len(out.Rows) < out.EffectiveMaxRows {
			out.Rows = append(out.Rows, row)
		} else {
			out.Truncated = true
		}
	}
	if c.Kind == "tables" {
		out.Columns = []string{"table_name"}
		names, e := s.inspectionNames(ctx)
		if e != nil {
			return nil, e
		}
		slices.Sort(names)
		for _, name := range names {
			if visible != nil {
				if _, ok := visible[strings.ToLower(name)]; !ok {
					continue
				}
			}
			add(map[string]any{"table_name": name})
		}
	} else {
		name := c.Table
		if s.Driver() != "sqlite" && !strings.Contains(name, ".") {
			name = s.DefaultSchema() + "." + name
		}
		var columns []string
		if visible != nil {
			var ok bool
			columns, ok = visible[strings.ToLower(name)]
			if !ok {
				return nil, fmt.Errorf("表不可访问")
			}
		}
		// Resolve canonical names before metadata access (including full-query mode).
		names, e := s.inspectionNames(ctx)
		if e != nil {
			return nil, e
		}
		found := false
		for _, n := range names {
			if strings.EqualFold(n, name) {
				name = n
				found = true
				break
			}
		}
		if !found {
			return nil, ErrTableNotFound
		}
		schema, e := s.Describe(ctx, name)
		if e != nil {
			return nil, e
		}
		if c.Kind == "indexes" {
			out.Columns = []string{"index"}
			if columns == nil {
				for _, index := range schema.Indexes {
					add(map[string]any{"index": index})
				}
			}
		} else {
			out.Columns = []string{"field", "type", "not_null", "primary_key", "comment"}
			for _, col := range schema.Columns {
				if columns != nil && !slices.ContainsFunc(columns, func(n string) bool { return strings.EqualFold(n, col.Name) }) {
					continue
				}
				add(map[string]any{"field": col.Name, "type": col.Type, "not_null": col.NotNull, "primary_key": col.PrimaryKey, "comment": col.Comment})
			}
		}
	}
	out.RowCount = len(out.Rows)
	return out, nil
}

// Use the immutable registered snapshot rather than rediscovering tables and
// mutating the shared dialect mapping on each cached service call.
func (s *Store) inspectionNames(ctx context.Context) ([]string, error) {
	if s.RegisteredSchema != nil {
		names := make([]string, 0, len(s.RegisteredSchema))
		for _, table := range s.RegisteredSchema {
			names = append(names, table.Name)
		}
		return names, nil
	}
	if s.engine != nil {
		names := []string{}
		for _, name := range s.engine.Tables {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
		return names, nil
	}
	return s.TableNames(ctx)
}

func (s *Store) prepareExplain(query string, args ...any) (string, []any, error) {
	c, err := sqlinspect.Parse(query)
	if err != nil {
		return "", nil, err
	}
	if c == nil || c.Kind != "explain" {
		return "", nil, fmt.Errorf("需要 EXPLAIN SELECT")
	}
	switch dialect.Family(s.Driver()) {
	case "sqlite":
	case "mysql", "postgresql":
		if c.Prefix != "EXPLAIN " {
			return "", nil, fmt.Errorf("EXPLAIN QUERY PLAN 仅支持 SQLite")
		}
	default:
		return "", nil, fmt.Errorf("当前数据库暂不支持 EXPLAIN")
	}
	inner, binds, err := s.Prepare(c.Query, args...)
	if err != nil {
		return "", nil, err
	}
	return c.Prefix + inner, binds, nil
}

func (s *Store) explain(ctx context.Context, query string, maxRows int, args ...any) (*QueryResult, error) {
	if original, rewritten := audit.SQL(ctx); original == "" {
		ctx = audit.WithSQL(ctx, query, rewritten)
	}
	// Plan output can contain literals and expressions. Until plan-specific
	// masking exists, reject diagnostics on sources with masking enabled.
	if s.hasMasking() {
		return nil, fmt.Errorf("配置字段脱敏的数据库暂不支持 EXPLAIN")
	}
	query, args, err := s.prepareExplain(query, args...)
	if err != nil {
		return nil, err
	}
	return s.readQuery(ctx, maxRows, query, true, args...)
}
