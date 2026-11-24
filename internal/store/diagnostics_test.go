package store

import (
	"context"
	"database/sql"
	"datascope-server/tests/testutil"
	"strings"
	"testing"
)

func TestRawDiagnostics(t *testing.T) {
	s, e := Open(testutil.Demo(t), 0)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	for _, q := range []string{"SHOW TABLES", "DESC customers", "SHOW INDEX FROM customers", "EXPLAIN SELECT customer_no FROM customers", "EXPLAIN QUERY PLAN SELECT customer_no FROM customers WHERE customer_no=:arg_number"} {
		r, e := s.RawQueryWithParameters(ctx, q, 1, map[string]any{"arg_number": "C001"})
		if e != nil {
			t.Fatalf("%s: %v", q, e)
		}
		if r.RowCount > 1 || r.EffectiveMaxRows != 1 {
			t.Fatal(r)
		}
	}
	for _, q := range []string{"SHOW GLOBAL VARIABLES", "EXPLAIN ANALYZE SELECT 1", "EXPLAIN DELETE FROM customers", "SHOW TABLES; DELETE FROM customers"} {
		if _, e := s.RawQuery(ctx, q, 10); e == nil {
			t.Errorf("accepted %s", q)
		}
	}
}

func TestNetworkExplainCompilesBindsAndUsesReadOnlyTransaction(t *testing.T) {
	for _, driver := range []string{"mysql", "oceanbase", "postgresql"} {
		t.Run(driver, func(t *testing.T) {
			c := &probeConn{}
			s := probeStore(driver, c)
			defer s.Close()
			_, e := s.Query(context.Background(), "EXPLAIN SELECT :userId", 5, sql.Named("userId", int64(7)))
			if e != nil {
				t.Fatal(e)
			}
			if !strings.HasPrefix(c.query, "EXPLAIN SELECT ") || strings.Contains(c.query, "LIMIT") || len(c.binds) != 1 || c.binds[0].Value != int64(7) || !c.opts.ReadOnly || !c.rolledBack {
				t.Fatalf("%s %+v %+v", c.query, c.binds, c.opts)
			}
		})
	}
}

func TestExplainRejectsUnsupportedDriversAndMasking(t *testing.T) {
	for _, driver := range []string{"oracle", "dameng", "postgresql"} {
		c := &probeConn{}
		s := probeStore(driver, c)
		q := "EXPLAIN SELECT 1"
		if driver == "postgresql" {
			q = "EXPLAIN QUERY PLAN SELECT 1"
		}
		_, err := s.Query(context.Background(), q, 10)
		s.Close()
		if err == nil || c.query != "" {
			t.Fatalf("unsupported diagnostic executed: %s %v", driver, err)
		}
	}
	s := maskingFixture(t)
	for _, q := range []string{"EXPLAIN SELECT mobile FROM customers", "EXPLAIN QUERY PLAN SELECT mobile FROM customers"} {
		if _, err := s.RawQuery(context.Background(), q, 10); err == nil {
			t.Fatal("masked source plan exposed")
		}
	}
}

func TestMetadataUsesRegisteredScopeAndRowCap(t *testing.T) {
	s, err := Open(testutil.Demo(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.RegisteredSchema = []TableSchema{{Name: "customers"}, {Name: "accounts"}}
	r, err := s.RawQuery(context.Background(), "SHOW TABLES", 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.RowCount != 1 || !r.Truncated || r.Rows[0]["table_name"] != "accounts" {
		t.Fatalf("scope/cap incorrect: %+v", r)
	}
	if _, err := s.RawQuery(context.Background(), "DESCRIBE employees", 10); err == nil {
		t.Fatal("unregistered table described")
	}
}
