package platform_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
)

func TestDatabaseSQLExamplesPersistenceAndIsolation(t *testing.T) {
	f := setup(t)
	must(t, f.c.Add(f.ctx, catalog.Database{ID: "other", Name: "Other", Driver: "sqlite", Path: f.st.Path()}))
	d, err := f.c.Get("default")
	must(t, err)
	d.SQLExamples = []catalog.SQLExample{{Label: "Items", SQL: "SELECT id FROM items"}}
	out, err := f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d})
	must(t, err)
	if !reflect.DeepEqual(out.(application.DatabaseView).SQLExamples, d.SQLExamples) {
		t.Fatal("save omitted SQL examples")
	}
	// The workbench receives examples from the database list, not the detail endpoint.
	listed, err := f.app.Call(f.ctx, "list_databases", application.Input{})
	must(t, err)
	found := false
	for _, database := range listed.([]application.DatabaseView) {
		if database.ID == d.ID {
			found = true
			if !reflect.DeepEqual(database.SQLExamples, d.SQLExamples) {
				t.Fatal("workbench database list omitted SQL examples")
			}
		}
	}
	if !found {
		t.Fatal("workbench database missing from list")
	}

	source, err := f.m.Source(f.ctx, d.ID)
	must(t, err)
	databaseYAML, err := os.ReadFile(filepath.Join(source.Path, "database.yml"))
	must(t, err)
	if strings.Contains(string(databaseYAML), "sqlExamples:") {
		t.Fatal("examples remain embedded in database.yml")
	}
	path := filepath.Join(source.Path, "sql-examples.yml")
	raw, err := os.ReadFile(path)
	must(t, err)
	if !strings.Contains(string(raw), "sqlExamples:") || !strings.Contains(string(raw), "SELECT id FROM items") {
		t.Fatal("examples were not persisted in the database sql-examples.yml")
	}
	// A YAML edit must reach the database response without a global fallback.
	must(t, os.WriteFile(path, []byte(strings.Replace(string(raw), "SELECT id FROM items", "SELECT name FROM items", 1)), 0600))
	out, err = f.app.Call(f.ctx, "get_database", application.Input{DatabaseID: d.ID})
	must(t, err)
	view := out.(application.DatabaseView)
	if len(view.SQLExamples) != 1 || view.SQLExamples[0].SQL != "SELECT name FROM items" {
		t.Fatal("database response did not reload YAML examples")
	}
	other, err := f.c.Get("other")
	must(t, err)
	if len(other.SQLExamples) != 0 {
		t.Fatal("examples leaked into another database")
	}
	// Examples are configuration: query permission alone cannot change them.
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: d.ID, Permissions: []string{"config.read", "query"}}))
	unauthorized := view.Database
	unauthorized.SQLExamples = []catalog.SQLExample{{Label: "Unauthorized", SQL: "SELECT secret FROM items"}}
	_, err = f.app.Call(f.readerContext(), "save_database", application.Input{DatabaseID: d.ID, Database: &unauthorized})
	if err == nil {
		t.Fatal("reader changed configuration")
	}
	view.SQLExamples = nil
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: &view.Database})
	must(t, err)
	d, err = f.c.Get(d.ID)
	must(t, err)
	if len(d.SQLExamples) != 0 {
		t.Fatal("removing examples was treated as a no-op")
	}
}
