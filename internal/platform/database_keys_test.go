package platform_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
)

func TestDatabaseOwnedAPIKeys(t *testing.T) {
	f, server := setupWorkbenchProxy(t, "ACCOUNT")
	d, err := f.c.Get("default")
	must(t, err)
	d.Authentication = &catalog.Authentication{Type: "local", Source: "DATABASE", Access: &management.AuthPolicy{ID: "key", Name: "Key", Mode: "TOKEN", ProviderID: "key", BusinessUserID: 10, Permissions: []string{"query"}}, Provider: &management.AuthProvider{ID: "key", Name: "Key", Type: "STATIC_TOKEN", Enabled: true}}
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d})
	must(t, err)
	rotate := func() string {
		t.Helper()
		out, err := f.app.Call(f.ctx, "rotate_database_api_key", application.Input{DatabaseID: "default"})
		must(t, err)
		return out.(map[string]string)["api_key"]
	}
	key := rotate()
	if !strings.HasPrefix(key, "ds_") || len(key) < 43 {
		t.Fatal("weak or missing generated key")
	}
	hashPath := filepath.Join(f.dir, "databases", "default", "auth", "api-key.sha256")
	raw, err := os.ReadFile(hashPath)
	must(t, err)
	if len(strings.TrimSpace(string(raw))) != 64 || strings.Contains(string(raw), key) {
		t.Fatal("API key not stored as digest")
	}
	info, err := os.Stat(hashPath)
	must(t, err)
	if info.Mode().Perm() != 0600 {
		t.Fatal("API key digest permissions")
	}
	source, err := f.m.Source(f.ctx, "default")
	must(t, err)
	must(t, filepath.WalkDir(source.Path, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if strings.Contains(string(raw), key) {
				t.Fatal("plaintext key leaked into YAML")
			}
		}
		return nil
	}))
	p, err := f.m.ResolveAuth(f.ctx, "DATABASE", "default")
	must(t, err)
	if _, err = p.VerifyBearer(context.Background(), "default", key); err != nil {
		t.Fatal("generated key rejected", err)
	}
	if _, err = p.VerifyBearer(context.Background(), "default", "wrong-api-key-value"); err == nil {
		t.Fatal("wrong key accepted")
	}
	session := connectMCP(t, strings.TrimSuffix(f.app.MCPBase, "/mcp")+"/mcp/default", key)
	out, err := callMCP(t, session, "query", map[string]any{"sql": "SELECT id FROM items ORDER BY id"})
	must(t, err)
	body, _ := json.Marshal(out)
	if out.IsError || !strings.Contains(string(body), `"row_count":2`) {
		t.Fatal("key identity not enforced", string(body))
	}
	session.Close()
	// The same provider definition in a different database does not share keys.
	d, err = f.c.Get("default")
	must(t, err)
	other := *d
	other.ID = "other"
	other.Name = "Other"
	other.MCPPath = "other"
	_, err = f.c.Create(f.ctx, other, "")
	must(t, err)
	otherPolicy, err := f.m.ResolveAuth(f.ctx, "DATABASE", "other")
	must(t, err)
	if _, err = otherPolicy.VerifyBearer(f.ctx, "other", key); err == nil {
		t.Fatal("API key crossed database boundary")
	}
	if _, err = f.app.Call(f.readerContext(), "rotate_database_api_key", application.Input{DatabaseID: "default"}); err == nil {
		t.Fatal("reader generated API key")
	}
	second := rotate()
	if second == key {
		t.Fatal("rotation reused key")
	}
	if _, err = p.VerifyBearer(f.ctx, "default", key); err == nil {
		t.Fatal("old key survived rotation")
	}
	if _, err = p.VerifyBearer(f.ctx, "default", second); err != nil {
		t.Fatal("rotated key rejected")
	}
	_, err = f.app.Call(f.ctx, "revoke_database_api_key", application.Input{DatabaseID: "default"})
	must(t, err)
	if _, err = p.VerifyBearer(f.ctx, "default", second); err == nil {
		t.Fatal("revoked key accepted")
	}
	if _, err = os.Stat(hashPath); !os.IsNotExist(err) {
		t.Fatal("revoked digest remains")
	}
	// The management API must not expose a credential through a read endpoint.
	code, _ := request(t, http.DefaultClient, "POST", server.URL+"/api/management/call", map[string]any{"action": "rotate_database_api_key", "arguments": map[string]string{"database_id": "default"}})
	if code != 401 {
		t.Fatalf("anonymous key generation: %d", code)
	}
}

func TestDatabaseSigningKeysAreIsolated(t *testing.T) {
	f, server := setupWorkbenchProxy(t, "ACCOUNT")
	d, err := f.c.Get("default")
	must(t, err)
	other := *d
	other.ID = "other"
	other.Name = "Other"
	other.MCPPath = "other"
	_, err = f.c.Create(f.ctx, other, "")
	must(t, err)
	otherSaved, err := f.c.Get("other")
	must(t, err)
	otherSaved.MCPEnabled = true
	must(t, f.c.Save(f.ctx, *otherSaved))
	get := func(id string) string {
		t.Helper()
		response, err := http.Get(server.URL + "/auth/databases/" + id + "/oauth/jwks.json")
		must(t, err)
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("JWKS unavailable", id)
		}
		var keys struct {
			Keys []struct {
				N string `json:"n"`
			} `json:"keys"`
		}
		must(t, json.NewDecoder(response.Body).Decode(&keys))
		return keys.Keys[0].N
	}
	first := get("default")
	second := get("other")
	if first == second {
		t.Fatal("databases share signing key")
	}
	if get("default") != first {
		t.Fatal("database key was regenerated")
	}
	for _, id := range []string{"default", "other"} {
		dir := filepath.Join(f.dir, "databases", id, "auth")
		for _, file := range []string{"signing.pem", "public.pem"} {
			info, err := os.Stat(filepath.Join(dir, file))
			must(t, err)
			if info.Mode().Perm() != 0600 {
				t.Fatal("key permissions", file)
			}
		}
		raw, err := os.ReadFile(filepath.Join(dir, "public.pem"))
		must(t, err)
		if !strings.Contains(string(raw), "BEGIN PUBLIC KEY") || strings.Contains(string(raw), "PRIVATE") {
			t.Fatal("invalid public key file")
		}
	}
}
