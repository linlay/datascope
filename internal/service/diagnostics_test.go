package service

import (
	"context"
	"strings"
	"testing"
)

func TestDiagnosticsRespectPolicies(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	pol, _ := s.Config().Lookup("customers")
	pol.Columns = []string{"customer_no"}
	r, e := s.Query(ctx, "SHOW COLUMNS FROM customers", 1, 1000)
	if e != nil {
		t.Fatal(e)
	}
	if r.RowCount != 1 || r.Rows[0]["field"] != "customer_no" {
		t.Fatalf("leaked columns: %+v", r.QueryResult)
	}
	r, e = s.Query(ctx, "SHOW INDEX FROM customers", 1, 1000)
	if e != nil || r.RowCount != 0 {
		t.Fatalf("leaked indexes: %+v %v", r, e)
	}
	disabled := false
	pol.Enabled = &disabled
	if _, e = s.Query(ctx, "DESC customers", 1, 10); e == nil {
		t.Fatal("disabled table described")
	}
	r, e = s.Query(ctx, "SHOW TABLES", 1, 1000)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range r.Rows {
		if row["table_name"] == "customers" {
			t.Fatal("disabled table listed")
		}
	}
}

func TestExplainRewritesAndBinds(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	r, e := s.QueryWithParameters(ctx, "EXPLAIN QUERY PLAN SELECT customer_no FROM customers WHERE customer_no=:arg_number", 1, 100, map[string]any{"arg_number": "C001"})
	if e != nil {
		t.Fatal(e)
	}
	if r.RowCount == 0 || !strings.HasPrefix(r.RewrittenSQL, "EXPLAIN QUERY PLAN ") || len(r.Binds) < 2 {
		t.Fatalf("missing rewrite/binds: %+v", r)
	}
	for _, q := range []string{"EXPLAIN SELECT * FROM employee_scopes", "EXPLAIN ANALYZE SELECT * FROM customers", "EXPLAIN DELETE FROM customers", "SHOW PROCESSLIST"} {
		if _, e = s.Query(ctx, q, 1, 100); e == nil {
			t.Errorf("accepted %s", q)
		}
	}
}
