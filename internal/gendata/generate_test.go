package gendata

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestDeterminismIntegrityAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.sqlite3"), filepath.Join(dir, "b.sqlite3")
	for _, p := range []string{a, b} {
		if e := Generate(Options{Path: p, Schema: "../../sql/schema.sqlite.sql", Customers: 25}); e != nil {
			t.Fatal(e)
		}
	}
	ra, _ := os.ReadFile(a)
	rb, _ := os.ReadFile(b)
	if !bytes.Equal(ra, rb) {
		t.Fatal("seeded databases differ byte for byte")
	}
	if e := Generate(Options{Path: a, Schema: "../../sql/schema.sqlite.sql"}); e == nil {
		t.Fatal("existing database overwritten")
	}
	db, e := sql.Open("sqlite", a)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var integrity string
	if e = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); e != nil || integrity != "ok" {
		t.Fatalf("integrity %s %v", integrity, e)
	}
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE orders SET filled_quantity=1 WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	if e = validate(tx); e == nil {
		t.Fatal("corrupt trade totals passed validation")
	}
}
func TestFailedGenerationCleansPartialOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	if e := Generate(Options{Path: path, Schema: "missing.sql"}); e == nil {
		t.Fatal("missing schema accepted")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("partial DB remains")
	}
}

func TestBusinessKeysAndAccountDistribution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.sqlite3")
	if err := Generate(Options{Path: path, Schema: "../../sql/schema.sqlite.sql", Customers: 141}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for table, key := range map[string]string{"branches": "code", "departments": "code", "employees": "employee_id", "customers": "customer_no", "fund_accounts": "fund_account_no", "securities": "code"} {
		var actual string
		if err := db.QueryRow(`SELECT name FROM pragma_table_info(?) WHERE pk=1`, table).Scan(&actual); err != nil || actual != key {
			t.Fatalf("%s primary key = %q, %v", table, actual, err)
		}
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name='id'`, table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s retained surrogate id: %d, %v", table, n, err)
		}
	}
	checks := map[string]string{
		"customer ownership removed":  `SELECT count(*) FROM pragma_table_info('customers') WHERE name IN ('department_code','branch_code','manager_id')`,
		"split ownership":             `SELECT NOT(count(*)>0) FROM (SELECT customer_no FROM fund_accounts GROUP BY customer_no HAVING count(DISTINCT department_code)>1 AND count(DISTINCT manager_id)>1)`,
		"stock account ownership":     `SELECT count(*) FROM fund_accounts f WHERE (SELECT count(*) FROM stock_accounts s WHERE s.fund_account_no=f.fund_account_no)!=2`,
		"stock customer link removed": `SELECT count(*) FROM pragma_table_info('stock_accounts') WHERE name='customer_no'`,
		"employee count":              `SELECT count(*)!=100 FROM employees`,
		"individual distribution":     `SELECT NOT(count(*)>0 AND sum(n=1)>sum(n>1) AND sum(n BETWEEN 2 AND 3)>0) FROM (SELECT c.customer_no,count(*) n FROM customers c JOIN fund_accounts f USING(customer_no) WHERE c.customer_type='INDIVIDUAL' GROUP BY c.customer_no)`,
		"corporate distribution":      `SELECT NOT(min(n)>=4 AND max(n)=10) FROM (SELECT c.customer_no,count(*) n FROM customers c JOIN fund_accounts f USING(customer_no) WHERE c.customer_type='CORPORATE' GROUP BY c.customer_no)`,
		"account activity":            `SELECT count(*) FROM fund_accounts f WHERE NOT EXISTS(SELECT 1 FROM orders o WHERE o.fund_account_no=f.fund_account_no) OR NOT EXISTS(SELECT 1 FROM daily_balances b WHERE b.fund_account_no=f.fund_account_no)`,
		"industry names":              `SELECT 7-count(DISTINCT substr(name,3)) FROM securities WHERE substr(name,3) IN ('科技','实业','发展','银行','证券','期货','保险')`,
	}
	for name, q := range checks {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: violations=%d, %v", name, n, err)
		}
	}
	// A valid account owned by another customer must still fail ledger validation.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE cash_flows SET customer_no='C00000002' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := validate(tx); err == nil {
		t.Fatal("cross-customer cash flow passed validation")
	}
}

func TestOrderCannotUseAnotherFundAccountsShareholderNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.sqlite3")
	if err := Generate(Options{Path: path, Schema: "../../sql/schema.sqlite.sql", Customers: 1}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Reassign to a valid shareholder number of the same exchange and customer,
	// but a different fund account: validation must reject the mismatch.
	if _, err := tx.Exec(`UPDATE orders SET stock_account_id=(SELECT a.id FROM stock_accounts a JOIN stock_accounts original ON original.id=orders.stock_account_id JOIN fund_accounts f ON f.fund_account_no=a.fund_account_no WHERE a.exchange=original.exchange AND a.fund_account_no!=orders.fund_account_no AND f.customer_no=orders.customer_no LIMIT 1) WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := validate(tx); err == nil {
		t.Fatal("order with another fund account's shareholder number passed validation")
	}
}
