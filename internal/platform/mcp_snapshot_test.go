package platform_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/mcpserver"
	"datascope-server/internal/requesttiming"
)

func TestMCPRequestSnapshotReadsOnceAndObservesChanges(t *testing.T) {
	f := setup(t)
	d, err := f.c.Get("default")
	must(t, err)
	d.Authentication = &catalog.Authentication{Type: "local", Source: "GLOBAL", Access: &management.AuthPolicy{
		ID: "test-public", Name: "Test public", Mode: "NONE", BusinessUserID: 10, Permissions: []string{"query"},
	}}
	must(t, f.c.Save(f.ctx, *d))
	must(t, f.c.Add(f.ctx, catalog.Database{ID: "unrelated", Name: "Unrelated", Driver: "sqlite", Path: d.Path}))
	h, err := mcpserver.ManagedHTTPHandler(f.c, f.st, mcpserver.Options{Management: f.m, Application: f.app}, "none", "", "http://localhost/mcp", "/mcp")
	must(t, err)
	call := func(path string, status int, body string) map[string]int {
		t.Helper()
		ctx, trace := requesttiming.New(context.Background())
		r := httptest.NewRequest("POST", path, strings.NewReader(body)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if status == 200 && (strings.Contains(w.Body.String(), `"isError":true`) || strings.Contains(w.Body.String(), `"error":`)) {
			t.Fatalf("MCP request failed: %s", w.Body.String())
		}
		if strings.Contains(body, "SELECT id") && !strings.Contains(w.Body.String(), `"row_count":2`) {
			t.Fatalf("row permissions changed: %s", w.Body.String())
		}
		_, _, counts := trace.Snapshot()
		return counts
	}
	ping := func(path string, status int) map[string]int {
		return call(path, status, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	}
	counts := ping("/mcp/default", 200)
	if counts["read_state"] != 1 || counts["resolve_auth"] != 1 || counts["business_get"] != 1 || counts["config_state"] != 4 {
		t.Fatalf("HTTP ping reloads configuration: %v", counts)
	}
	query := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query","arguments":{"sql":"SELECT id FROM items ORDER BY id"}}}`
	call("/mcp/default", 200, query) // Warm query-service initialization.
	counts = call("/mcp/default", 200, query)
	if counts["read_state"] != 1 || counts["resolve_auth"] != 1 || counts["business_get"] != 1 || counts["config_state"] != 5 {
		t.Fatalf("query view lookup escaped the request snapshot: %v", counts)
	}
	counts = call("/mcp/default", 200, `{"jsonrpc":"2.0","id":3,"method":"resources/templates/list"}`)
	if counts["read_state"] != 1 || counts["config_state"] != 5 {
		t.Fatalf("resource lookup escaped the request snapshot: %v", counts)
	}
	// The same handler must observe disabling, re-enabling and route changes on
	// the next HTTP request, rather than retaining configuration across requests.
	d, err = f.c.Get("default")
	must(t, err)
	d.MCPEnabled = false
	must(t, f.c.Save(f.ctx, *d))
	ping("/mcp/default", 403)
	d, err = f.c.Get("default")
	must(t, err)
	d.MCPEnabled = true
	d.MCPPath = "finance"
	must(t, f.c.Save(f.ctx, *d))
	ping("/mcp/default", 404)
	ping("/mcp/finance", 200)
	// Revoking anonymous access must also take effect without rebuilding h.
	d, err = f.c.Get("default")
	must(t, err)
	d.Authentication.Access = &management.AuthPolicy{ID: "test-account", Name: "Test account", Mode: "ACCOUNT", ProviderID: "business-account"}
	must(t, f.c.Save(f.ctx, *d))
	ping("/mcp/finance", 401)
}
