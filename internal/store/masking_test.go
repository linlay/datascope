package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func maskingFixture(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "business.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE customers(id INTEGER, mobile TEXT, name TEXT, secret TEXT); INSERT INTO customers VALUES(1,'13812345678','张三丰','secret-value'),(2,NULL,'李','another-secret'); CREATE TABLE orders(customer_id INTEGER, amount INTEGER); INSERT INTO orders VALUES(1,99);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.RegisteredSchema = []TableSchema{
		{Name: "customers", Columns: []Column{{Name: "id"}, {Name: "mobile", Masking: &Masking{Type: "partial", KeepPrefix: 3, KeepSuffix: 4}}, {Name: "name", Masking: &Masking{Type: "partial", KeepPrefix: 1}}, {Name: "secret", Masking: &Masking{Type: "omit"}}}},
		{Name: "orders", Columns: []Column{{Name: "customer_id"}, {Name: "amount"}}},
	}
	return st
}

func TestMaskingQueryLineageAndRawQuery(t *testing.T) {
	st := maskingFixture(t)
	for _, query := range []string{
		`SELECT mobile AS contact FROM customers WHERE id=1`,
		`SELECT c.mobile AS contact FROM customers c WHERE id=1`,
		`SELECT c.mobile AS contact FROM customers c JOIN orders o ON c.id=o.customer_id`,
		`SELECT contact FROM (SELECT mobile AS contact FROM customers WHERE id=1) c`,
		`SELECT * FROM (SELECT mobile AS contact FROM customers WHERE id=1)`,
		`WITH c AS (SELECT mobile AS contact FROM customers WHERE id=1) SELECT contact FROM c`,
		`WITH c(contact) AS (SELECT mobile FROM customers WHERE id=1) SELECT contact FROM c`,
		`WITH c AS (SELECT mobile FROM customers WHERE id=1), d AS (SELECT mobile AS contact FROM c) SELECT contact FROM d`,
	} {
		t.Run(query, func(t *testing.T) {
			for _, raw := range []bool{false, true} {
				var out *QueryResult
				var err error
				if raw {
					out, err = st.RawQuery(context.Background(), query, 10)
				} else {
					out, err = st.Query(context.Background(), query, 10)
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(out.Rows) != 1 || out.Rows[0]["contact"] != "138****5678" {
					t.Fatalf("unmasked: %+v", out)
				}
			}
		})
	}
	out, err := st.RawQuery(context.Background(), `SELECT c.* FROM customers c ORDER BY id`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Columns) != 3 || out.Rows[0]["name"] != "张**" || out.Rows[1]["name"] != "*" || out.Rows[1]["mobile"] != nil {
		t.Fatalf("bad values: %+v", out)
	}
	payload, _ := json.Marshal(out)
	if strings.Contains(string(payload), "secret") {
		t.Fatalf("hidden field leaked: %s", payload)
	}
	out, err = st.RawQuery(context.Background(), `SELECT secret FROM customers WHERE id=1`, 10)
	if err != nil || len(out.Columns) != 0 || len(out.Rows) != 1 || len(out.Rows[0]) != 0 {
		t.Fatalf("omit only result: %+v %v", out, err)
	}
	out, err = st.Query(context.Background(), `SELECT count(*) AS total, sum(id) AS n FROM customers`, 10)
	if err != nil || out.Rows[0]["total"] != int64(2) {
		t.Fatalf("safe aggregation: %+v %v", out, err)
	}
}

func TestMaskingRejectsDerivedAndAmbiguousOutputs(t *testing.T) {
	st := maskingFixture(t)
	for _, query := range []string{
		`SELECT substr(mobile,1,3) FROM customers`,
		`SELECT 'phone:'||mobile FROM customers`,
		`SELECT hex(mobile) FROM customers`,
		`SELECT CASE WHEN mobile='13812345678' THEN 'yes' ELSE 'no' END FROM customers`,
		`SELECT json_object('value',mobile) FROM customers`,
		`SELECT secret||'' FROM customers`,
		`SELECT max(mobile) FROM customers`,
		`SELECT (SELECT mobile FROM customers LIMIT 1)`,
		`SELECT id AS contact, mobile AS contact FROM customers`,
		`SELECT id FROM customers UNION SELECT mobile FROM customers`,
		`SELECT * FROM customers NATURAL JOIN orders`,
		`SELECT row_number() OVER (ORDER BY mobile) FROM customers`,
		`SELECT mobile FROM customers WHERE id=2 UNION ALL SELECT mobile FROM customers WHERE id=1`,
		`SELECT mobile AS value FROM customers WHERE 1=0 UNION ALL SELECT id AS value FROM customers`,
	} {
		t.Run(query, func(t *testing.T) {
			out, err := st.RawQuery(context.Background(), query, 10)
			if err == nil || out != nil {
				t.Fatalf("unsafe output: %+v %v", out, err)
			}
		})
	}
}

func TestMaskingBrowseAndRegisteredWildcard(t *testing.T) {
	st := maskingFixture(t)
	page, err := st.Browse(context.Background(), BrowseOptions{Table: "customers", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Columns) != 3 || page.Rows[0]["mobile"] != "138****5678" || page.Total != 2 {
		t.Fatalf("unmasked browse: %+v", page)
	}
	// The snapshot is deliberately missing a live field. Wildcards must use the
	// registered definition rather than include an unreviewed new column.
	st.RegisteredSchema[0].Columns = st.RegisteredSchema[0].Columns[:3]
	out, err := st.RawQuery(context.Background(), `SELECT * FROM customers WHERE id=1`, 10)
	if err != nil || len(out.Columns) != 3 {
		t.Fatalf("wildcard included drift: %+v %v", out, err)
	}
	if out, err := st.Browse(context.Background(), BrowseOptions{Table: "customers"}); err == nil || out != nil {
		t.Fatal("browse ignored unregistered columns")
	}
}

func TestMaskingUnicodeFullHMACAndValidation(t *testing.T) {
	for _, tc := range []struct {
		value any
		rule  Masking
		want  any
	}{
		{"欧阳修", Masking{Type: "partial", KeepPrefix: 2}, "欧阳*"},
		{"138", Masking{Type: "partial", KeepPrefix: 3, KeepSuffix: 4}, "***"},
		{"", Masking{Type: "partial", KeepPrefix: 1}, ""},
		{nil, Masking{Type: "full"}, nil},
		{123456, Masking{Type: "full"}, "******"},
	} {
		got, err := tc.rule.apply(tc.value, nil, "")
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %v %v", tc, got, err)
		}
	}
	for _, m := range []*Masking{{Type: "bad"}, {Type: "partial", KeepPrefix: -1}, {Type: "full", KeepSuffix: 1}, {Type: "partial", KeepSuffix: 101}} {
		if m.Validate() == nil {
			t.Fatal("invalid rule accepted", m)
		}
	}
	st := maskingFixture(t)
	st.RegisteredSchema[0].Columns[1].Masking = &Masking{Type: "hmac"}
	if out, err := st.RawQuery(context.Background(), `SELECT mobile FROM customers`, 1); err == nil || out != nil {
		t.Fatal("missing key allowed")
	}
	st.MaskingKey = []byte(strings.Repeat("k", 32))
	query := `SELECT mobile AS a, mobile AS b FROM customers WHERE id=1`
	one, err := st.RawQuery(context.Background(), query, 1)
	if err != nil {
		t.Fatal(err)
	}
	two, err := st.RawQuery(context.Background(), query, 1)
	if err != nil {
		t.Fatal(err)
	}
	if one.Rows[0]["a"] != two.Rows[0]["a"] || one.Rows[0]["a"] != one.Rows[0]["b"] || !strings.HasPrefix(one.Rows[0]["a"].(string), "h1:") {
		t.Fatal(one, two)
	}
	m := Masking{Type: "hmac"}
	a, _ := m.apply("13812345678", st.MaskingKey, "customers.mobile")
	b, _ := m.apply("13812345678", st.MaskingKey, "other.mobile")
	if a == b {
		t.Fatal("field domains not isolated")
	}
}
