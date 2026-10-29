package platform_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"datascope-server/internal/adminapi"
	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"datascope-server/internal/mcpserver"
	"datascope-server/internal/platform"
	"datascope-server/internal/store"
)

func TestEmptyCatalogCanStartAndManageConnections(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	a, err := audit.Open(filepath.Join(dir, "audit.sqlite"))
	must(t, err)
	defer a.Close()
	m, err := management.Open(filepath.Join(dir, "management.sqlite"), a)
	must(t, err)
	defer m.Close()
	m.AllowedRoots = []string{dir}
	must(t, m.Bootstrap(ctx, "admin", "empty-startup-password"))
	c, err := catalog.BootstrapManaged(ctx, m, filepath.Join(dir, "catalog"), nil, "missing-rewrite.yml", store.DefaultAsOf, time.Second, filepath.Join(dir, "missing.json"))
	must(t, err)
	ds, err := c.List()
	must(t, err)
	if len(ds) != 0 {
		t.Fatal("demo source unexpectedly imported")
	}
	must(t, platform.InitializeDemoRealm(ctx, c, ""))
	app := &application.Service{Management: m, Catalog: c}
	h := adminapi.PlatformHandler(app, adminapi.ManagedHandler(c, &adminapi.Inspector{Endpoint: "http://127.0.0.1:8080/mcp"}))
	srv := httptest.NewServer(h)
	defer srv.Close()
	status, body := request(t, srv.Client(), "GET", srv.URL+"/healthz", nil)
	if status != 200 {
		t.Fatalf("health %d: %s", status, body)
	}
	client := login(t, srv.URL, "admin", "empty-startup-password")
	status, body = request(t, client, "GET", srv.URL+"/api/databases", nil)
	if status != 200 || string(body) != "[]\n" {
		t.Fatalf("empty list %d: %s", status, body)
	}
	// Connection probes run at platform scope even when no selected source exists.
	status, body = request(t, client, "POST", srv.URL+"/api/management/call", map[string]any{"action": "test_database_connection", "arguments": map[string]any{"database": map[string]any{"driver": "dameng", "connection": map[string]any{"host": "127.0.0.1", "port": 5236, "schema": "BUSINESS", "username": "reader", "password_env": "ADMIN_PASSWORD"}}}})
	if status == 200 || status == 500 {
		t.Fatalf("invalid secret reference was not a validation failure: %d %s", status, body)
	}
	reader, err := m.CreateUser(ctx, "reader", "Reader", "reader-test-password")
	must(t, err)
	_, err = app.Call(guard.WithUser(ctx, reader), "test_database_connection", application.Input{Database: &catalog.Database{Driver: "dameng"}})
	if !errors.Is(err, management.ErrDenied) {
		t.Fatalf("non-admin connection probe: %v", err)
	}
	mcp, err := mcpserver.ManagedHTTPHandler(c, nil, mcpserver.Options{Management: m}, "oauth", "http://127.0.0.1:8081", "http://127.0.0.1:8080/mcp", "/mcp")
	must(t, err)
	w := httptest.NewRecorder()
	mcp.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatal("MCP empty health failed")
	}
}
