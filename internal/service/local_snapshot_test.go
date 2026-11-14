package service

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
)

// Opt-in comparison opens both local snapshots read-only, through the production rewriter.
func TestLocalCompactedSnapshotPermissions(t *testing.T) {
	before, after := os.Getenv("DATASCOPE_SECURITIES_BEFORE_DB"), os.Getenv("DATASCOPE_SECURITIES_AFTER_DB")
	if before == "" || after == "" {
		t.Skip("set DATASCOPE_SECURITIES_BEFORE_DB and DATASCOPE_SECURITIES_AFTER_DB")
	}
	ctx := context.Background()
	open := func(path string) *QueryService {
		st, err := store.Open(path, 20*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		cfg, err := rewrite.LoadConfig("../../examples/securities/rewrite.yml")
		if err != nil {
			t.Fatal(err)
		}
		svc, err := New(ctx, st, cfg, store.DefaultAsOf)
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	a, b := open(before), open(after)
	for _, date := range []string{"2026-09-30", "2026-10-01", "2026-10-21", "2026-11-01"} {
		a.AsOf, _ = time.Parse("2006-01-02", date)
		b.AsOf = a.AsOf
		for uid := int64(1); uid <= 179; uid++ {
			for _, q := range []string{
				"SELECT customer_no FROM customers ORDER BY customer_no",
				"SELECT fund_account_no FROM fund_accounts ORDER BY fund_account_no",
				"SELECT COUNT(*) AS n,COALESCE(SUM(market_value_cents),0) AS value FROM positions WHERE trade_date='2026-10-20'",
				"SELECT COUNT(*) AS n,COALESCE(SUM(closing_balance),0) AS value FROM daily_balances WHERE trade_date='2026-10-20'",
				"SELECT COUNT(*) AS n,COALESCE(SUM(filled_quantity),0) AS quantity FROM orders WHERE trade_date BETWEEN '2026-09-15' AND '2026-10-20'",
				"SELECT COUNT(*) AS n,COALESCE(SUM(amount_cents),0) AS value FROM trades WHERE trade_date BETWEEN '2026-09-15' AND '2026-10-20'",
				"SELECT COUNT(*) AS n,COALESCE(SUM(amount_cents),0) AS value FROM cash_flows WHERE trade_date BETWEEN '2026-09-15' AND '2026-10-20'",
			} {
				want, e := a.Query(ctx, q, uid, 3000)
				if e != nil {
					t.Fatal(e)
				}
				got, e := b.Query(ctx, q, uid, 3000)
				if e != nil {
					t.Fatal(e)
				}
				if !reflect.DeepEqual(got.Rows, want.Rows) {
					t.Fatalf("date=%s uid=%d query=%s: snapshot permissions/results changed", date, uid, q)
				}
			}
		}
	}
}
