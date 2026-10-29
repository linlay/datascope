package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditFiltersCountsAndPaginationUseSameScope(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second)
	for _, event := range []Event{
		{ID: "a", Metadata: Metadata{DatabaseID: "first", ActorID: 7, Source: "web", RequestID: "request-a", Tool: "query"}, Kind: "sql.execute", Status: "failed", StartedAt: at.Add(-time.Hour).Format(time.RFC3339Nano), OriginalSQL: "SELECT 100% FROM records", Error: "blocked"},
		{ID: "b", Metadata: Metadata{DatabaseID: "first", ActorID: 7, Source: "web", RequestID: "request-b", Tool: "query"}, Kind: "sql.execute", Status: "success", StartedAt: at.Format(time.RFC3339Nano)},
		{ID: "c", Metadata: Metadata{DatabaseID: "second", ActorID: 8, Source: "database-mcp"}, Kind: "sql.execute", Status: "success", StartedAt: at.Format(time.RFC3339Nano)},
		{ID: "d", Metadata: Metadata{ActorID: 9, Source: "web"}, Kind: "user.create", Status: "success", StartedAt: at.Format(time.RFC3339Nano)},
		{ID: "e", Metadata: Metadata{DatabaseID: "first", ActorID: 0, Source: "discovery"}, Kind: "database.discover", Status: "running", StartedAt: at.Add(-10 * time.Minute).Format(time.RFC3339Nano)},
		{ID: "f", Metadata: Metadata{DatabaseID: "first", ActorID: 0, Source: "discovery"}, Kind: "database.discover", Status: "running", StartedAt: at.Format(time.RFC3339Nano)},
	} {
		if err = s.Put(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	actor := int64(7)
	page, err := s.Search(ctx, Filter{DatabaseID: "first", ActorID: &actor, Kind: "sql.execute", Source: "web", Tool: "query", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != "b" {
		t.Fatalf("filtered count or first page incorrect: %+v", page)
	}
	page, err = s.Search(ctx, Filter{DatabaseID: "first", ActorID: &actor, Kind: "sql.execute", Source: "web", Tool: "query", Limit: 1, Offset: 1})
	if err != nil || page.Total != 2 || page.Items[0].ID != "a" {
		t.Fatal("pagination lost filter scope", err)
	}
	for _, test := range []struct {
		filter Filter
		id     string
	}{
		{Filter{DatabaseID: "first", Status: "failed", Query: "100%"}, "a"},
		{Filter{DatabaseID: "first", RequestID: "request-a"}, "a"},
		{Filter{DatabaseID: "first", Status: "unknown"}, "e"},
		{Filter{DatabaseID: "first", Status: "running"}, "f"},
		{Filter{PlatformOnly: true}, "d"},
		{Filter{DatabaseID: "first", ActorID: &actor, StartedFrom: at.Format(time.RFC3339), StartedTo: at.Format(time.RFC3339)}, "b"},
	} {
		page, err := s.Search(ctx, test.filter)
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != test.id {
			t.Fatalf("filter %+v: %+v %v", test.filter, page, err)
		}
	}
	page, err = s.Search(ctx, Filter{DatabaseID: "first", Query: "' OR 1=1 --"})
	if err != nil || page.Total != 0 {
		t.Fatal("filter text was interpreted as SQL", err)
	}
	for _, filter := range []Filter{{DatabaseID: "first", PlatformOnly: true}, {Status: "other"}, {StartedFrom: "yesterday"}, {StartedFrom: at.Format(time.RFC3339), StartedTo: at.Add(-time.Hour).Format(time.RFC3339)}} {
		if _, err = s.Search(ctx, filter); err == nil {
			t.Fatal("invalid filter accepted", filter)
		}
	}
	// Date filters accept timezone offsets without relying on lexical timestamp order.
	start := at.In(time.FixedZone("Shanghai", 8*3600)).Format(time.RFC3339)
	page, err = s.Search(ctx, Filter{DatabaseID: "second", StartedFrom: start})
	if err != nil || page.Total != 1 {
		t.Fatal("timezone-aware filter failed", err)
	}
}
