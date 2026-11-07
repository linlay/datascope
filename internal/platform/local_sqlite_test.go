package platform_test

import (
	"database/sql"
	"datascope-server/internal/adminapi"
	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedSQLiteCreationAndSchema(t *testing.T) {
	f := setup(t)
	server := httptest.NewServer(adminapi.PlatformHandler(f.app, adminapi.ManagedHandler(f.c, &adminapi.Inspector{})))
	defer server.Close()
	client := login(t, server.URL, "admin", "admin-test-password")
	status, body := request(t, client, "POST", server.URL+"/api/databases", map[string]any{"name": "Business", "driver": "sqlite", "path": "business.sqlite3", "create_sqlite": true})
	if status != 200 {
		t.Fatalf("create: %d %s", status, body)
	}
	var d application.DatabaseView
	must(t, json.Unmarshal(body, &d))
	if !d.SchemaEditable || d.Path != filepath.Join(f.c.LocalSQLiteRoot(), "business.sqlite3") {
		t.Fatalf("unexpected local source: %+v", d)
	}
	for _, query := range []string{"CREATE TABLE clients(id INTEGER PRIMARY KEY, name TEXT)", "ALTER TABLE clients ADD COLUMN email TEXT", "CREATE INDEX clients_name ON clients(name)"} {
		_, err := f.app.Call(f.ctx, "edit_sqlite_schema", application.Input{DatabaseID: d.ID, SQL: query})
		must(t, err)
	}
	for _, query := range []string{"ATTACH DATABASE '/tmp/escape.db' AS other", "PRAGMA writable_schema=ON", "INSERT INTO clients(name) VALUES ('x')", "CREATE TABLE bad(id); DROP TABLE clients", "CREATE TABLE copy AS SELECT * FROM clients", "CREATE TEMP TABLE temp_items(id)", "CREATE TABLE temp.temp_items(id)"} {
		if _, err := f.app.Call(f.ctx, "edit_sqlite_schema", application.Input{DatabaseID: d.ID, SQL: query}); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	if _, err := f.app.Call(f.readerContext(), "edit_sqlite_schema", application.Input{DatabaseID: d.ID, SQL: "DROP TABLE clients"}); err == nil {
		t.Fatal("reader edited schema")
	}
	if _, err := f.app.Call(f.ctx, "edit_sqlite_schema", application.Input{DatabaseID: "default", SQL: "DROP TABLE items"}); err == nil {
		t.Fatal("edited external SQLite")
	}
	if _, err := f.c.CreateLocalSQLite(f.ctx, catalog.Database{Name: "Duplicate", Driver: "sqlite", Path: "business.sqlite3"}); err == nil {
		t.Fatal("overwrote existing file")
	}
	db, err := sql.Open("sqlite", d.Path)
	must(t, err)
	defer db.Close()
	var count int
	must(t, db.QueryRow("SELECT count(*) FROM clients").Scan(&count))
	if count != 0 {
		t.Fatal("forbidden DML ran")
	}
	updated, err := f.c.Discover(f.ctx, d.ID)
	must(t, err)
	if updated.Discovery.Pending == nil {
		t.Fatal("schema change was not staged for review")
	}
	for _, name := range []string{"../escape.db", "/tmp/escape.db", "business.db?mode=rwc"} {
		if _, err = f.c.CreateLocalSQLite(f.ctx, catalog.Database{Name: "Bad", Driver: "sqlite", Path: name}); err == nil {
			t.Fatalf("accepted unsafe filename %s", name)
		}
	}
	// An invalid registration must remove the freshly created file.
	if _, err = f.c.CreateLocalSQLite(f.ctx, catalog.Database{Driver: "sqlite", Path: "failed.db"}); err == nil {
		t.Fatal("accepted missing display name")
	}
	if _, err = os.Stat(filepath.Join(f.dir, "database", "failed.db")); !os.IsNotExist(err) {
		t.Fatal("failed registration left a file")
	}
	link := filepath.Join(f.dir, "database", "link.db")
	must(t, os.Symlink(f.st.Path(), link))
	if f.c.CanEditSchema(&catalog.Database{Driver: "sqlite", Path: link}) {
		t.Fatal("symlink escaped schema scope")
	}
}
