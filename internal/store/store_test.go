package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sql-rewrite-server/internal/testutil"
	"testing"
)

func openDemo(t *testing.T) *Store {
	t.Helper()
	st, err := Open(testutil.Demo(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestNormalizeReadOnlySQL(t *testing.T) {
	allowed := map[string]string{
		"SELECT 1":       "SELECT 1",
		"  select 1 ;; ": "select 1",
		"-- note\nWITH x AS (SELECT 1) SELECT * FROM x;": "-- note\nWITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT ';' AS s; -- trailing":                   "SELECT ';' AS s",
		"/* c */ (SELECT 1)":                             "/* c */ (SELECT 1)",
		"VALUES (1), (2)":                                "VALUES (1), (2)",
	}
	for in, want := range allowed {
		got, err := NormalizeReadOnlySQL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeReadOnlySQL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"DELETE FROM customers",
		"PRAGMA writable_schema=ON",
		"ATTACH DATABASE ':memory:' AS x",
		"SELECT 1; SELECT 2",
		"SELECT 1; DROP TABLE users",
		"SELECT 'unterminated",
		"",
	} {
		if _, err := NormalizeReadOnlySQL(in); err == nil {
			t.Errorf("NormalizeReadOnlySQL(%q) succeeded; want error", in)
		}
	}
}

func TestQueryReadOnly(t *testing.T) {
	st := openDemo(t)
	ctx := context.Background()
	res, err := st.Query(ctx, "SELECT COUNT(*) AS n FROM customers", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows[0]["n"] != int64(1500) {
		t.Fatalf("customer count = %v", res.Rows[0]["n"])
	}
	// The guard lets WITH through; the read-only connection must still reject writes.
	if _, err := st.Query(ctx, "WITH x AS (SELECT 1) DELETE FROM customers", 0); err == nil {
		t.Fatal("write through CTE succeeded")
	}
	res, err = st.Query(ctx, "SELECT customer_no FROM customers ORDER BY customer_no", 5)
	if err != nil {
		t.Fatal(err)
	}
	if res.RowCount != 5 || !res.Truncated {
		t.Fatalf("row_count=%d truncated=%v", res.RowCount, res.Truncated)
	}
}

func TestTablesAndDescribe(t *testing.T) {
	st := openDemo(t)
	ctx := context.Background()
	tables, err := st.Tables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 17 {
		t.Fatalf("got %d tables, want 17", len(tables))
	}
	for _, tbl := range tables {
		if tbl.Comment == "" || tbl.RowCount == 0 {
			t.Errorf("table %+v missing comment or rows", tbl)
		}
	}
	schema, err := st.Describe(ctx, "fund_accounts")
	if err != nil {
		t.Fatal(err)
	}
	if schema.Comment != "资金账户" {
		t.Errorf("comment = %q", schema.Comment)
	}
	i := slices.IndexFunc(schema.Columns, func(c Column) bool { return c.Name == "customer_no" })
	if i < 0 || schema.Columns[i].References != "customers.customer_no" || schema.Columns[i].Comment != "客户业务编号" {
		t.Errorf("customer_no column = %+v", schema.Columns)
	}
	if _, err := st.Describe(ctx, "nope"); err == nil {
		t.Error("describe of missing table succeeded")
	}
}

func TestVisibleCustomerIDsMatchesFixtures(t *testing.T) {
	st := openDemo(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "permission_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID       string   `json:"id"`
		UserID   int64    `json:"user_id"`
		Expected []string `json:"expected_customer_ids"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got, err := st.VisibleCustomerIDs(context.Background(), c.UserID, DefaultAsOf)
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		if !slices.Equal(got, c.Expected) {
			t.Errorf("%s: got %v, want %v", c.ID, got, c.Expected)
		}
	}
}
