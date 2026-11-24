package platform_test

import (
	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseTimeoutSettingsAuthorizationAndPersistence(t *testing.T) {
	f := setup(t)
	d, err := f.c.Get("default")
	must(t, err)
	query, connect := 90, 5
	input := application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: d.Revision, QueryTimeoutSeconds: &query, ConnectTimeoutSeconds: &connect}}
	_, err = f.app.Call(f.readerContext(), "update_database", input)
	if !errors.Is(err, management.ErrDenied) {
		t.Fatalf("reader changed timeouts: %v", err)
	}
	_, err = f.app.Call(f.ctx, "update_database", input)
	must(t, err)
	if _, err = f.app.Call(f.ctx, "update_database", input); err == nil {
		t.Fatal("stale revision accepted")
	}
	saved, err := f.c.Get(d.ID)
	must(t, err)
	view := f.app.DatabaseView(f.ctx, *saved)
	if view.EffectiveQueryTimeoutSeconds != 90 || view.EffectiveConnectTimeoutSeconds != 5 {
		t.Fatal("effective limits incorrect")
	}
	source, err := f.m.Source(f.ctx, d.ID)
	must(t, err)
	raw, err := os.ReadFile(filepath.Join(source.Path, "database.yml"))
	must(t, err)
	if !strings.Contains(string(raw), "query_timeout_seconds") && !strings.Contains(string(raw), "queryTimeoutSeconds") {
		t.Fatal("timeout absent from YAML")
	}
	// Full saves must enforce config.write too, including when restoring inheritance.
	clone := *saved
	zero := 0
	clone.QueryTimeoutSeconds = &zero
	_, err = f.app.Call(f.readerContext(), "save_database", application.Input{DatabaseID: d.ID, Database: &clone})
	if !errors.Is(err, management.ErrDenied) {
		t.Fatalf("full save bypass: %v", err)
	}
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: &clone})
	must(t, err)
	saved, err = f.c.Get(d.ID)
	must(t, err)
	// Name-only patches preserve overrides.
	_, err = f.app.Call(f.ctx, "update_database", application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: saved.Revision, Name: "Renamed"}})
	must(t, err)
	saved, err = f.c.Get(d.ID)
	must(t, err)
	if saved.QueryTimeoutSeconds == nil || *saved.QueryTimeoutSeconds != 0 || saved.ConnectTimeoutSeconds == nil || *saved.ConnectTimeoutSeconds != 5 {
		t.Fatal("partial update reset timeouts")
	}
	bad := 121
	_, err = f.app.Call(f.ctx, "update_database", application.Input{DatabaseID: d.ID, Database: &catalog.Database{ID: d.ID, Revision: saved.Revision, QueryTimeoutSeconds: &bad}})
	if err == nil {
		t.Fatal("invalid limit accepted")
	}
}
