package examples_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"datascope-server/internal/catalog"
	"datascope-server/internal/datasource"
	"datascope-server/internal/management"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
)

// SQLite stand-ins exercise the shipped scripts' transaction and event semantics;
// the separate local demonstration validates the actual vendor connections.
func TestImportDemoScriptsRollbackUpdatesAndReplay(t *testing.T) {
	for _, tc := range []struct {
		scenario        string
		tables, sources []string
	}{
		{"regional-sales", []string{"imported_erp_customers", "imported_warehouse_inventory", "imported_online_orders"}, []string{"erp_customer_events", "warehouse_events", "online_order_events"}},
		{"academic", []string{"imported_student_registry", "imported_attendance_summary", "imported_course_signups"}, []string{"student_registry_events", "attendance_summary_events", "course_signup_events"}},
		{"channel-operations", []string{"imported_partner_accounts", "imported_content_metrics", "imported_lead_conversions"}, []string{"partner_account_events", "content_metric_events", "lead_conversion_events"}},
		{"securities", []string{"imported_customer_profiles", "imported_market_quotes", "imported_account_applications"}, []string{"customer_profile_events", "market_quote_events", "account_application_events"}},
	} {
		t.Run(tc.scenario, func(t *testing.T) { checkImportDemo(t, tc.scenario, tc.tables, tc.sources) })
	}
}
func checkImportDemo(t *testing.T, scenario string, tables, sources []string) {
	ctx := context.Background()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.sqlite3")
	db, err := sql.Open("sqlite", target)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("python3", "../../examples/data-imports/generate.py", "--scenario", scenario, "--directory", filepath.Join(dir, "fixture"), "--target", target, "--database-id", "imports").CombinedOutput()
	if err != nil {
		t.Fatalf("prepare: %v %s", err, out)
	}
	var manifest struct {
		Tasks []datasource.Task `json:"tasks"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "fixture/sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	m, err := management.Open(filepath.Join(dir, "management.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.AllowedRoots = []string{dir}
	if err = m.Bootstrap(ctx, "admin", "import-test-password"); err != nil {
		t.Fatal(err)
	}
	admin, err := m.ByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	c := &catalog.Catalog{Management: m, Timeout: 10 * time.Second, AsOf: store.DefaultAsOf}
	cfg := &rewrite.Config{Version: 1, Schemes: []rewrite.SchemeInfo{{Scheme: "NONE", Name: "test"}}}
	for _, table := range tables {
		cfg.Tables = append(cfg.Tables, &rewrite.Policy{Table: table, Scheme: "NONE", Passthrough: true})
	}
	if err = c.Add(ctx, catalog.Database{ID: "imports", Name: "Imports", Driver: "sqlite", Path: target, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	manager := datasource.New(m, c)
	defer manager.Close()
	for i, task := range manifest.Tasks {
		t.Run(task.ID, func(t *testing.T) {
			path := filepath.Join(dir, fmt.Sprintf("source%d.sqlite3", i))
			source, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.Exec("CREATE TABLE " + sources[i] + "(event_id INTEGER PRIMARY KEY, record_id INTEGER, display_name TEXT, metric_value INTEGER, is_deleted INTEGER, occurred_at TEXT)")
			if err != nil {
				t.Fatal(err)
			}
			for seq := 1; seq <= 201; seq++ {
				deleted := 0
				if seq == 201 {
					deleted = 1
				}
				_, err = source.Exec("INSERT INTO "+sources[i]+" VALUES(?,?,?,?,?,?)", seq, (seq-1)%100+1, " fictional ", seq, deleted, "2026-01-01T00:00:00Z")
				if err != nil {
					t.Fatal(err)
				}
			}
			source.Close()
			if _, err = manager.SaveConnection(ctx, "imports", datasource.Connection{ID: task.Sources[0], Name: "fixture", Driver: "sqlite", Path: path}, ""); err != nil {
				t.Fatal(err)
			}
			task.Enabled = false
			task.Schedule = datasource.Schedule{Mode: "manual"}
			task.Parameters = map[string]any{"batchSize": 500}
			if _, err = manager.SaveTask(ctx, "imports", task, admin.ID); err != nil {
				t.Fatal(err)
			}
			run := func(trial, fail bool) *datasource.Run {
				t.Helper()
				r, err := manager.Start(ctx, "imports", task.ID, admin.ID, trial, false, map[string]any{"batchSize": 500, "simulateFailure": fail})
				if err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(15 * time.Second)
				for time.Now().Before(deadline) {
					r, err = manager.GetRun(ctx, "imports", r.ID)
					if err != nil {
						t.Fatal(err)
					}
					if r.Status != "running" {
						return r
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("timeout")
				return nil
			}
			count := func() int {
				t.Helper()
				var n int
				if err := db.QueryRow("SELECT COUNT(*) FROM " + tables[i]).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			if r := run(true, false); r.Status != "rolled_back" || count() != 0 {
				t.Fatalf("trial: %+v", r)
			}
			if r := run(false, true); r.Status != "failed" || count() != 0 {
				t.Fatalf("failure rollback: %+v", r)
			}
			if r := run(false, false); r.Status != "succeeded" || count() != 100 {
				t.Fatalf("commit: %+v", r)
			}
			var seq, deleted, metric int
			var label string
			if err := db.QueryRow("SELECT source_seq,is_deleted,metric_value,name FROM "+tables[i]+" WHERE id=1").Scan(&seq, &deleted, &metric, &label); err != nil {
				t.Fatal(err)
			}
			if seq != 201 || deleted != 1 || metric != 201 || label != "fictional" {
				t.Fatalf("update/tombstone: %d %d %d %q", seq, deleted, metric, label)
			}
			if r := run(false, false); r.Status != "succeeded" || r.Written != 0 || r.Checkpoint["lastEventId"] != float64(201) {
				t.Fatalf("checkpoint/replay: %+v", r)
			}
		})
	}
}
