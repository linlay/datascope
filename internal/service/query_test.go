package service

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
	"datascope-server/internal/testutil"
	"testing"
	"time"
)

func newService(t *testing.T) *QueryService {
	t.Helper()
	st, e := store.Open(testutil.Demo(t), 0)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	cfg, e := rewrite.LoadConfig(filepath.Join("../../config/rewrite.yml"))
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(context.Background(), st, cfg, store.DefaultAsOf)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestEveryEmployeeMatchesIndependentOracle(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	for _, date := range []string{"2026-09-30", "2026-10-01", "2026-10-21", "2026-11-01"} {
		s.AsOf, _ = time.Parse("2006-01-02", date)
		for uid := int64(1); uid <= 179; uid++ {
			want, e := s.Store.VisibleCustomerIDs(ctx, uid, s.AsOf)
			if e != nil {
				t.Fatal(e)
			}
			if len(want) > 1000 {
				want = want[:1000]
			}
			for _, q := range []string{"SELECT customer_no FROM customers ORDER BY customer_no", "SELECT DISTINCT customer_no FROM trades ORDER BY customer_no", "WITH c AS (SELECT customer_no FROM customers) SELECT DISTINCT c.customer_no FROM c JOIN fund_accounts f ON f.customer_no=c.customer_no ORDER BY c.customer_no"} {
				got, e := s.Query(ctx, q, uid, 1000)
				if e != nil {
					t.Fatalf("employee %d: %v", uid, e)
				}
				ids := []string{}
				for _, r := range got.Rows {
					ids = append(ids, r["customer_no"].(string))
				}
				if !slices.Equal(ids, want) {
					t.Fatalf("date %s employee %d %s: got %v want %v", date, uid, q, ids, want)
				}
			}
		}
	}
}
func TestSQLSafetyAndGuards(t *testing.T) {
	s := newService(t)
	for _, q := range []string{"SELECT * FROM employee_scopes", "SELECT * FROM customers; SELECT 1", "DELETE FROM customers", "PRAGMA table_info(customers)", "SELECT * FROM main.customers", "SELECT * FROM pragma_table_info('employees')", "WITH customers AS (SELECT 1 AS id) SELECT * FROM customers", "SELECT * FROM sqlite_schema", "SELECT * FROM customers WHERE id=:userId", "SELECT 1 WHERE 1 IN customers"} {
		if _, e := s.Query(context.Background(), q, 1, 0); e == nil {
			t.Errorf("accepted %s", q)
		}
	}
	r, e := s.Query(context.Background(), "SELECT * FROM employee_scopes", 175, 0)
	if e != nil || r.RowCount == 0 {
		t.Fatalf("admin: %v", e)
	}
	r, e = s.Query(context.Background(), "SELECT COUNT(*) AS n FROM customers", 175, 0)
	if e != nil || r.Rows[0]["n"] != int64(1500) {
		t.Fatalf("admin count %v %v", r, e)
	}
}
func TestEmployeeVisibilityAndBinds(t *testing.T) {
	s := newService(t)
	for uid, n := range map[int64]int64{1: 1, 121: 4, 151: 13, 161: 34, 162: 17, 175: 100, 176: 0, 9999: 0} {
		r, e := s.Query(context.Background(), "SELECT COUNT(*) AS n FROM employees", uid, 0)
		if e != nil || r.Rows[0]["n"] != n {
			t.Errorf("employee %d: %v %v want %d", uid, r, e, n)
		}
	}
	r, e := s.Rewrite(context.Background(), "SELECT customer_no FROM customers", 1)
	if e != nil || r.Binds["userId"] != int64(1) || r.Binds["asOf"] == nil {
		t.Fatalf("binds %v %v", r, e)
	}
}

func TestCrossDepartmentServiceUsesAccountOpeningDepartment(t *testing.T) {
	s := newService(t)
	for uid, want := range map[int64]int64{13: 1, 121: 1, 124: 0} {
		r, e := s.Query(context.Background(), "SELECT COUNT(*) AS n FROM fund_accounts WHERE fund_account_no='880000000001'", uid, 0)
		if e != nil || r.Rows[0]["n"] != want {
			t.Fatalf("cross-department employee %d got %v %v", uid, r, e)
		}
	}
}

func TestSharedCustomerDoesNotGrantOtherAccounts(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	// Customer 1 has accounts in departments 1, 4, 7 and 10.
	// The first account is managed across departments by employee 13.
	for _, uid := range []int64{13, 121, 124, 151} {
		customer, err := s.Query(ctx, "SELECT customer_no FROM customers WHERE customer_no='C00000001'", uid, 1000)
		if err != nil || customer.RowCount != 1 {
			t.Fatalf("employee %d customer visibility: %+v %v", uid, customer, err)
		}
		want, err := s.Store.Query(ctx, `SELECT f.fund_account_no FROM fund_accounts f JOIN departments d ON d.code=f.department_code WHERE f.customer_no='C00000001' AND (f.manager_id=`+fmt.Sprint(uid)+` OR EXISTS(SELECT 1 FROM employee_scopes x WHERE x.employee_id=`+fmt.Sprint(uid)+` AND (x.department_code=f.department_code OR x.branch_code=d.branch_code) AND x.effective_date<='2026-10-21' AND (x.expiry_date IS NULL OR x.expiry_date>'2026-10-21'))) ORDER BY f.fund_account_no`, 1000)
		if err != nil {
			t.Fatal(err)
		}
		expected := []string{}
		for _, row := range want.Rows {
			expected = append(expected, row["fund_account_no"].(string))
		}
		if len(expected) != 1 {
			t.Fatalf("employee %d expected one sample account, got %v", uid, expected)
		}
		for _, table := range []string{"fund_accounts", "orders", "trades", "cash_flows", "positions", "daily_balances"} {
			for _, query := range []string{
				"SELECT DISTINCT fund_account_no FROM " + table + " WHERE customer_no='C00000001' ORDER BY fund_account_no",
				"WITH c AS (SELECT customer_no FROM customers WHERE customer_no='C00000001') SELECT DISTINCT t.fund_account_no FROM " + table + " t JOIN c ON c.customer_no=t.customer_no ORDER BY t.fund_account_no",
			} {
				got, err := s.Query(ctx, query, uid, 1000)
				if err != nil {
					t.Fatal(err)
				}
				actual := []string{}
				for _, row := range got.Rows {
					actual = append(actual, row["fund_account_no"].(string))
				}
				if !slices.Equal(actual, expected) {
					t.Fatalf("employee %d %s: got %v want %v", uid, table, actual, expected)
				}
			}
		}
	}
}

func TestShareholderNumbersFollowFundAccountVisibility(t *testing.T) {
	s := newService(t)
	for uid, accountNo := range map[int64]string{13: "880000000001", 121: "880000000001", 124: "880000000002", 151: "880000000001"} {
		for _, q := range []string{
			"SELECT a.fund_account_no,a.exchange FROM stock_accounts a JOIN fund_accounts f ON f.fund_account_no=a.fund_account_no WHERE f.customer_no='C00000001' ORDER BY a.exchange",
			"WITH c AS (SELECT customer_no FROM customers WHERE customer_no='C00000001') SELECT a.fund_account_no,a.exchange FROM stock_accounts a WHERE a.fund_account_no IN (SELECT f.fund_account_no FROM fund_accounts f JOIN c ON c.customer_no=f.customer_no) ORDER BY a.exchange",
		} {
			got, err := s.Query(context.Background(), q, uid, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if got.RowCount != 2 {
				t.Fatalf("employee %d: expected two shareholder numbers, got %+v", uid, got)
			}
			for i, row := range got.Rows {
				if row["fund_account_no"] != accountNo || row["exchange"] != []string{"SH", "SZ"}[i] {
					t.Fatalf("employee %d leaked shareholder number: %v", uid, row)
				}
			}
		}
	}
}
