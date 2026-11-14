package platform_test

import (
	"context"
	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/view"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
	"testing"
)

func TestCapabilitySwitchesAtomicAndScoped(t *testing.T) {
	f := setup(t)
	set := func(capability string, enabled bool) application.DatabaseView {
		t.Helper()
		d, err := f.c.Get("default")
		must(t, err)
		in := application.Input{DatabaseID: d.ID, Revision: d.Revision, Capability: capability, Enabled: &enabled}
		if _, err = f.app.Call(f.readerContext(), "configure_capability", in); !errors.Is(err, management.ErrDenied) {
			t.Fatalf("reader changed %s: %v", capability, err)
		}
		result, err := f.app.Call(f.ctx, "configure_capability", in)
		must(t, err)
		if _, err = f.app.Call(f.ctx, "configure_capability", in); err == nil {
			t.Fatal("stale revision accepted")
		}
		return result.(application.DatabaseView)
	}
	for _, enabled := range []bool{true, false, true} {
		saved := set("actions", enabled)
		if saved.WriteEnabled != enabled || saved.Capabilities["actions"] != enabled {
			t.Fatal("action status disagrees")
		}
		for _, name := range catalog.ActionTools {
			if slices.Contains(saved.Tools, name) != enabled {
				t.Fatal("partial action tool group", saved.Tools)
			}
		}
		if !slices.Contains(saved.Tools, "query") {
			t.Fatal("query tools changed")
		}
	}
	saved := set("views", false)
	if saved.ViewsAvailable() || saved.Capabilities["views"] {
		t.Fatal("views stayed enabled")
	}
	saved = set("knowledge", false)
	if saved.KnowledgeAvailable() {
		t.Fatal("knowledge stayed enabled")
	}
	saved = set("knowledge", true)
	if !saved.KnowledgeAvailable() {
		t.Fatal("knowledge stayed disabled")
	}
	// Knowledge editors may toggle their group's exposure without broad MCP administration.
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: "default", BusinessUserID: 10, Permissions: []string{"knowledge.write"}}))
	enabled := false
	_, err := f.app.Call(f.readerContext(), "configure_capability", application.Input{DatabaseID: "default", Capability: "knowledge", Enabled: &enabled, Revision: saved.Revision})
	must(t, err)
	// Redacted database responses still carry the actual group status.
	current, err := f.c.Get("default")
	must(t, err)
	status := f.app.DatabaseView(f.readerContext(), *current)
	if !status.Capabilities["actions"] || status.Capabilities["knowledge"] {
		t.Fatal("redaction changed capability status", status.Capabilities)
	}
}

func TestViewsMasterSwitchEnforcesLiveResources(t *testing.T) {
	env := setupRealms(t)
	f := env.f
	ctx := context.Background()
	d, err := f.c.Get("default")
	must(t, err)
	d.Views = []view.Definition{{ID: "sample", Name: "Sample", Type: "view", HTML: "<!doctype html><p>View</p>", Published: true}}
	d.QueryViewID = "sample"
	documentation := "# Database reference"
	d.Documentation = &documentation
	must(t, f.c.Save(f.ctx, *d))
	tokens, _, _ := employeeTokens(t, env, "default", "employee-default-password")
	session := connectMCP(t, env.resource+"/default", tokens["access"])
	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: view.URI(d.ID, "sample")})
	must(t, err)
	d, err = f.c.Get(d.ID)
	must(t, err)
	enabled := false
	_, err = f.app.Call(f.ctx, "configure_capability", application.Input{DatabaseID: d.ID, Revision: d.Revision, Capability: "views", Enabled: &enabled})
	must(t, err)
	listed, err := session.ListResources(ctx, nil)
	must(t, err)
	if len(listed.Resources) != 1 || listed.Resources[0].MIMEType != "text/markdown" {
		t.Fatal("disabled views leaked or documentation hidden", listed)
	}
	if _, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: view.URI(d.ID, "sample")}); err == nil {
		t.Fatal("disabled view readable on old session")
	}
	queried, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "query", Arguments: map[string]any{"sql": "SELECT id FROM items"}})
	must(t, err)
	if queried.IsError || queried.Meta["datascope/viewUri"] != nil {
		t.Fatal("disabled view broke query or remained recommended", queried)
	}
	queried, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "query", Arguments: map[string]any{"sql": "SELECT id FROM items", "view_id": "sample"}})
	if err == nil && !queried.IsError {
		t.Fatal("explicit disabled view accepted")
	}
	d, err = f.c.Get(d.ID)
	must(t, err)
	enabled = true
	_, err = f.app.Call(f.ctx, "configure_capability", application.Input{DatabaseID: d.ID, Revision: d.Revision, Capability: "views", Enabled: &enabled})
	must(t, err)
	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: view.URI(d.ID, "sample")})
	must(t, err)
}
