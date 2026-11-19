package examples_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/configbundle"
	"datascope-server/internal/datasource"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
)

func TestSupplyChainBusinessImports(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	target := filepath.Join(dir, "regional.sqlite3")
	python := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("python3", args...).CombinedOutput(); err != nil {
			t.Fatalf("python: %v %s", err, out)
		}
	}
	python("../../examples/regional-sales/generate.py", "--db", target, "--orders", "240")
	fixture := filepath.Join(dir, "sources")
	args := []string{"../../examples/sales-transactions/generate.py", "--target", target, "--directory", fixture, "--database-id", "regional"}
	python(args...)
	if out, err := exec.Command("python3", args...).CombinedOutput(); err == nil {
		t.Fatalf("existing sources overwritten: %s", out)
	}
	var manifest struct {
		Connections []datasource.Connection `json:"connections"`
		Tasks       []datasource.Task       `json:"tasks"`
	}
	raw, err := os.ReadFile(filepath.Join(fixture, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	m, err := management.Open(filepath.Join(dir, "management.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.AllowedRoots = []string{dir}
	if err = m.Bootstrap(ctx, "admin", "supply-chain-test-password"); err != nil {
		t.Fatal(err)
	}
	admin, err := m.ByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", target)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	count := func(query string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: got %d, want %d, err %v", query, got, want, err)
		}
	}
	count("SELECT COUNT(*) FROM sales_orders", 240) // Preparation never writes target.
	c := &catalog.Catalog{Management: m, Timeout: 10 * time.Second, AsOf: store.DefaultAsOf}
	cfg := &rewrite.Config{Version: 1, Schemes: []rewrite.SchemeInfo{{Scheme: "NONE", Name: "test"}}}
	tables, err := db.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		t.Fatal(err)
	}
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			t.Fatal(err)
		}
		cfg.Tables = append(cfg.Tables, &rewrite.Policy{Table: name, Scheme: "NONE", Passthrough: true})
	}
	if err := tables.Err(); err != nil {
		t.Fatal(err)
	}
	tables.Close()
	if err = c.Add(ctx, catalog.Database{ID: "regional", Name: "Regional", Driver: "sqlite", Path: target, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	// Validate and install both real declarative packages, including SQL examples.
	for i, folder := range []string{"sales-transactions", "warehouse-fulfillment"} {
		b, err := configbundle.Load("../../examples/" + folder + "/bundle.yml")
		if err != nil {
			t.Fatal(err)
		}
		b.Database.Path = manifest.Connections[i].Path
		app := &application.Service{Management: m, Catalog: c}
		if _, err := configbundle.Import(guard.WithUser(ctx, admin), app, b, "supply-chain-test-password", false); err != nil {
			t.Fatalf("bundle %s: %v", folder, err)
		}
	}
	manager := datasource.New(m, c)
	defer manager.Close()
	for _, connection := range manifest.Connections {
		if _, err = manager.SaveConnection(ctx, "regional", connection, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range manifest.Tasks {
		if task.Enabled {
			t.Fatal("demo must not enable scheduling automatically")
		}
		if _, err = manager.SaveTask(ctx, "regional", task, admin.ID); err != nil {
			t.Fatal(err)
		}
	}
	run := func(kind, status string, trial, fail bool, batch int) *datasource.Run {
		t.Helper()
		r, err := manager.Start(ctx, "regional", "supply-chain-"+kind, admin.ID, trial, false, map[string]any{"batchSize": batch, "simulateFailure": fail})
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			r, err = manager.GetRun(ctx, "regional", r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "running" {
				if r.Status != status {
					t.Fatalf("%s: expected %s, got %+v", kind, status, r)
				}
				return r
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("import timeout")
		return nil
	}
	run("fulfillment", "failed", false, false, 20) // Dependencies may arrive first.
	run("orders", "rolled_back", true, false, 20)
	run("orders", "failed", false, true, 20)
	count("SELECT COUNT(*) FROM sales_orders WHERE id>8100000", 0)
	for i := 0; i < 4; i++ {
		run("orders", "succeeded", false, false, 5)
	}
	count("SELECT COUNT(*) FROM sales_orders WHERE id>8100000", 20)
	count("SELECT COUNT(*) FROM sales_order_items WHERE id>8100000", 40)
	run("settlements", "failed", false, false, 20) // Refund depends on received return.
	count("SELECT COUNT(*) FROM payments WHERE id>8100000", 0)
	run("fulfillment", "succeeded", false, false, 20)
	run("settlements", "succeeded", false, false, 20)
	run("inventory", "succeeded", false, false, 20)
	count("SELECT COUNT(*) FROM shipments WHERE id>8100000", 15)
	count("SELECT COUNT(*) FROM refunds WHERE id>8100000", 1)
	count("SELECT COUNT(*) FROM inventory_balances WHERE id>8100000", 12)
	// Fresh checkpoint replay after an uncertain commit must not duplicate or rewrite rows.
	for _, task := range manifest.Tasks {
		task.ID += "-replay"
		if _, err = manager.SaveTask(ctx, "regional", task, admin.ID); err != nil {
			t.Fatal(err)
		}
		if r := run(task.ID[len("supply-chain-"):], "succeeded", false, false, 20); r.Written != 0 {
			t.Fatalf("replay wrote %d rows", r.Written)
		}
	}
	python("../../examples/sales-transactions/generate.py", "--directory", fixture, "--advance")
	if out, err := exec.Command("python3", "../../examples/sales-transactions/generate.py", "--directory", fixture, "--advance").CombinedOutput(); err == nil {
		t.Fatalf("advanced twice: %s", out)
	}
	run("orders", "failed", false, true, 20)
	count("SELECT COUNT(*) FROM sales_orders WHERE id=8100001 AND status='partial_shipped'", 1)
	for _, kind := range []string{"orders", "fulfillment", "settlements", "inventory"} {
		run(kind, "succeeded", false, false, 20)
		if r := run(kind, "succeeded", false, false, 20); r.Written != 0 {
			t.Fatalf("empty run wrote %d rows", r.Written)
		}
	}
	count("SELECT COUNT(*) FROM sales_orders WHERE id=8100019 AND status='cancelled'", 1)
	count("SELECT COUNT(*) FROM shipments WHERE order_id=8100001", 2)
	count("SELECT SUM(si.quantity) FROM shipment_items si JOIN shipments s ON s.id=si.shipment_id WHERE s.order_id=8100001", 4)
	count("SELECT COUNT(*) FROM sales_orders o WHERE o.id=8100001 AND o.total_cents=(SELECT SUM(a.amount_cents) FROM payment_allocations a WHERE a.order_id=o.id)", 1)
	count("SELECT on_hand FROM inventory_balances WHERE id=8100001", 39)
	count("SELECT COUNT(*) FROM sales_orders WHERE id<=8100000", 240)
	violations, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer violations.Close()
	if violations.Next() {
		t.Fatal("foreign key violation after imports")
	}
}
