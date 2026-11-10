package platform_test

import (
	"bytes"
	"database/sql"
	"datascope-server/internal/adminapi"
	"datascope-server/internal/application"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSourceUpload(t *testing.T) {
	f := setup(t)
	server := httptest.NewServer(adminapi.PlatformHandler(f.app, adminapi.ManagedHandler(f.c, &adminapi.Inspector{})))
	defer server.Close()
	admin := login(t, server.URL, "admin", "admin-test-password")
	reader := login(t, server.URL, "reader", "reader-test-password")
	upload := func(client *http.Client, format string, data io.Reader, status int) application.DatabaseView {
		response, err := client.Post(server.URL+"/api/databases/import?name=Uploaded&format="+format, "application/octet-stream", data)
		must(t, err)
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		must(t, err)
		if response.StatusCode != status {
			t.Fatalf("upload: %d %s", response.StatusCode, body)
		}
		var d application.DatabaseView
		if status == 200 {
			must(t, json.Unmarshal(body, &d))
			if d.Discovery == nil || len(d.Discovery.Tables) == 0 {
				t.Fatal("missing discovered tables")
			}
		}
		return d
	}
	upload(reader, "csv", strings.NewReader("id\n123"), 403)
	d := upload(admin, "csv", strings.NewReader("\ufeff\"编号\",note\r\n001,\"first,\nsecond\""), 200)
	db, err := sql.Open("sqlite", d.Path)
	must(t, err)
	var id, note string
	must(t, db.QueryRow("SELECT 编号,note FROM data").Scan(&id, &note))
	must(t, db.Close())
	if id != "001" || note != "first,\nsecond" {
		t.Fatalf("changed CSV values: %q %q", id, note)
	}
	original, err := os.ReadFile(f.st.Path())
	must(t, err)
	d = upload(admin, "sqlite", bytes.NewReader(original), 200)
	db, err = sql.Open("sqlite", d.Path)
	must(t, err)
	var count int
	must(t, db.QueryRow("SELECT count(*) FROM items").Scan(&count))
	must(t, db.Close())
	if count != 3 || d.Path == f.st.Path() {
		t.Fatal("expected independent copy with existing rows")
	}
	for _, bad := range []string{"a,A\n", "a,b\n1,2\n3", "a\n\xff"} {
		upload(admin, "csv", strings.NewReader(bad), 400)
	}
	upload(admin, "sqlite", strings.NewReader("SQLite format 3\x00bad"), 400)
	files, err := filepath.Glob(filepath.Join(f.c.LocalSQLiteRoot(), "upload-*"))
	must(t, err)
	if len(files) != 2 {
		t.Fatalf("orphaned files: %v", files)
	}
}
