package dialect_test

import (
	"database/sql"
	"strings"
	"testing"

	"datascope-server/internal/dialect"
	"datascope-server/internal/rewrite"
)

func crossSchemaRewriter(t *testing.T) (*rewrite.Rewriter, *rewrite.Config) {
	t.Helper()
	cfg, err := rewrite.ParseConfig([]byte(`version: 1
schemes: [{scheme: CUSTOM, name: Custom}]
tables:
 - table: sales.orders
   scheme: CUSTOM
   columns: [order_id, customer_id]
   sql: SELECT * FROM sales.orders WHERE owner_id = ${userId}
 - table: crm.customers
   scheme: CUSTOM
   columns: [customer_id, customer_name]
   passthrough: true
`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := rewrite.New(cfg, []string{"sales.orders", "crm.customers"})
	if err != nil {
		t.Fatal(err)
	}
	r.AllowQualified = true
	r.DefaultSchema = "sales"
	r.Columns = map[string][]string{"sales.orders": {"order_id", "customer_id"}, "crm.customers": {"customer_id", "customer_name"}}
	return r, cfg
}
func TestCrossSchemaJoinPoliciesAndExecution(t *testing.T) {
	// Independent relational execution reference, not a vendor integration claim.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{`ATTACH DATABASE ':memory:' AS sales`, `ATTACH DATABASE ':memory:' AS crm`, `CREATE TABLE sales.orders(order_id INTEGER,customer_id INTEGER,owner_id INTEGER,secret TEXT)`, `CREATE TABLE crm.customers(customer_id INTEGER,customer_name TEXT,secret TEXT)`, `INSERT INTO sales.orders VALUES(1,7,1,'a'),(2,8,2,'b')`, `INSERT INTO crm.customers VALUES(7,'Alice','a'),(8,'Bob','b')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	queries := []string{
		`SELECT o.order_id,c.customer_name FROM sales.orders AS o JOIN crm.customers AS c ON c.customer_id=o.customer_id`,
		`SELECT o.order_id,c.customer_name FROM (SELECT order_id,customer_id FROM sales.orders) AS o JOIN crm.customers AS c ON c.customer_id=o.customer_id`,
		`WITH o AS (SELECT order_id,customer_id FROM sales.orders) SELECT o.order_id,c.customer_name FROM o JOIN crm.customers AS c ON c.customer_id=o.customer_id`,
		`SELECT o.order_id,c.customer_name FROM orders AS o JOIN crm.customers AS c ON c.customer_id=o.customer_id`,
	}
	for _, driver := range []string{"mysql", "oceanbase", "postgresql", "oracle", "dameng"} {
		t.Run(driver, func(t *testing.T) {
			r, cfg := crossSchemaRewriter(t)
			e := dialect.Engine{Driver: driver, Schema: "sales", AllowedSchemas: []string{"sales", "crm"}, Tables: map[string]string{"sales.orders": "sales.orders", "crm.customers": "crm.customers"}}
			if driver == "oracle" || driver == "dameng" {
				e.Schema = "SALES"
				e.AllowedSchemas = []string{"SALES", "CRM"}
				e.Tables = map[string]string{"sales.orders": "SALES.ORDERS", "crm.customers": "CRM.CUSTOMERS"}
			}
			for _, q := range queries {
				rewritten, err := r.Rewrite(q)
				if err != nil {
					t.Fatalf("%s: %v", q, err)
				}
				out, args, err := e.Compile(rewritten.RewrittenSQL, sql.Named("userId", 1))
				if err != nil {
					t.Fatalf("%s: %v", rewritten.RewrittenSQL, err)
				}
				out = strings.ReplaceAll(out, ":1", "?")
				var id int
				var name string
				if err := db.QueryRow(out, args...).Scan(&id, &name); err != nil {
					t.Fatalf("%s: %v", out, err)
				}
				if id != 1 || name != "Alice" {
					t.Fatalf("row policy or join failed: %d %s", id, name)
				}
			}
			for _, q := range []string{
				`SELECT o.secret FROM sales.orders o`,
				`SELECT o.order_id FROM sales.orders o JOIN crm.customers c ON c.secret=o.customer_id`,
				`SELECT o.order_id FROM sales.orders o WHERE EXISTS(SELECT 1 FROM crm.customers c WHERE c.secret=o.customer_id)`,
				`WITH x AS(SELECT secret FROM crm.customers) SELECT * FROM x`,
				`SELECT o.order_id FROM sales.orders o ORDER BY o.secret`,
				`SELECT * FROM outside.orders`,
				`WITH x AS (SELECT * FROM outside.orders) SELECT * FROM x`,
				`SELECT * FROM (SELECT * FROM outside.orders) x`,
				`SELECT * FROM sales.orders WHERE EXISTS(SELECT 1 FROM outside.orders)`,
				`SELECT order_id FROM sales.orders UNION SELECT order_id FROM outside.orders`,
				`SELECT * FROM (WITH orders AS(SELECT 1 AS order_id) SELECT * FROM orders) x JOIN outside.orders o ON 1=1`,
			} {
				if _, err := r.Rewrite(q); err == nil {
					t.Fatalf("unauthorized query accepted: %s", q)
				}
			}
			no := false
			cfg.Tables[1].Enabled = &no
			for _, q := range queries {
				if _, err := r.Rewrite(q); err == nil {
					t.Fatalf("disabled table accepted: %s", q)
				}
			}
			for _, q := range []string{`SELECT * FROM outside.orders`, `WITH x AS(SELECT * FROM outside.orders) SELECT * FROM x`, `SELECT * FROM (WITH orders AS(SELECT 1) SELECT * FROM orders) x JOIN orders ON 1=1`} {
				e.Schema = "outside"
				if _, _, err := e.Compile(q); err == nil {
					t.Fatalf("raw range or nested CTE scope bypass: %s", q)
				}
			}
		})
	}
}
func TestNoDefaultSchemaRequiresQualifiedTable(t *testing.T) {
	r, _ := crossSchemaRewriter(t)
	r.DefaultSchema = ""
	if _, err := r.Rewrite(`SELECT * FROM orders`); err == nil {
		t.Fatal("unqualified query without default accepted")
	}
	if _, err := r.Rewrite(`SELECT * FROM sales.orders`); err != nil {
		t.Fatal(err)
	}
}
