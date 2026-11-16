package regionalsales

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"datascope-server/internal/service"
	"datascope-server/internal/store"
)

func fixture(t *testing.T) (string, *service.QueryService) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sales.sqlite3")
	out, err := exec.Command("python3", "../../../examples/regional-sales/generate.py", "--db", path, "--orders", "1200").CombinedOutput()
	if err != nil {
		t.Fatalf("generate: %s %v", out, err)
	}
	st, err := store.Open(path, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg, err := Policies(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(context.Background(), st, cfg, store.DefaultAsOf)
	if err != nil {
		t.Fatal(err)
	}
	return path, svc
}

func TestComplexSQLAndDataIntegrity(t *testing.T) {
	_, svc := fixture(t)
	ctx := context.Background()
	// Verify the same queries through the production service, for all six accounts.
	for _, ex := range Examples() {
		raw, err := svc.Store.RawQuery(ctx, ex.SQL, 5)
		if err != nil || raw.RowCount == 0 {
			t.Fatalf("raw example %s: %+v %v", ex.Label, raw, err)
		}
		for _, id := range []int64{1, 2, 3, 9, 107078, 900330} {
			got, err := svc.Query(ctx, ex.SQL, id, 5)
			if err != nil {
				t.Fatalf("%s user=%d: %v", ex.Label, id, err)
			}
			if id == 1 && !reflect.DeepEqual(raw.Rows, got.Rows) {
				t.Fatalf("HQ aggregate differs from direct SQL for %s: raw=%+v rewritten=%+v", ex.Label, raw.Rows, got.Rows)
			}
		}
	}
	invariants := []string{
		"SELECT COUNT(*) AS n FROM sales_orders o WHERE total_cents<>(SELECT SUM(net_cents) FROM sales_order_items i WHERE i.order_id=o.id)",
		"SELECT COUNT(*) AS n FROM sales_orders o WHERE (SELECT SUM(amount_cents) FROM payment_allocations a WHERE a.order_id=o.id)>total_cents",
		"SELECT COUNT(*) AS n FROM payments p WHERE amount_cents<>(SELECT SUM(amount_cents) FROM payment_allocations a WHERE a.payment_id=p.id)",
		"SELECT COUNT(*) AS n FROM sales_order_items i WHERE (SELECT SUM(quantity) FROM shipment_items s WHERE s.order_item_id=i.id)>quantity",
		"SELECT COUNT(*) AS n FROM sales_return_items i WHERE quantity>(SELECT SUM(quantity) FROM shipment_items s WHERE s.order_item_id=i.order_item_id)",
		"SELECT COUNT(*) AS n FROM refunds f JOIN sales_returns r ON r.id=f.return_id JOIN sales_orders o ON o.id=r.order_id JOIN payments p ON p.id=f.payment_id WHERE r.status<>'received' OR p.customer_id<>o.customer_id OR f.refund_date<r.return_date",
		"SELECT COUNT(*) AS n FROM sales_return_items ri JOIN sales_returns r ON r.id=ri.return_id JOIN sales_order_items i ON i.id=ri.order_item_id WHERE r.order_id<>i.order_id OR ri.amount_cents>i.net_cents",
		"SELECT COUNT(*) AS n FROM payments p WHERE (SELECT SUM(amount_cents) FROM refunds f WHERE f.payment_id=p.id)>p.amount_cents",
		"SELECT COUNT(*) AS n FROM sales_returns r WHERE (SELECT SUM(amount_cents) FROM refunds f WHERE f.return_id=r.id)>(SELECT SUM(amount_cents) FROM sales_return_items i WHERE i.return_id=r.id)",
		"SELECT COUNT(*) AS n FROM shipment_items i JOIN shipments s ON s.id=i.shipment_id JOIN sales_order_items oi ON oi.id=i.order_item_id WHERE s.order_id<>oi.order_id",
	}
	for _, q := range invariants {
		r, err := svc.Store.RawQuery(ctx, q, 1)
		if err != nil || r.Rows[0]["n"] != int64(0) {
			t.Fatalf("invariant %s: %+v %v", q, r, err)
		}
	}
}

func TestPermissionsCannotBypassViaChildrenCTEOrUnion(t *testing.T) {
	_, svc := fixture(t)
	ctx := context.Background()
	// Independent expected counts: fixture IDs map to fixed regions/offices/owners.
	for _, tc := range []struct {
		id     int64
		filter string
	}{{1, "1=1"}, {2, "o.region_id=1"}, {3, "o.region_id=2"}, {9, "o.office_id=1"}, {107078, "o.owner_employee_id=107078"}, {900330, "o.owner_employee_id=900330"}} {
		for table, join := range map[string]string{
			"sales_orders":        "sales_orders o",
			"sales_order_items":   "sales_order_items x JOIN sales_orders o ON o.id=x.order_id",
			"shipments":           "shipments x JOIN sales_orders o ON o.id=x.order_id",
			"shipment_items":      "shipment_items x JOIN shipments s ON s.id=x.shipment_id JOIN sales_orders o ON o.id=s.order_id",
			"sales_returns":       "sales_returns x JOIN sales_orders o ON o.id=x.order_id",
			"sales_return_items":  "sales_return_items x JOIN sales_returns r ON r.id=x.return_id JOIN sales_orders o ON o.id=r.order_id",
			"payment_allocations": "payment_allocations x JOIN sales_orders o ON o.id=x.order_id",
			"refunds":             "refunds x JOIN sales_returns r ON r.id=x.return_id JOIN sales_orders o ON o.id=r.order_id",
			"customers":           "customers o", "customer_contacts": "customer_contacts x JOIN customers o ON o.id=x.customer_id",
			"payments": "payments o", "sales_targets": "sales_targets o",
		} {
			expected, err := svc.Store.RawQuery(ctx, "SELECT COUNT(*) AS n FROM "+join+" WHERE "+tc.filter, 1)
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.Query(ctx, "SELECT COUNT(*) AS n FROM "+table, tc.id, 1)
			if err != nil || got.Rows[0]["n"] != expected.Rows[0]["n"] {
				t.Fatalf("%s id=%d expected=%+v got=%+v err=%v", table, tc.id, expected, got, err)
			}
		}
	}
	for _, q := range []string{
		"SELECT id FROM sales_orders WHERE region_id<>1",
		"WITH leaked AS (SELECT * FROM sales_orders) SELECT id FROM leaked WHERE region_id<>1",
		"SELECT id FROM sales_orders WHERE region_id<>1 UNION ALL SELECT order_id FROM sales_order_items WHERE order_id IN (SELECT id FROM sales_orders WHERE region_id<>1)",
		"SELECT i.id FROM sales_order_items i WHERE NOT EXISTS (SELECT 1 FROM sales_orders o WHERE o.id=i.order_id)",
	} {
		r, err := svc.Query(ctx, q, 2, 100)
		if err != nil || r.RowCount != 0 {
			t.Fatalf("leak: %s %+v %v", q, r, err)
		}
	}
	for _, q := range []string{"SELECT phone FROM customer_contacts", "SELECT email FROM customer_contacts"} {
		if _, err := svc.Query(ctx, q, 1, 1); err == nil {
			t.Fatalf("sensitive column allowed: %s", q)
		}
	}
	r, err := svc.Query(ctx, "SELECT * FROM customer_contacts", 107078, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, col := range r.Columns {
		if col == "phone" || col == "email" {
			t.Fatal("SELECT * exposed private column")
		}
	}
	if _, err = svc.Query(ctx, "SELECT COUNT(*) FROM products", 99999, 1); err == nil {
		t.Fatal("unknown employee allowed")
	}
	if _, err = svc.RewriteWithParameters(ctx, "SELECT COUNT(*) FROM sales_orders", 107078, map[string]any{"userId": int64(1)}); err == nil {
		t.Fatal("forged user identity allowed")
	}
}

func TestInstallPreservesExistingConfigurationAndPasswords(t *testing.T) {
	path, _ := fixture(t)
	ctx := context.Background()
	dir := t.TempDir()
	a, err := audit.Open(filepath.Join(dir, "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m, err := management.Open(filepath.Join(dir, "management.db"), a)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.AllowedRoots = []string{filepath.Dir(path)}
	if err = m.Bootstrap(ctx, "admin", "regional-test-password"); err != nil {
		t.Fatal(err)
	}
	u, err := m.ByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ctx = guard.WithUser(ctx, u)
	c := &catalog.Catalog{Management: m, Audit: a, Timeout: 20 * time.Second, AsOf: store.DefaultAsOf}
	app := &application.Service{Management: m, Catalog: c}
	d, err := Install(ctx, app, path, "regional-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if !d.MCPEnabled || len(d.Config.Tables) != 27 || len(d.SQLExamples) != 10 {
		t.Fatalf("incomplete install %+v", d)
	}
	account, err := m.AuthenticateBusiness(ctx, d.ID, "107078", "regional-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(account.Permissions) != "[query knowledge.read]" {
		t.Fatal(account)
	}
	account.Active = false
	if _, err = m.SaveBusinessAccount(ctx, account, "changed-test-password", account.AuthVersion); err != nil {
		t.Fatal(err)
	}
	d.Name = "自定义名称"
	d.MCPEnabled = false
	if err = c.Save(ctx, *d); err != nil {
		t.Fatal(err)
	}
	d, err = c.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	revision := d.Revision
	again, err := Install(ctx, app, path, "another-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if again.Revision != revision || again.Name != "自定义名称" || again.MCPEnabled {
		t.Fatal("reinstall overwrote configuration")
	}
	saved, err := m.BusinessAccount(ctx, d.ID, "107078")
	if err != nil || saved.Active {
		t.Fatalf("reinstall reactivated account: %+v %v", saved, err)
	}
	if saved.AuthVersion != account.AuthVersion+1 {
		t.Fatal("reinstall changed password/version")
	}
}
