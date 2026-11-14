package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/view"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDatabaseDocumentationPatchPersistenceAndAuthorization(t *testing.T) {
	f := setup(t)
	d, err := f.c.Get("default")
	must(t, err)
	description, documentation := "数据库简介", "# 业务范围\n\n```sql\nSELECT id FROM items;\n```\n"
	input := application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: d.Revision, Description: &description, Documentation: &documentation}}
	_, err = f.app.Call(f.readerContext(), "update_database", input)
	if !errors.Is(err, management.ErrDenied) {
		t.Fatalf("reader edited documentation: %v", err)
	}
	_, err = f.app.Call(f.ctx, "update_database", input)
	must(t, err)
	if _, err = f.app.Call(f.ctx, "update_database", input); err == nil {
		t.Fatal("accepted stale revision")
	}
	saved, err := f.c.Get(d.ID)
	must(t, err)
	if saved.Description == nil || *saved.Description != description || saved.Documentation == nil || *saved.Documentation != documentation {
		t.Fatal("documentation lost")
	}
	source, err := f.m.Source(f.ctx, d.ID)
	must(t, err)
	raw, err := os.ReadFile(filepath.Join(source.Path, "database.yml"))
	must(t, err)
	if !strings.Contains(string(raw), "documentation:") || !strings.Contains(string(raw), "数据库简介") {
		t.Fatal("documentation absent from YAML")
	}
	// A legacy name-only patch must preserve both new fields.
	_, err = f.app.Call(f.ctx, "update_database", application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: saved.Revision, Name: "Renamed"}})
	must(t, err)
	saved, err = f.c.Get(d.ID)
	must(t, err)
	if *saved.Documentation != documentation || *saved.Description != description {
		t.Fatal("name patch cleared documentation")
	}
	value := f.app.DatabaseView(f.readerContext(), *saved)
	if value.Documentation == nil || *value.Documentation != documentation {
		t.Fatal("database user cannot see documentation")
	}
	empty := ""
	_, err = f.app.Call(f.ctx, "update_database", application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: saved.Revision, Description: &empty}})
	must(t, err)
	saved, err = f.c.Get(d.ID)
	must(t, err)
	if *saved.Description != "" || *saved.Documentation != documentation {
		t.Fatal("clear or omitted field semantics broken")
	}
	// Metadata editing must not require a working business connection.
	must(t, os.Rename(f.st.Path(), f.st.Path()+".offline"))
	must(t, os.WriteFile(f.st.Path(), []byte("not an openable SQLite database"), 0600))
	_, err = f.app.Call(f.ctx, "update_database", application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: saved.Revision, Documentation: &empty}})
	must(t, err)
	saved, err = f.c.Get(d.ID)
	must(t, err)
	if *saved.Documentation != "" {
		t.Fatal("cannot clear documentation")
	}
}

func TestDatabaseDocumentationMCPResourceScopeAndLiveUpdates(t *testing.T) {
	env := setupRealms(t)
	f := env.f
	d, err := f.c.Get("default")
	must(t, err)
	description, body := "Database reference", "# BODY_ONLY_ON_READ\nSELECT id FROM items;"
	d.Description, d.Documentation = &description, &body
	d.Views = []view.Definition{{ID: "sample", Name: "Sample", Type: "view", HTML: "<!doctype html><p>PRIVATE_VIEW</p>", Published: true}}
	must(t, f.c.Save(f.ctx, *d))
	a := env.accounts["default"]
	a.Permissions = []string{"knowledge.read"}
	a, err = f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
	must(t, err)
	tokens, _, _ := employeeTokens(t, env, "default", "employee-default-password")
	session := connectMCP(t, env.resource+"/default", tokens["access"])
	ctx := context.Background()
	initial := session.InitializeResult()
	if !strings.Contains(initial.Instructions, description) || strings.Contains(initial.Instructions, "BODY_ONLY_ON_READ") {
		t.Fatal("incorrect initialization guidance", initial.Instructions)
	}
	uri := "datascope://database/default/documentation"
	list, err := session.ListResources(ctx, nil)
	must(t, err)
	raw, _ := json.Marshal(list)
	if len(list.Resources) != 1 || list.Resources[0].URI != uri || strings.Contains(string(raw), "BODY_ONLY_ON_READ") {
		t.Fatalf("incorrect resource listing: %s", raw)
	}
	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	must(t, err)
	if len(result.Contents) != 1 || result.Contents[0].Text != body || result.Contents[0].MIMEType != "text/markdown" {
		t.Fatal(result)
	}
	for _, forbidden := range []string{"datascope://database/other/documentation", view.URI(d.ID, "sample")} {
		if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: forbidden}); err == nil {
			t.Fatal("read forbidden resource", forbidden)
		}
	}
	d, err = f.c.Get(d.ID)
	must(t, err)
	updated := "# Updated reference"
	d.Documentation = &updated
	must(t, f.c.Save(f.ctx, *d))
	result, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	must(t, err)
	if result.Contents[0].Text != updated {
		t.Fatal("stale documentation")
	}
	d, err = f.c.Get(d.ID)
	must(t, err)
	d.Documentation = nil
	must(t, f.c.Save(f.ctx, *d))
	if _, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri}); err == nil {
		t.Fatal("cleared resource remains readable")
	}
	a.Permissions = nil
	_, err = f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
	must(t, err)
	if _, err = session.ListResources(ctx, nil); err == nil {
		t.Fatal("revoked account can read resources")
	}
}

func TestDatabaseDocumentationFullSaveChecksChangesAndPermissions(t *testing.T) {
	f := setup(t)
	d, err := f.c.Get("default")
	must(t, err)
	text := "Updated reference"
	d.Documentation = &text
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: d.ID, BusinessUserID: 10, Permissions: []string{"config.read"}}))
	if _, err = f.app.Call(f.readerContext(), "save_database", application.Input{DatabaseID: d.ID, Database: d}); !errors.Is(err, management.ErrDenied) {
		t.Fatalf("full save bypassed config.write: %v", err)
	}
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d})
	must(t, err)
	saved, err := f.c.Get(d.ID)
	must(t, err)
	if saved.Documentation == nil || *saved.Documentation != text {
		t.Fatal("full save ignored documentation-only change")
	}
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: saved})
	must(t, err)
	again, err := f.c.Get(d.ID)
	must(t, err)
	if saved.Revision != again.Revision {
		t.Fatal("unchanged full save updated revision")
	}
}
