package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func (s *Store) remoteBrowse(ctx context.Context, opts BrowseOptions) (*BrowseResult, error) {
	schema, err := s.Describe(ctx, opts.Table)
	if err != nil {
		return nil, err
	}
	columns := []string{}
	for _, c := range schema.Columns {
		columns = append(columns, c.Name)
	}
	if len(columns) == 0 {
		return nil, ErrTableNotFound
	}
	if opts.Limit <= 0 {
		opts.Limit = DefaultPageSize
	}
	opts.Limit = min(opts.Limit, MaxPageSize)
	opts.Offset = max(opts.Offset, 0)
	args := []any{}
	where := []string{}
	bind := func(v any) string {
		args = append(args, v)
		switch s.Driver() {
		case "postgresql":
			return fmt.Sprintf("$%d", len(args))
		case "oracle", "dameng":
			return fmt.Sprintf(":%d", len(args))
		default:
			return "?"
		}
	}
	if opts.FilterColumn != "" {
		if !slices.Contains(columns, opts.FilterColumn) {
			return nil, fmt.Errorf("表没有字段 %s", opts.FilterColumn)
		}
		where = append(where, s.engine.Quote(opts.FilterColumn)+" = "+bind(opts.FilterValue))
	}
	if opts.Search != "" {
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(opts.Search) + "%"
		likes := []string{}
		for _, c := range columns {
			typ := "TEXT"
			switch s.Driver() {
			case "mysql":
				typ = "CHAR"
			case "oracle":
				typ = "VARCHAR2(4000)"
			case "dameng":
				typ = "VARCHAR(4000)"
			}
			likes = append(likes, "CAST("+s.engine.Quote(c)+" AS "+typ+") LIKE "+bind(pattern)+" ESCAPE '!'")
		}
		where = append(where, "("+strings.Join(likes, " OR ")+")")
	}
	from := " FROM " + s.engine.Table(schema.Name)
	if len(where) > 0 {
		from += " WHERE " + strings.Join(where, " AND ")
	}
	order := columns[0]
	if opts.OrderBy != "" {
		if !slices.Contains(columns, opts.OrderBy) {
			return nil, fmt.Errorf("表没有字段 %s", opts.OrderBy)
		}
		order = opts.OrderBy
	}
	order = s.engine.Quote(order)
	if opts.Desc {
		order += " DESC"
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	result := &BrowseResult{Table: schema.Name, Columns: columns, Rows: []map[string]any{}, Limit: opts.Limit, Offset: opts.Offset}
	count, err := s.readQuery(ctx, 1, "SELECT COUNT(*) AS "+s.engine.Quote("total")+from, true, args...)
	if err != nil {
		return nil, err
	}
	if len(count.Rows) > 0 {
		result.Total, err = strconv.ParseInt(fmt.Sprint(count.Rows[0]["total"]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("无法解析行数: %w", err)
		}
	}
	pageSQL := "SELECT *" + from + " ORDER BY " + order
	if s.Driver() == "oracle" {
		pageSQL += fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", opts.Offset, opts.Limit)
	} else {
		pageSQL += fmt.Sprintf(" LIMIT %d OFFSET %d", opts.Limit, opts.Offset)
	}
	page, err := s.readQuery(ctx, opts.Limit, pageSQL, true, args...)
	if err != nil {
		return nil, err
	}
	result.Rows = page.Rows
	return result, nil
}
