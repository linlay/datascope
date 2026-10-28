package platform_test

import (
	"database/sql"
	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"errors"
	"testing"
)

func TestSchemaChangesRequireExplicitReviewAndRejectStaleCandidates(t *testing.T) {
	f := setup(t)
	w, err := sql.Open("sqlite", f.st.Path())
	must(t, err)
	defer w.Close()
	_, err = w.Exec(`CREATE TABLE schema_comments(table_name TEXT,column_name TEXT,comment_zh TEXT); INSERT INTO schema_comments VALUES('items','id','原编号');`)
	must(t, err)
	out, err := f.app.Call(f.ctx, "discover_database", application.Input{DatabaseID: "default"})
	must(t, err)
	d := out.(application.DatabaseView)
	manual := "人工业务编号"
	out, err = f.app.Call(f.ctx, "save_annotation", application.Input{DatabaseID: d.ID, Table: "items", Column: "id", Revision: d.Revision, Annotation: &store.Annotation{DisplayName: "编号", Comment: &manual}})
	must(t, err)
	d = out.(application.DatabaseView)
	_, err = w.Exec(`CREATE TABLE later(id INTEGER); UPDATE schema_comments SET comment_zh='数据库新编号' WHERE table_name='items' AND column_name='id';`)
	must(t, err)
	out, err = f.app.Call(f.ctx, "discover_database", application.Input{DatabaseID: d.ID})
	must(t, err)
	next := out.(application.DatabaseView)
	if next.Revision != d.Revision || next.Discovery.Version != d.Discovery.Version || next.Discovery.Pending == nil {
		t.Fatal("detection changed approved configuration")
	}
	for _, p := range next.Config.Tables {
		if p.Table == "later" {
			t.Fatal("new table registered before approval")
		}
	}
	first := next.Discovery.Pending.ID
	out, err = f.app.Call(f.ctx, "discover_database", application.Input{DatabaseID: d.ID})
	must(t, err)
	next = out.(application.DatabaseView)
	if next.Discovery.Pending.ID != first {
		t.Fatal("unchanged proposal invalidated an open comparison")
	}
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: d.ID, Permissions: []string{"config.read", "config.write"}}))
	_, err = f.app.Call(f.readerContext(), "apply_discovery", application.Input{DatabaseID: d.ID, ID: first, Revision: next.Revision})
	if !errors.Is(err, management.ErrDenied) {
		t.Fatal("schema approval bypassed policy.write")
	}
	_, err = w.Exec(`ALTER TABLE later ADD COLUMN note TEXT;`)
	must(t, err)
	_, err = f.app.Call(f.ctx, "apply_discovery", application.Input{DatabaseID: d.ID, ID: first, Revision: next.Revision})
	if err == nil {
		t.Fatal("a source that changed after preview was applied")
	}
	unchanged, err := f.c.Get(d.ID)
	must(t, err)
	if unchanged.Discovery.Version != d.Discovery.Version {
		t.Fatal("rejected approval changed the source snapshot")
	}
	out, err = f.app.Call(f.ctx, "discover_database", application.Input{DatabaseID: d.ID})
	must(t, err)
	next = out.(application.DatabaseView)
	if next.Discovery.Pending.ID == first {
		t.Fatal("changed source did not invalidate old comparison")
	}
	_, err = f.app.Call(f.ctx, "apply_discovery", application.Input{DatabaseID: d.ID, ID: first, Revision: next.Revision})
	if err == nil {
		t.Fatal("old proposal accepted")
	}
	out, err = f.app.Call(f.ctx, "apply_discovery", application.Input{DatabaseID: d.ID, ID: next.Discovery.Pending.ID, Revision: next.Revision, Resolutions: []catalog.AnnotationResolution{{Table: "items", Column: "id", FollowSource: true}}})
	must(t, err)
	approved := out.(application.DatabaseView)
	if approved.Discovery.Version != d.Discovery.Version+1 || approved.Discovery.Pending != nil {
		t.Fatal("approval did not publish new snapshot and clear pending state")
	}
	if approved.Discovery.Annotations["items"].Columns["id"].Comment != nil {
		t.Fatal("chosen source comment was not adopted")
	}
	if approved.Discovery.Annotations["items"].Columns["id"].DisplayName != "编号" {
		t.Fatal("comment resolution discarded display mapping")
	}
	found := false
	for _, p := range approved.Config.Tables {
		if p.Table == "later" {
			found = true
			if p.Enabled == nil || *p.Enabled {
				t.Fatal("approved new table was automatically opened")
			}
		}
	}
	if !found {
		t.Fatal("approved new table not registered")
	}
}
