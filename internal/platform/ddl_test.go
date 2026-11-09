package platform_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/ddl"
	"datascope-server/internal/management"
	"datascope-server/internal/managementmcp"
	"datascope-server/internal/mcpserver"
	"datascope-server/internal/oauth"
)

func TestDDLManagementSwitchPermissionsAndServerDiscovery(t *testing.T) {
	f := setup(t)
	in := application.Input{DatabaseID: "default", SQL: "CREATE TABLE added(id INTEGER PRIMARY KEY)"}
	if _, err := f.app.Call(f.ctx, "preview_ddl", in); err == nil {
		t.Fatal("DDL enabled by default")
	}
	d, err := f.c.Get("default")
	must(t, err)
	enabled := true
	_, err = f.app.Call(f.readerContext(), "update_database", application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: d.Revision, DDLEnabled: &enabled}})
	if err == nil {
		t.Fatal("reader changed switch")
	}
	_, err = f.app.Call(f.ctx, "update_database", application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: d.Revision, DDLEnabled: &enabled}})
	must(t, err)
	d, err = f.c.Get("default")
	must(t, err)
	if !f.c.CanEditSchema(d) {
		t.Fatal("existing local source not editable")
	}
	if _, err = f.app.Call(f.readerContext(), "preview_ddl", in); err == nil {
		t.Fatal("reader previewed DDL")
	}
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: d.ID, Permissions: []string{"config.read", "config.write", "ddl.write"}}))
	// A full configuration save cannot bypass administrator-only enablement.
	copy := *d
	disabled := false
	copy.DDLEnabled = &disabled
	if _, err = f.app.Call(f.readerContext(), "save_database", application.Input{DatabaseID: d.ID, Database: &copy}); err == nil {
		t.Fatal("non-admin disabled DDL through full save")
	}
	out, err := f.app.Call(f.readerContext(), "preview_ddl", in)
	must(t, err)
	p := out.(*ddl.Plan)
	out, err = f.app.Call(f.readerContext(), "apply_ddl", application.Input{DatabaseID: d.ID, PlanID: p.ID, RequestID: "reader-create"})
	must(t, err)
	execution := out.(*ddl.Execution)
	if execution.Status != "succeeded" || execution.DiscoveryStatus != "review_required" {
		t.Fatalf("%+v", execution)
	}
	d, err = f.c.Get(d.ID)
	must(t, err)
	if d.Discovery.Pending == nil {
		t.Fatal("server did not discover change")
	}
	for _, table := range d.Discovery.Tables {
		if table.Name == "added" {
			t.Fatal("DDL auto-approved new table")
		}
	}
	source, err := f.m.Source(f.ctx, d.ID)
	must(t, err)
	backup := filepath.Join(filepath.Dir(source.Path), "ddl", "backups", execution.BackupID, "database.sqlite")
	if _, err = os.Stat(backup); err != nil {
		t.Fatal(err)
	}
	if err = f.m.ValidatePath(backup); err == nil {
		t.Fatal("DDL backup registered as a business source")
	}
	// Plans survive rebuilding the service and catalog instances.
	c, err := catalog.BootstrapManaged(context.Background(), f.m, f.c.Path, nil, "unused", f.c.AsOf, f.c.Timeout, "")
	must(t, err)
	app := &application.Service{Management: f.m, Catalog: c}
	out, err = app.Call(f.readerContext(), "apply_ddl", application.Input{DatabaseID: d.ID, PlanID: p.ID, RequestID: "reader-create"})
	must(t, err)
	if out.(*ddl.Execution).ID != execution.ID {
		t.Fatal("retry not durable")
	}
	_, err = f.app.Call(f.ctx, "update_database", application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: d.Revision, DDLEnabled: &disabled}})
	must(t, err)
	if _, err = f.app.Call(f.ctx, "list_ddl_objects", application.Input{DatabaseID: d.ID}); err == nil {
		t.Fatal("admin bypassed disabled switch")
	}
}

func TestDDLOAuthAdminMCPParity(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	issuer := "http://" + server.Listener.Addr().String()
	resource := issuer + "/mcp/admin"
	as, err := oauth.New(oauth.Config{Catalog: f.c, Management: f.m, Issuer: issuer, Resource: resource, KeyPath: filepath.Join(f.dir, "signing.pem"), StatePath: f.m.Path, ResourceAllowed: func(r string) bool { return r == resource }, DatabaseForResource: func(string) (string, error) { return "", nil }}, nil)
	must(t, err)
	t.Cleanup(func() { as.Close() })
	handler, err := mcpserver.HTTPHandlerWithManagement(managementmcp.New(f.app), nil, "oauth", issuer, resource, "/mcp/admin", true, f.m, "")
	must(t, err)
	server.Config.Handler = as.Handler(handler)
	server.Start()
	t.Cleanup(server.Close)
	token, err := as.SignEmployeeForResource(ctx, f.admin.ID, resource)
	must(t, err)
	session := connectMCP(t, resource, token)
	invoke := func(tool, action string, args map[string]any) json.RawMessage {
		t.Helper()
		out, err := callMCP(t, session, tool, map[string]any{"action": action, "args": args})
		must(t, err)
		if out.IsError {
			t.Fatalf("%s %s: %+v", tool, action, out)
		}
		raw, err := json.Marshal(out.StructuredContent)
		must(t, err)
		return raw
	}
	d, err := f.c.Get("default")
	must(t, err)
	invoke("datascope_manage", "update", map[string]any{"resourceType": "database", "database_id": d.ID, "database": map[string]any{"id": d.ID, "revision": d.Revision, "ddl_enabled": true}})
	raw := invoke("datascope_inspect", "preview", map[string]any{"resourceType": "ddl", "database_id": d.ID, "sql": "CREATE TABLE mcp_items(id INTEGER PRIMARY KEY,name TEXT);CREATE VIEW mcp_view AS SELECT * FROM mcp_items;CREATE TRIGGER mcp_trigger AFTER INSERT ON mcp_items BEGIN UPDATE mcp_items SET name=upper(new.name) WHERE id=new.id;END;"})
	var p ddl.Plan
	must(t, json.Unmarshal(raw, &p))
	if p.ID == "" {
		t.Fatalf("preview %s", raw)
	}
	raw = invoke("datascope_manage", "apply", map[string]any{"resourceType": "ddl", "database_id": d.ID, "plan_id": p.ID, "request_id": "mcp-create"})
	var execution ddl.Execution
	must(t, json.Unmarshal(raw, &execution))
	if execution.Status != "succeeded" {
		t.Fatalf("apply %s", raw)
	}
	invoke("datascope_query", "list", map[string]any{"resourceType": "ddl", "database_id": d.ID, "object_type": "view"})
	raw = invoke("datascope_query", "get", map[string]any{"resourceType": "ddl", "database_id": d.ID, "object_type": "table", "name": "mcp_items"})
	var object ddl.Object
	must(t, json.Unmarshal(raw, &object))
	if len(object.Definition.Columns) != 2 {
		t.Fatalf("get %s", raw)
	}
	invoke("datascope_query", "execution", map[string]any{"resourceType": "ddl", "database_id": d.ID, "execution_id": execution.ID})
	invoke("datascope_query", "history", map[string]any{"resourceType": "ddl", "database_id": d.ID})
	raw = invoke("datascope_inspect", "preview", map[string]any{"resourceType": "ddl", "database_id": d.ID, "changes": []map[string]any{{"operation": "update", "object_type": "column", "name": "name", "parent_name": "mcp_items", "definition": map[string]any{"sql": "name TEXT NOT NULL DEFAULT 'unknown'"}}}})
	must(t, json.Unmarshal(raw, &p))
	invoke("datascope_manage", "apply", map[string]any{"resourceType": "ddl", "database_id": d.ID, "plan_id": p.ID, "request_id": "mcp-alter"})
	d, err = f.c.Get(d.ID)
	must(t, err)
	invoke("datascope_manage", "update", map[string]any{"resourceType": "database", "database_id": d.ID, "database": map[string]any{"id": d.ID, "revision": d.Revision, "ddl_enabled": false}})
	out, err := callMCP(t, session, "datascope_query", map[string]any{"action": "list", "args": map[string]any{"resourceType": "ddl", "database_id": d.ID}})
	if err == nil && !out.IsError {
		t.Fatal("MCP bypassed disabled switch")
	}
}

func TestDDLHTTPSharedActions(t *testing.T) {
	f := setup(t)
	server := api(t, f)
	client := login(t, server.URL, "admin", "admin-test-password")
	d, err := f.c.Get("default")
	must(t, err)
	status, body := request(t, client, "POST", server.URL+"/api/management/call", map[string]any{"action": "update_database", "arguments": map[string]any{"database_id": d.ID, "database": map[string]any{"id": d.ID, "revision": d.Revision, "ddl_enabled": true}}})
	if status != 200 {
		t.Fatalf("enable %d %s", status, body)
	}
	status, body = request(t, client, "POST", server.URL+"/api/management/call", map[string]any{"action": "preview_ddl", "arguments": map[string]any{"database_id": d.ID, "changes": []map[string]any{{"operation": "create", "object_type": "table", "name": "http_items", "definition": map[string]any{"columns": []map[string]any{{"name": "id", "type": "INTEGER", "primary_key": 1}}}}}}})
	if status != 200 {
		t.Fatalf("preview %d %s", status, body)
	}
	var p ddl.Plan
	must(t, json.Unmarshal(body, &p))
	status, body = request(t, client, "POST", server.URL+"/api/management/call", map[string]any{"action": "apply_ddl", "arguments": map[string]any{"database_id": d.ID, "plan_id": p.ID, "request_id": "http-create"}})
	if status != 200 {
		t.Fatalf("apply %d %s", status, body)
	}
	var e ddl.Execution
	must(t, json.Unmarshal(body, &e))
	if e.Status != "succeeded" {
		t.Fatalf("%s", body)
	}
}
