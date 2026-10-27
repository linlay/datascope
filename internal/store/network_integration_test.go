package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"datascope-server/internal/catalog"
	"datascope-server/internal/store"
)

// Opt-in, non-destructive vendor tests. See docs/databases.md for fixture DDL
// and configuration. This test never creates objects using a business account.
func TestNetworkDatabaseIntegration(t *testing.T) {
	for _, driver := range []string{"mysql", "postgresql", "oracle", "dameng"} {
		t.Run(driver, func(t *testing.T) {
			raw := os.Getenv("DATASCOPE_INTEGRATION_" + strings.ToUpper(driver))
			if raw == "" {
				t.Skip("未配置真实数据库连接")
			}
			var connection store.Connection
			if err := json.Unmarshal([]byte(raw), &connection); err != nil {
				t.Fatal("invalid integration connection JSON")
			}
			ctx := context.Background()
			c := &catalog.Catalog{Path: filepath.Join(t.TempDir(), "catalog"), Timeout: 5 * time.Second, AsOf: store.DefaultAsOf}
			if err := c.Add(ctx, catalog.Database{ID: "integration", Name: "Integration", Driver: driver, Connection: &connection}); err != nil {
				t.Fatal(err)
			}
			d, err := c.Get("integration")
			if err != nil {
				t.Fatal(err)
			}
			svc, err := c.Open(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Query(ctx, "SELECT * FROM datascope_records", 1, 10); err == nil {
				t.Fatal("new source was not deny-by-default")
			}
			schema, err := svc.Store.Describe(ctx, "datascope_records")
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range d.Config.Tables {
				if p.Table == "datascope_records" {
					yes := true
					p.Enabled = &yes
					p.Scheme = "CUSTOM"
					p.Passthrough = false
					p.SQL = "SELECT * FROM main.datascope_records WHERE owner_id = ${userId}"
					p.DenyUnless = "EXISTS (SELECT 1 FROM main.datascope_records WHERE owner_id = ${userId})"
					p.Columns = []string{}
					for _, col := range schema.Columns {
						if strings.EqualFold(col.Name, "id") || strings.EqualFold(col.Name, "owner_id") {
							p.Columns = append(p.Columns, col.Name)
						}
					}
				}
			}
			svc.Store.Close()
			if err := c.Save(ctx, *d); err != nil {
				t.Fatal(err)
			}
			d, err = c.Get("integration")
			if err != nil {
				t.Fatal(err)
			}
			svc, err = c.Open(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			defer svc.Store.Close()
			for _, query := range []string{`SELECT * FROM datascope_records ORDER BY id`, `WITH x AS (SELECT * FROM datascope_records) SELECT * FROM x ORDER BY id`, `SELECT a.id,a.owner_id FROM datascope_records a JOIN datascope_records b ON a.id=b.id ORDER BY a.id`} {
				out, err := svc.Query(ctx, query, 1, 10)
				if err != nil {
					t.Fatal(err)
				}
				if out.RowCount != 2 || len(out.Columns) != 2 {
					t.Fatalf("row/column scope mismatch: %+v", out.QueryResult)
				}
				for _, row := range out.Rows {
					for key, value := range row {
						if strings.EqualFold(key, "owner_id") && fmt.Sprint(value) != "1" {
							t.Fatal("row policy escaped")
						}
						if strings.EqualFold(key, "secret") {
							t.Fatal("column policy escaped")
						}
					}
				}
			}
			if _, err := svc.Query(ctx, "SELECT * FROM datascope_records", 3, 10); err == nil {
				t.Fatal("deny_unless guard failed")
			}
			for _, query := range []string{`DELETE FROM datascope_records`, `WITH x AS (DELETE FROM datascope_records RETURNING *) SELECT * FROM x`, `SELECT * FROM datascope_records; DELETE FROM datascope_records`, `SELECT custom_fn(id) FROM datascope_records`} {
				if _, err := svc.Store.RawQuery(ctx, query, 10); err == nil {
					t.Fatalf("unsafe raw query accepted: %s", query)
				}
			}
			page, err := svc.Store.Browse(ctx, store.BrowseOptions{Table: "datascope_records", Limit: 1, Offset: 1})
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 3 || len(page.Rows) != 1 {
				t.Fatalf("pagination: %+v", page)
			}
		})
	}
}
