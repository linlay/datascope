package catalog_test

import (
	"context"
	"database/sql"
	"datascope-server/internal/catalog"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestDiscoveryReconcilesSchemaAndPreservesAnnotations(t *testing.T) {
	c, path := setup(t)
	ctx := context.Background()
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.Exec(`CREATE TABLE schema_comments(table_name TEXT,column_name TEXT,comment_zh TEXT); INSERT INTO schema_comments VALUES('records','','源表说明'),('records','id','原编号');`); err != nil {
		t.Fatal(err)
	}
	if err = c.Add(ctx, catalog.Database{ID: "discovery", Name: "侦查", Driver: "sqlite", Path: path}); err != nil {
		t.Fatal(err)
	}
	d := mustGet(t, c, "discovery")
	if d.Discovery.Version != 1 || len(d.Discovery.Tables) != 3 {
		t.Fatalf("missing snapshot: %+v", d.Discovery)
	}
	for _, p := range d.Config.Tables {
		if p.Enabled == nil || *p.Enabled || p.Columns == nil {
			t.Fatalf("unsafe initial policy: %+v", p)
		}
	}
	manual := "人工口径"
	d, err = c.SaveAnnotation(ctx, d.ID, "records", "id", d.Revision, store.Annotation{DisplayName: "记录编号", Comment: &manual})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.SaveAnnotation(ctx, d.ID, "records", "id", d.Revision-1, store.Annotation{}); err == nil {
		t.Fatal("stale annotation accepted")
	}
	for _, p := range d.Config.Tables {
		if p.Table == "records" {
			yes := true
			p.Enabled = &yes
			p.Columns = []string{"id", "owner", "secret"}
		}
	}
	if err = c.Save(ctx, *d); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Exec(`UPDATE schema_comments SET comment_zh='新编号说明' WHERE table_name='records' AND column_name='id'; ALTER TABLE records DROP COLUMN secret; ALTER TABLE records RENAME COLUMN owner TO owner_id; ALTER TABLE records ADD COLUMN new_secret TEXT; DROP TABLE other; CREATE TABLE fresh(id INTEGER);`); err != nil {
		t.Fatal(err)
	}
	// Metadata access survives drift before a policy can be reconciled.
	st, err := c.OpenStore(ctx, mustGet(t, c, d.ID))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := st.Describe(ctx, "records")
	st.Close()
	if err != nil || schema.Columns[0].Comment != manual {
		t.Fatalf("inspection blocked or annotation lost: %+v %v", schema, err)
	}
	d, err = c.Discover(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Discovery.Version != 1 || d.Discovery.Pending == nil {
		t.Fatal("scan must propose changes without applying them")
	}
	pendingID := d.Discovery.Pending.ID
	if slices.ContainsFunc(d.Config.Tables, func(p *rewrite.Policy) bool { return p.Table == "fresh" }) {
		t.Fatal("scan changed access before approval")
	}
	d, err = c.ApplyDiscovery(ctx, d.ID, pendingID, d.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Discovery.Version != 2 || d.Discovery.Pending != nil {
		t.Fatal("reviewed changes were not applied")
	}

	conflict := false
	for _, ch := range d.Discovery.Reports[1].Changes {
		if ch.Table == "records" && ch.Column == "id" && ch.Property == "comment" {
			conflict = ch.Conflict && ch.Before == "原编号" && ch.After == "新编号说明" && ch.Manual != nil && *ch.Manual == manual
		}
	}
	if !conflict {
		t.Fatal("missing three-way comment conflict")
	}
	for _, p := range d.Config.Tables {
		switch p.Table {
		case "other":
			t.Fatal("dropped table policy retained")
		case "fresh":
			if p.Enabled == nil || *p.Enabled {
				t.Fatal("new table exposed")
			}
		case "records":
			if !slices.Equal(p.Columns, []string{"id"}) {
				t.Fatalf("access not reconciled: %v", p.Columns)
			}
		}
	}
	svc, err := c.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Query(ctx, "SELECT * FROM records", 1, 5)
	if err != nil || !slices.Equal(out.Columns, []string{"id"}) {
		t.Fatalf("query access: %+v %v", out, err)
	}
	if _, err = svc.Query(ctx, "SELECT * FROM fresh", 1, 5); !rewrite.IsDenied(err) {
		t.Fatalf("new table exposed: %v", err)
	}
	schema, err = svc.Store.Describe(ctx, "records")
	svc.Store.Close()
	if err != nil || schema.Columns[0].DisplayName != "记录编号" || schema.Columns[0].Comment != manual {
		t.Fatal("effective mapping not applied")
	}
	d, err = c.Discover(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Discovery.Version != 2 || len(d.Discovery.Reports) != 2 {
		t.Fatal("unchanged scan created report")
	}
	// Explicitly clearing and following source are distinct operations.
	empty := ""
	d, err = c.SaveAnnotation(ctx, d.ID, "records", "id", d.Revision, store.Annotation{Comment: &empty})
	if err != nil {
		t.Fatal(err)
	}
	st, err = c.OpenStore(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	schema, err = st.Describe(ctx, "records")
	st.Close()
	if err != nil || schema.Columns[0].Comment != "" {
		t.Fatal("explicit empty override lost")
	}
	d, err = c.SaveAnnotation(ctx, d.ID, "records", "id", d.Revision, store.Annotation{DisplayName: "编号"})
	if err != nil {
		t.Fatal(err)
	}
	st, err = c.OpenStore(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	schema, err = st.Describe(ctx, "records")
	st.Close()
	if err != nil || schema.Columns[0].Comment != "新编号说明" {
		t.Fatal("follow source failed")
	}
	// A failed scan cannot publish an empty snapshot or delete policies.
	before := mustGet(t, c, d.ID)
	writer.Close()
	if err = os.Rename(path, path+".offline"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Discover(ctx, d.ID); err == nil {
		t.Fatal("offline scan succeeded")
	}
	after := mustGet(t, c, d.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed scan changed last good state")
	}
}

func TestAutomaticDiscoveryWithoutBrowser(t *testing.T) {
	c, path := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Add(ctx, catalog.Database{ID: "automatic", Name: "Automatic", Driver: "sqlite", Path: path}); err != nil {
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err = w.Exec(`CREATE TABLE added_later(id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); c.RunDiscovery(ctx, 20*time.Millisecond) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d := mustGet(t, c, "automatic")
		if d.Discovery.Pending != nil {
			if d.Discovery.Version != 1 {
				t.Fatal("automatic scan applied changes without approval")
			}
			cancel()
			<-done
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("automatic scanner did not find added table")
}

// Opt-in acceptance against an arbitrary existing database. It creates no
// source objects and uses a temporary local catalog, never fixture DDL.
func TestLiveDiscovery(t *testing.T) {
	raw := os.Getenv("DATASCOPE_DISCOVERY_CONNECTION")
	if raw == "" {
		t.Skip("未配置只读侦查验收连接")
	}
	var conn store.Connection
	if err := json.Unmarshal([]byte(raw), &conn); err != nil {
		t.Fatal("invalid connection JSON")
	}
	driver := os.Getenv("DATASCOPE_DISCOVERY_DRIVER")
	if driver == "" {
		driver = "mysql"
	}
	c := &catalog.Catalog{Path: filepath.Join(t.TempDir(), "catalog"), Timeout: 10 * time.Second, AsOf: store.DefaultAsOf}
	ctx := context.Background()
	if err := c.Add(ctx, catalog.Database{ID: "live", Name: "Live", Driver: driver, Connection: &conn}); err != nil {
		t.Fatal(err)
	}
	d := mustGet(t, c, "live")
	fields, comments := 0, 0
	for _, table := range d.Discovery.Tables {
		fields += len(table.Columns)
		if table.Comment != "" {
			comments++
		}
	}
	if len(d.Discovery.Tables) == 0 {
		t.Fatal("database contains no visible tables")
	}
	d, err := c.Discover(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Discovery.Version != 1 {
		t.Fatal("unchanged live schema generated changes")
	}
	t.Logf("read-only discovery passed: tables=%d columns=%d tables_with_comments=%d", len(d.Discovery.Tables), fields, comments)
}
