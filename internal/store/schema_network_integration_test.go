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
	"datascope-server/internal/mcpserver"
	"datascope-server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Opt-in and read-only. An administrator provisions the isolated fixtures in
// fixtures/databases/schema_join first. Missing environments are SKIP, not PASS.
func TestSchemaJoinNetworkIntegration(t *testing.T) {
	for _, driver := range []string{"mysql", "oceanbase", "postgresql", "oracle", "dameng"} {
		t.Run(driver, func(t *testing.T) {
			raw := os.Getenv("DATASCOPE_SCHEMA_INTEGRATION_" + strings.ToUpper(driver))
			if raw == "" {
				t.Skip("未配置跨 Schema 真实数据库验收环境")
			}
			var fixture struct {
				Connection   store.Connection `json:"connection"`
				FirstSchema  string           `json:"first_schema"`
				SecondSchema string           `json:"second_schema"`
			}
			if err := json.Unmarshal([]byte(raw), &fixture); err != nil {
				t.Fatal("invalid schema integration configuration")
			}
			if fixture.FirstSchema == "" || fixture.SecondSchema == "" || fixture.FirstSchema == fixture.SecondSchema {
				t.Fatal("requires two distinct fixture schemas")
			}
			ctx := context.Background()
			c := &catalog.Catalog{Path: filepath.Join(t.TempDir(), "catalog"), Timeout: 10 * time.Second, AsOf: store.DefaultAsOf}
			fixture.Connection.AllowedSchemas = []string{fixture.FirstSchema, fixture.SecondSchema}
			fixture.Connection.Schema = ""
			if (driver == "mysql" || driver == "oceanbase") {
				fixture.Connection.Database = ""
			}
			if err := c.Add(ctx, catalog.Database{ID: "schema_join", Name: "Schema acceptance", Driver: driver, Connection: &fixture.Connection}); err != nil {
				t.Fatal(err)
			}
			d, err := c.Get("schema_join")
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Discovery.Tables) != 0 {
				t.Fatal("creation eagerly enrolled candidates")
			}
			page, err := c.Candidates(ctx, d.ID, "records", 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 2 || len(page.Items) != 1 {
				t.Fatalf("same-name discovery/search/page: %+v", page)
			}
			page2, err := c.Candidates(ctx, d.ID, "records", 1, 1)
			if err != nil {
				t.Fatal(err)
			}
			if page.Items[0].Name == page2.Items[0].Name {
				t.Fatal("pagination conflated same-name tables")
			}
			all, err := c.Candidates(ctx, d.ID, "", 200, 0)
			if err != nil {
				t.Fatal(err)
			}
			find := func(owner, table string) string {
				for _, candidate := range all.Items {
					if candidate.Schema == owner && strings.EqualFold(candidate.TableName, table) {
						return candidate.Name
					}
				}
				t.Fatalf("missing fixture %s.%s", owner, table)
				return ""
			}
			recordsA, recordsB := find(fixture.FirstSchema, "records"), find(fixture.SecondSchema, "records")
			orders, customers := find(fixture.FirstSchema, "orders"), find(fixture.SecondSchema, "customers")
			d, err = c.EnrollTables(ctx, d.ID, d.Revision, []string{recordsA, orders, customers})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range d.Config.Tables {
				yes := true
				p.Enabled = &yes
				cols := []string{}
				for _, name := range p.Columns {
					if !strings.EqualFold(name, "secret") {
						cols = append(cols, name)
					}
				}
				p.Columns = cols
			}
			if err := c.Save(ctx, *d); err != nil {
				t.Fatal(err)
			}
			d, _ = c.Get(d.ID)
			svc, err := c.Open(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			defer svc.Store.Close()
			preview, err := svc.Store.Browse(ctx, store.BrowseOptions{Table: recordsA, Limit: 10})
			if err != nil || len(preview.Rows) != 1 {
				t.Fatalf("qualified preview: %+v %v", preview, err)
			}
			server := mcpserver.New(svc, mcpserver.Options{Managed: true, AuthMode: "none", DefaultUserID: 1, Tools: []string{"query", "list_tables", "describe_table", "raw_query"}})
			left, right := mcp.NewInMemoryTransports()
			ss, err := server.Connect(ctx, left, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "network-schema-acceptance", Version: "1"}, nil)
			session, err := client.Connect(ctx, right, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			call := func(query string, wantError bool) {
				t.Helper()
				out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "query", Arguments: map[string]any{"sql": query}})
				failed := err != nil || out.IsError
				if failed != wantError {
					t.Fatalf("query expectation error=%v: %s: %+v %v", wantError, query, out, err)
				}
			}
			call("SELECT * FROM "+recordsA, false)
			call("SELECT * FROM "+recordsB, true)
			joins := []string{
				fmt.Sprintf("SELECT o.order_id,c.customer_name FROM %s o JOIN %s c ON c.customer_id=o.customer_id", orders, customers),
				fmt.Sprintf("SELECT o.order_id,c.customer_name FROM (SELECT order_id,customer_id FROM %s) o JOIN %s c ON c.customer_id=o.customer_id", orders, customers),
				fmt.Sprintf("WITH o AS(SELECT order_id,customer_id FROM %s) SELECT o.order_id,c.customer_name FROM o JOIN %s c ON c.customer_id=o.customer_id", orders, customers),
			}
			for _, q := range joins {
				out, err := svc.Query(ctx, q, 1, 10)
				if err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(out.QueryResult)
				if out.RowCount != 1 || !strings.Contains(string(data), "Alice") {
					t.Fatalf("wrong JOIN result: %s", data)
				}
				call(q, false)
			}
			for _, q := range []string{"SELECT secret FROM " + orders, "SELECT * FROM outside.orders", "SELECT * FROM " + orders + " o WHERE EXISTS(SELECT 1 FROM " + recordsB + ")"} {
				call(q, true)
			}
			d, _ = c.Get(d.ID)
			for _, p := range d.Config.Tables {
				if strings.EqualFold(p.Table, customers) {
					no := false
					p.Enabled = &no
				}
			}
			if err := c.Save(ctx, *d); err != nil {
				t.Fatal(err)
			}
			revoked, err := c.Open(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range joins {
				if _, err := revoked.Query(ctx, q, 1, 10); err == nil {
					t.Fatal("revoked JOIN accepted")
				}
			}
			revoked.Store.Close()
			// Removing a schema is independently enforced by the raw test execution path.
			d.Connection.AllowedSchemas = []string{fixture.FirstSchema}
			d.Connection.Schema = fixture.FirstSchema
			if (driver == "mysql" || driver == "oceanbase") {
				d.Connection.Database = fixture.FirstSchema
			}
			if err := c.Save(ctx, *d); err != nil {
				t.Fatal(err)
			}
			d, _ = c.Get(d.ID)
			scoped, err := c.OpenStore(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			defer scoped.Close()
			if _, err := scoped.RawQuery(ctx, joins[0], 10); err == nil {
				t.Fatal("raw query escaped schema whitelist")
			}
			single, err := c.Candidates(ctx, d.ID, "records", 50, 0)
			if err != nil || single.Total != 1 {
				t.Fatalf("single-schema discovery: %+v %v", single, err)
			}
		})
	}
}
