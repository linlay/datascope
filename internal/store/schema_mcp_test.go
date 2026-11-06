package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"datascope-server/internal/mcpserver"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/service"
	"datascope-server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOracleOptionalSchemaDiscoveryAndMCPIsolation(t *testing.T) {
	ctx := context.Background()
	st, probe := store.NewOracleSchemaFixture([]string{"A", "B"})
	defer st.Close()
	names, err := st.TableNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "A.RECORDS,B.RECORDS" || probe.ColumnReads != 0 {
		t.Fatalf("candidate names or lazy discovery: %v %+v", names, probe)
	}
	a, err := st.Describe(ctx, "A.RECORDS")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "A.RECORDS" || a.Comment != "A table" {
		t.Fatalf("owner lost: %+v", a)
	}
	st.RegisteredSchema = []store.TableSchema{*a}
	cfg, err := rewrite.ParseConfig([]byte(`version: 1
schemes: [{scheme: NONE, name: None}]
tables:
 - table: a.records
   scheme: NONE
   columns: [ID, VALUE]
   passthrough: true
`))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, st, cfg, store.DefaultAsOf)
	if err != nil {
		t.Fatal(err)
	}
	server := mcpserver.New(svc, mcpserver.Options{Managed: true, Tools: []string{"list_tables", "describe_table", "query", "raw_query"}, AuthMode: "none", DefaultUserID: 1})
	left, right := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, left, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-acceptance", Version: "1"}, nil)
	session, err := client.Connect(ctx, right, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tables, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_tables"})
	if err != nil || tables.IsError {
		t.Fatalf("list: %v %v", tables, err)
	}
	raw, _ := json.Marshal(tables.StructuredContent)
	if !strings.Contains(string(raw), "A.RECORDS") || strings.Contains(string(raw), "B.RECORDS") {
		t.Fatalf("candidate exposed: %s", raw)
	}
	for _, query := range []string{`SELECT r.ID,r.VALUE FROM A.RECORDS r`, `WITH x AS (SELECT ID,VALUE FROM A.RECORDS) SELECT * FROM x`, `SELECT * FROM (SELECT ID,VALUE FROM A.RECORDS) x`} {
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "query", Arguments: map[string]any{"sql": query}})
		if err != nil || out.IsError {
			t.Fatalf("query: %s: %+v %v", query, out, err)
		}
	}
	page, err := svc.Store.Browse(ctx, store.BrowseOptions{Table: "A.RECORDS", Limit: 10})
	if err != nil || len(page.Rows) != 1 || page.Rows[0]["VALUE"] != "A value" {
		t.Fatalf("preview lost owner: %+v %v", page, err)
	}
	for _, query := range []string{`SELECT * FROM B.RECORDS`, `WITH x AS(SELECT * FROM B.RECORDS) SELECT * FROM x`, `SELECT * FROM (SELECT * FROM B.RECORDS) x`, `SELECT * FROM A.RECORDS a WHERE EXISTS(SELECT 1 FROM B.RECORDS)`, `SELECT * FROM A.RECORDS UNION SELECT * FROM B.RECORDS`} {
		before := len(probe.BusinessReads)
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "query", Arguments: map[string]any{"sql": query}})
		if err == nil && !out.IsError {
			t.Fatalf("unpublished query accepted: %s", query)
		}
		if len(probe.BusinessReads) != before {
			t.Fatal("denied query reached database")
		}
	}
	if _, err := svc.Store.RawQuery(ctx, `SELECT * FROM B.RECORDS`, 10); err != nil {
		t.Fatalf("test raw query must bypass publication: %v", err)
	}
	for _, owner := range probe.BusinessReads[:len(probe.BusinessReads)-1] {
		if owner != "A" {
			t.Fatalf("wrong physical owner: %v", probe.BusinessReads)
		}
	}
	onlyA, p := store.NewOracleSchemaFixture([]string{"A"})
	defer onlyA.Close()
	names, err = onlyA.TableNames(ctx)
	if err != nil || strings.Join(names, ",") != "A.RECORDS" || p.ColumnReads != 0 {
		t.Fatalf("scope leak: %v %v", names, err)
	}
	none, _ := store.NewOracleSchemaFixture([]string{})
	defer none.Close()
	names, err = none.TableNames(ctx)
	if err != nil || len(names) != 0 {
		t.Fatalf("zero readable tables must not fail connection: %v %v", names, err)
	}
}
