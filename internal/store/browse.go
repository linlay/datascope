package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 500
)

// BrowseOptions selects one page of a table. Column names are validated
// against the table schema before they are placed into SQL.
type BrowseOptions struct {
	Table        string
	Limit        int
	Offset       int
	OrderBy      string
	Desc         bool
	Search       string // substring match over every column
	FilterColumn string // exact match, e.g. following a foreign key
	FilterValue  string
}

// BrowseResult is one page of rows plus the total matching row count.
type BrowseResult struct {
	Table   string           `json:"table"`
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Total   int64            `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
}

// Browse returns a page of rows from a table, unfiltered by business
// permissions (the super-admin view).
func (s *Store) Browse(ctx context.Context, opts BrowseOptions) (*BrowseResult, error) {
	if s.engine != nil {
		return s.remoteBrowse(ctx, opts)
	}
	schema, err := s.Describe(ctx, opts.Table)
	if err != nil {
		return nil, err
	}
	columns := make([]string, len(schema.Columns))
	for i, c := range schema.Columns {
		columns[i] = c.Name
	}
	if opts.Limit <= 0 {
		opts.Limit = DefaultPageSize
	}
	opts.Limit = min(opts.Limit, MaxPageSize)
	opts.Offset = max(opts.Offset, 0)

	var where []string
	var args []any
	if opts.FilterColumn != "" {
		if !slices.Contains(columns, opts.FilterColumn) {
			return nil, fmt.Errorf("表 %s 没有字段 %s", opts.Table, opts.FilterColumn)
		}
		where = append(where, quoteIdent(opts.FilterColumn)+" = ?")
		args = append(args, opts.FilterValue)
	}
	if opts.Search != "" {
		pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(opts.Search) + "%"
		var likes []string
		for _, c := range columns {
			likes = append(likes, "CAST("+quoteIdent(c)+` AS TEXT) LIKE ? ESCAPE '\'`)
			args = append(args, pattern)
		}
		where = append(where, "("+strings.Join(likes, " OR ")+")")
	}
	from := " FROM " + quoteIdent(schema.Name)
	if len(where) > 0 {
		from += " WHERE " + strings.Join(where, " AND ")
	}

	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	result := &BrowseResult{Table: schema.Name, Columns: columns, Limit: opts.Limit, Offset: opts.Offset}
	count, err := s.read(ctx, 1, "SELECT COUNT(*) AS total"+from, args...)
	if err != nil {
		return nil, err
	}
	if len(count.Rows) > 0 {
		if n, ok := count.Rows[0]["total"].(int64); ok {
			result.Total = n
		}
	}

	order := quoteIdent(columns[0])
	if opts.OrderBy != "" {
		if !slices.Contains(columns, opts.OrderBy) {
			return nil, fmt.Errorf("表 %s 没有字段 %s", opts.Table, opts.OrderBy)
		}
		order = quoteIdent(opts.OrderBy)
	}
	if opts.Desc {
		order += " DESC"
	}
	page, err := s.read(ctx, opts.Limit, "SELECT *"+from+" ORDER BY "+order+" LIMIT ? OFFSET ?",
		append(args, opts.Limit, opts.Offset)...)
	if err != nil {
		return nil, err
	}
	result.Rows = page.Rows
	return result, nil
}

// UserSummary is one system user with role names, for identity pickers.
type UserSummary struct {
	AuthVersion    int64   `json:"-"`
	ID             int64   `json:"id"`
	EmpNo          string  `json:"emp_no"`
	Name           string  `json:"name"`
	RoleCode       string  `json:"role_code"`
	BranchCode     *string `json:"branch_code"`
	DepartmentCode *string `json:"department_code"`
	Status         string  `json:"status"`
	Roles          string  `json:"roles"`
}

// Users lists every system user with their role names.
func (s *Store) Users(ctx context.Context) ([]UserSummary, error) {
	if s == nil || s.engine != nil {
		return []UserSummary{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
 SELECT e.employee_id,e.emp_no,e.name,r.code,e.branch_code,e.department_code,e.status,r.name
 FROM employees e JOIN roles r ON r.id=e.role_id ORDER BY e.employee_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []UserSummary{}
	for rows.Next() {
		var u UserSummary
		if err := rows.Scan(&u.ID, &u.EmpNo, &u.Name, &u.RoleCode, &u.BranchCode, &u.DepartmentCode, &u.Status, &u.Roles); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
