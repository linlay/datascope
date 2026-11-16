package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestBoundedQueryPreservesAggregateAndOffset(t *testing.T) {
	c := &probeConn{}
	s := probeStore("mysql", c)
	defer s.Close()
	for _, tc := range []struct{ sql, want string }{
		{"SELECT SUM(1)", "SELECT SUM( 1 ) LIMIT 3"},
		{"SELECT 1 LIMIT 1 OFFSET 7", "SELECT 1 LIMIT 1 OFFSET 7"},
		{"SELECT 1 LIMIT 100 OFFSET 7", "SELECT 1 LIMIT 3 OFFSET 7"},
	} {
		if _, err := s.Query(context.Background(), tc.sql, 2); err != nil {
			t.Fatal(err)
		}
		if c.query != tc.want {
			t.Fatalf("got %q want %q", c.query, tc.want)
		}
	}
}

func TestRowLimitReportsAndSQLSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE customers(customer_no INTEGER); WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<1500) INSERT INTO customers SELECT x FROM n")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, tc := range []struct {
		requested, effective int
		source               string
	}{
		{0, 200, "default"}, {500, 500, "request"}, {5000, 1000, "hard_limit"},
	} {
		r, err := s.Query(ctx, "SELECT customer_no FROM customers ORDER BY customer_no", tc.requested)
		if err != nil {
			t.Fatal(err)
		}
		if r.EffectiveMaxRows != tc.effective || r.RowCount != tc.effective || r.LimitSource != tc.source || !r.Truncated {
			t.Fatalf("bad cap: %+v", r)
		}
	}
	for _, tc := range []struct {
		query     string
		count     int
		truncated bool
	}{
		{"SELECT customer_no FROM customers ORDER BY customer_no LIMIT 2", 2, false},
		{"SELECT customer_no FROM customers ORDER BY customer_no LIMIT 20 OFFSET 1499", 1, false},
		{"SELECT COUNT(*) AS n FROM customers", 1, false},
		{"WITH x AS (SELECT customer_no FROM customers LIMIT 10) SELECT * FROM x", 2, true},
		{"VALUES (1), (2), (3)", 2, true},
		{"SELECT customer_no FROM customers -- trailing comment", 2, true},
		{"SELECT customer_no FROM customers LIMIT 1499, 20", 1, false},
	} {
		r, err := s.RawQuery(ctx, tc.query, 2)
		if err != nil {
			t.Fatalf("%s: %v", tc.query, err)
		}
		if r.RowCount != tc.count || r.Truncated != tc.truncated {
			t.Fatalf("%s: %+v", tc.query, r)
		}
		if tc.query == "SELECT COUNT(*) AS n FROM customers" && r.Rows[0]["n"] != int64(1500) {
			t.Fatal("aggregate input was limited")
		}
	}
	if _, err := s.Query(ctx, "SELECT missing_column FROM customers", 2); err == nil {
		t.Fatal("unknown column became a quoted string literal")
	}
}
