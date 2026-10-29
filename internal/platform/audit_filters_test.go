package platform_test

import (
	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/management"
	"errors"
	"testing"
)

func TestAuditPageFiltersRespectDatabasePermissions(t *testing.T) {
	f := setup(t)
	for _, event := range []audit.Event{
		{ID: "visible-audit", Metadata: audit.Metadata{DatabaseID: "default", ActorID: f.admin.ID, Source: "web", Tool: "review", RequestID: "audit-filter-check"}, Kind: "test.audit.filter", Status: "success"},
		{ID: "private-audit", Metadata: audit.Metadata{DatabaseID: "private", ActorID: f.reader.ID, Source: "management-mcp", Tool: "review"}, Kind: "test.audit.filter", Status: "failed"},
		{ID: "platform-audit", Metadata: audit.Metadata{ActorID: 0, Source: "discovery"}, Kind: "test.audit.filter", Status: "success"},
	} {
		must(t, f.a.Put(f.ctx, event))
	}
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: "default", Permissions: []string{"audit.read"}}))
	out, err := f.app.Call(f.readerContext(), "get_audit_page", application.Input{DatabaseID: "default", Kind: "test.audit.filter", Source: "web", Status: "success", RequestID: "audit-filter-check", Limit: 20})
	must(t, err)
	page := out.(*audit.Page)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "visible-audit" {
		t.Fatal("database-scoped filters failed")
	}
	for _, source := range page.Sources {
		if source == "management-mcp" {
			t.Fatal("filter choices leaked another database's logs")
		}
	}
	for _, in := range []application.Input{{}, {DatabaseID: "private"}, {DatabaseID: "default", PlatformOnly: true}} {
		_, err := f.app.Call(f.readerContext(), "get_audit_page", in)
		if !errors.Is(err, management.ErrDenied) {
			t.Fatalf("audit scope bypassed: %+v %v", in, err)
		}
	}
	out, err = f.app.Call(f.ctx, "get_audit_page", application.Input{PlatformOnly: true, Kind: "test.audit.filter"})
	must(t, err)
	if page := out.(*audit.Page); page.Total != 1 || page.Items[0].ID != "platform-audit" {
		t.Fatal("platform-only filter failed")
	}
	out, err = f.app.Call(f.ctx, "search_audit_events", application.Input{DatabaseID: "default", Kind: "test.audit.filter"})
	must(t, err)
	if rows, ok := out.([]audit.Event); !ok || len(rows) != 1 {
		t.Fatal("legacy audit array response changed")
	}
}
