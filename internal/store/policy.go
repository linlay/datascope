package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var DefaultAsOf = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

// VisibleCustomerIDs independently derives customer visibility from owned or
// scoped fund accounts without using the rewrite configuration.
func (s *Store) VisibleCustomerIDs(ctx context.Context, userID int64, asOf time.Time) ([]string, error) {
	var status, role string
	err := s.db.QueryRowContext(ctx, `SELECT e.status,r.code FROM employees e JOIN roles r ON r.id=e.role_id WHERE e.employee_id=?`, userID).Scan(&status, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return []string{}, nil
	}
	depts := map[string]bool{}
	branches := map[string]bool{}
	date := asOf.UTC().Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `SELECT branch_code,department_code,effective_date,expiry_date FROM employee_scopes WHERE employee_id=?`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var b, d sql.NullString
		var start string
		var end sql.NullString
		if err = rows.Scan(&b, &d, &start, &end); err != nil {
			rows.Close()
			return nil, err
		}
		if start <= date && (!end.Valid || end.String > date) {
			if b.Valid {
				branches[b.String] = true
			}
			if d.Valid {
				depts[d.String] = true
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT f.customer_no,f.manager_id,f.department_code,d.branch_code FROM fund_accounts f JOIN departments d ON d.code=f.department_code ORDER BY f.customer_no`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	seen := map[string]bool{}
	for rows.Next() {
		var id string
		var m int64
		var d, b string
		if err = rows.Scan(&id, &m, &d, &b); err != nil {
			return nil, err
		}
		if !seen[id] && (role == "ADMIN" || m == userID || depts[d] || branches[b]) {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, rows.Err()
}
