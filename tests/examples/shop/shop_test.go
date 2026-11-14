package shop_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"datascope-server/internal/rewrite"
	"datascope-server/internal/service"
	"datascope-server/internal/store"
	"gopkg.in/yaml.v3"
)

func TestExpandedShopPermissions(t *testing.T) {
	path := os.Getenv("DATASCOPE_SHOP_TEST_DB")
	if path == "" {
		path = filepath.Join(t.TempDir(), "shop.sqlite3")
		if out, err := exec.Command("python3", "../../../examples/shop/generate.py", "--db", path).CombinedOutput(); err != nil {
			t.Fatalf("generate: %s %v", out, err)
		}
	}
	st, err := store.Open(path, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, err := rewrite.LoadConfig("../../../examples/shop/rewrite.yml")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(context.Background(), st, cfg, store.DefaultAsOf)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var examples struct {
		Queries []struct {
			Label string `yaml:"label"`
			SQL   string `yaml:"sql"`
		} `yaml:"sqlExamples"`
	}
	raw, err := os.ReadFile("../../../examples/shop/sql-examples.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(raw, &examples); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{1, 2, 3, 4, 9999} {
		for _, example := range examples.Queries {
			if _, err := svc.Query(ctx, example.SQL, uid, 100); err != nil {
				t.Fatalf("uid=%d example=%s: %v", uid, example.Label, err)
			}
		}
		for _, pair := range [][2]string{
			{"SELECT COUNT(*) AS n FROM customers", "SELECT COUNT(*) AS n FROM customers WHERE owner_employee_id=:arg_uid"},
			{"SELECT COUNT(*) AS n FROM orders", "SELECT COUNT(*) AS n FROM orders WHERE customer_id IN (SELECT id FROM customers WHERE owner_employee_id=:arg_uid)"},
			{"SELECT COALESCE(SUM(quantity*unit_price_cents),0) AS n FROM orders WHERE status IN ('paid','shipped')", "SELECT COALESCE(SUM(quantity*unit_price_cents),0) AS n FROM orders WHERE status IN ('paid','shipped') AND customer_id IN (SELECT id FROM customers WHERE owner_employee_id=:arg_uid)"},
			{"SELECT COUNT(*) AS n FROM customers WHERE owner_employee_id=2 OR 1=1", "SELECT COUNT(*) AS n FROM customers WHERE owner_employee_id=:arg_uid"},
		} {
			got, e := svc.Query(ctx, pair[0], uid, 1)
			if e != nil {
				t.Fatal(e)
			}
			want, e := st.RawQueryWithParameters(ctx, pair[1], 1, map[string]any{"arg_uid": float64(uid)})
			if e != nil {
				t.Fatal(e)
			}
			if got.Rows[0]["n"] != want.Rows[0]["n"] {
				t.Fatalf("uid=%d query=%s got=%v want=%v", uid, pair[0], got.Rows, want.Rows)
			}
		}
		if _, e := svc.Query(ctx, "SELECT cost_cents FROM products", uid, 1); e == nil {
			t.Fatal("cost field exposed")
		}
		if _, e := svc.Query(ctx, "SELECT p.cost_cents FROM products p JOIN orders o ON o.product_id=p.id", uid, 1); e == nil {
			t.Fatal("cost field exposed through JOIN")
		}
		got, e := svc.Query(ctx, "SELECT COUNT(*) AS n FROM products", uid, 1)
		if e != nil || got.Rows[0]["n"] != int64(100) {
			t.Fatalf("catalog: %v %v", got, e)
		}
	}
}
