package sqlinspect

import "testing"

func TestDiagnosticContract(t *testing.T) {
	for _, q := range []string{"SHOW TABLES", "show columns from customers;", "SHOW INDEXES IN sales.customers", "DESC `customers`", `DESCRIBE "sales"."customers"`, "EXPLAIN SELECT 'a;--b'", "EXPLAIN QUERY PLAN WITH x AS (SELECT 1) SELECT * FROM x"} {
		if c, e := Parse(q); e != nil || c == nil {
			t.Errorf("%s: %v", q, e)
		}
	}
	for _, q := range []string{"SHOW DATABASES", "SHOW PROCESSLIST", "SHOW TABLES; SELECT 1", "SHOW COLUMNS FROM customers WHERE 1=1", "DESC customers; DELETE FROM customers", "EXPLAIN ANALYZE SELECT 1", "EXPLAIN (ANALYZE true) SELECT 1", "EXPLAIN UPDATE customers SET x=1", "EXPLAIN SELECT 1; DELETE FROM customers", "EXPLAIN EXPLAIN SELECT 1"} {
		if _, e := Parse(q); e == nil {
			t.Errorf("accepted %s", q)
		}
	}
}

func TestExplainPreservesQuotedInput(t *testing.T) {
	c, e := Parse(`EXPLAIN SELECT 'a''b', "quoted" FROM customers`)
	if e != nil || c.Query != `SELECT 'a''b', "quoted" FROM customers` {
		t.Fatalf("%+v %v", c, e)
	}
}
