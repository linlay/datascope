package platform_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"datascope-server/internal/application"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
)

func setItemMask(t *testing.T, f *fixture, column string, rule *store.Masking) {
	t.Helper()
	d, err := f.c.Get("default")
	must(t, err)
	for i := range d.Discovery.Tables {
		if d.Discovery.Tables[i].Name == "items" {
			for j := range d.Discovery.Tables[i].Columns {
				c := &d.Discovery.Tables[i].Columns[j]
				if c.Name == column {
					c.Masking = rule
				}
			}
		}
	}
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d})
	must(t, err)
}

func TestColumnMaskingPersistencePermissionsAndQueries(t *testing.T) {
	f := setup(t)
	_, err := f.c.Discover(f.ctx, "default")
	must(t, err)
	// A config editor cannot remove or introduce a field security rule.
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: "default", Permissions: []string{"config.read", "config.write", "query", "raw_query"}, BusinessUserID: 10}))
	d, err := f.c.Get("default")
	must(t, err)
	d.Discovery.Tables[0].Columns[2].Masking = &store.Masking{Type: "full"}
	if _, err = f.app.Call(f.readerContext(), "save_database", application.Input{DatabaseID: d.ID, Database: d}); err == nil {
		t.Fatal("config editor changed masking")
	}
	setItemMask(t, f, "name", &store.Masking{Type: "full"})
	setItemMask(t, f, "secret", &store.Masking{Type: "omit"})
	d, err = f.c.Get("default")
	must(t, err)
	oldRevision := d.Revision
	// A normal metadata edit must retain the rule.
	_, err = f.c.SaveAnnotation(f.ctx, "default", "items", "name", d.Revision, store.Annotation{DisplayName: "名称"})
	must(t, err)
	if err = f.c.Save(f.ctx, *d); err == nil {
		t.Fatal("stale masking edit accepted")
	}
	d, err = f.c.Discover(f.ctx, "default")
	must(t, err)
	if d.Discovery.Pending != nil {
		t.Fatal("managed masking was mistaken for a source change")
	}
	if d.Revision != oldRevision+1 {
		t.Fatal("unchanged scan mutated configuration")
	}
	raw, err := os.ReadFile(filepath.Join(f.dir, "databases", "default", "management", "tables", "items.yml"))
	must(t, err)
	if !strings.Contains(string(raw), "masking:") || strings.Contains(string(raw), "maskingKey") {
		t.Fatal("masking not stored on columns or secret leaked", string(raw))
	}
	svc, err := f.c.Open(f.ctx, d)
	must(t, err)
	defer svc.Store.Close()
	scoped, err := svc.QueryWithParameters(f.ctx, `SELECT name AS label,secret FROM items WHERE id=:arg_id`, 10, 10, map[string]any{"arg_id": float64(1)})
	must(t, err)
	if len(scoped.Columns) != 1 || scoped.Rows[0]["label"] != "******" {
		t.Fatal(scoped)
	}
	srv := api(t, f)
	client := login(t, srv.URL, "admin", "admin-test-password")
	for _, path := range []string{"/api/databases/default/query", "/api/databases/default/rewrite", "/api/databases/default/tables/items/rows"} {
		method := http.MethodPost
		var body any = map[string]any{"sql": "SELECT name AS label,secret FROM items", "user_id": 10}
		if strings.HasSuffix(path, "/rows") {
			method = http.MethodGet
			body = nil
		}
		status, data := request(t, client, method, srv.URL+path, body)
		if status != 200 || strings.Contains(string(data), `"one"`) || strings.Contains(string(data), `"s1"`) || !strings.Contains(string(data), "******") {
			t.Fatalf("unmasked %s: %d %s", path, status, data)
		}
	}
	// Incoming snapshots cannot overwrite source attributes or annotations.
	d, err = f.c.Get("default")
	must(t, err)
	d.Discovery.Tables[0].Columns[0].Type = "malicious-type"
	d.Discovery.Annotations = nil
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d})
	must(t, err)
	saved, err := f.c.Get("default")
	must(t, err)
	if saved.Discovery.Tables[0].Columns[0].Type == "malicious-type" || saved.Discovery.Annotations["items"].Columns["name"].DisplayName != "名称" {
		t.Fatal("untrusted snapshot overwrote metadata")
	}
}

func TestColumnHMACStableAndLostKeyFailsClosed(t *testing.T) {
	f := setup(t)
	_, err := f.c.Discover(f.ctx, "default")
	must(t, err)
	setItemMask(t, f, "name", &store.Masking{Type: "hmac"})
	query := func() string {
		d, e := f.c.Get("default")
		must(t, e)
		svc, e := f.c.Open(f.ctx, d)
		must(t, e)
		defer svc.Store.Close()
		out, e := svc.Store.RawQuery(f.ctx, "SELECT name FROM items WHERE id=1", 1)
		must(t, e)
		return out.Rows[0]["name"].(string)
	}
	first := query()
	second := query()
	if first != second || !strings.HasPrefix(first, "h1:") {
		t.Fatal(first, second)
	}
	reopened, err := management.Open(f.m.Path, f.a)
	must(t, err)
	defer reopened.Close()
	key, err := reopened.MaskingKey("default", false)
	must(t, err)
	original, err := f.m.MaskingKey("default", false)
	must(t, err)
	if string(key) != string(original) {
		t.Fatal("key changed across store instances")
	}
	keyPath := filepath.Join(f.dir, "databases", "default", "credentials.key")
	must(t, os.Rename(keyPath, keyPath+".backup"))
	d, err := f.c.Get("default")
	must(t, err)
	if svc, err := f.c.Open(f.ctx, d); err == nil {
		svc.Store.Close()
		t.Fatal("missing key allowed query")
	}
	if err = f.c.Save(f.ctx, *d); err == nil {
		t.Fatal("missing HMAC key silently recreated on save")
	}
	if _, err = os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("lost key was regenerated", err)
	}
}

func TestColumnMaskingDoesNotChangeActionValues(t *testing.T) {
	f, business := setupAction(t, false)
	_, err := f.c.Discover(f.ctx, "default")
	must(t, err)
	setItemMask(t, f, "name", &store.Masking{Type: "full"})
	d, err := f.c.Get("default")
	must(t, err)
	d.Actions[0].Script = "function run(args,ctx){return my.one`SELECT name FROM items WHERE id=${args.id}`}"
	must(t, f.c.Save(f.ctx, *d))
	result, err := f.app.InvokeAction(business, "default", "rename_item", "masking-query-only", map[string]any{"id": 1, "name": "unused"})
	must(t, err)
	raw, _ := json.Marshal(result.Result)
	if !strings.Contains(string(raw), `"one"`) {
		t.Fatal("query masking changed action internals", string(raw))
	}
}
