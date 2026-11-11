package catalog_test

import (
	"context"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Optional acceptance against any existing network source, without source DDL.
func TestLiveDirectPasswordAndIndependentServer(t *testing.T) {
	raw := os.Getenv("DATASCOPE_DISCOVERY_CONNECTION")
	if raw == "" {
		t.Skip("未配置只读网络库验收连接")
	}
	var connection store.Connection
	if err := json.Unmarshal([]byte(raw), &connection); err != nil {
		t.Fatal(err)
	}
	password := os.Getenv(connection.PasswordEnv)
	if password == "" {
		t.Skip("未配置验收密码")
	}
	driver := os.Getenv("DATASCOPE_DISCOVERY_DRIVER")
	if driver == "" {
		driver = "mysql"
	}
	connection.PasswordEnv = ""
	connection.Port = 0
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "management.sqlite")
	m, err := management.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c := &catalog.Catalog{Management: m, Timeout: 10 * time.Second, AsOf: store.DefaultAsOf}
	d, err := c.Create(ctx, catalog.Database{Name: "直接输入密码验收", Driver: driver, Connection: &connection}, password)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.ID, "db_") || d.Connection.CredentialID != d.ID || d.Connection.PasswordEnv != "" || d.Connection.Port != store.DefaultPort(driver) {
		t.Fatal("identity, credential reference or default port incorrect")
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), password) {
		t.Fatal("API representation leaked password")
	}
	// Testing a draft must not persist it; a blank password resolves the old secret.
	if err := c.EditConnection(ctx, d.ID, d.Revision, d.Connection, "", false); err != nil {
		t.Fatal(err)
	}
	unchanged, err := c.Get(d.ID)
	if err != nil || unchanged.Revision != d.Revision {
		t.Fatal("probe changed revision", err)
	}
	if err := c.EditConnection(ctx, d.ID, d.Revision, d.Connection, password, true); err != nil {
		t.Fatal(err)
	}
	if err := c.EditConnection(ctx, d.ID, d.Revision, d.Connection, password, true); err == nil {
		t.Fatal("stale edit accepted")
	}
	d, err = c.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The edit API must preserve logical scope and never trust credential references.
	draft := *d.Connection
	draft.Database, draft.Schema, draft.ServiceName = "other", "other", "other"
	draft.AllowedSchemas = []string{"other"}
	draft.CredentialID, draft.PasswordEnv = "other-source", "DATASCOPE_DB_OTHER"
	if err := c.EditConnection(ctx, d.ID, d.Revision, &draft, "", true); err != nil {
		t.Fatal(err)
	}
	retainedSource, err := c.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retainedSource.Connection.Database != d.Connection.Database || retainedSource.Connection.Schema != d.Connection.Schema || retainedSource.Connection.CredentialID != d.ID {
		t.Fatal("edit changed logical target or credential reference")
	}
	d = retainedSource
	broken := *d.Connection
	broken.Host, broken.Port = "127.0.0.1", 1
	if err := c.EditConnection(ctx, d.ID, d.Revision, &broken, "not-the-saved-password", true); err == nil {
		t.Fatal("unreachable target accepted")
	}
	unchanged, err = c.Get(d.ID)
	if err != nil || unchanged.Revision != d.Revision || unchanged.Connection.Host != d.Connection.Host {
		t.Fatal("failed edit changed source", err)
	}
	retained, err := m.DatabasePassword(ctx, d.ID)
	if err != nil || retained != password {
		t.Fatal("failed edit changed password", err)
	}
	other, err := management.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	second := &catalog.Catalog{Management: other, Timeout: 10 * time.Second, AsOf: store.DefaultAsOf}
	saved, err := second.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	st, err := second.OpenStore(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	names, err := st.TableNames(ctx)
	if err != nil || len(names) == 0 {
		t.Fatal("independent server could not use saved password")
	}
	if _, err := st.Describe(ctx, names[0]); err != nil {
		t.Fatal(err)
	}
	t.Logf("direct password, auto ID, default port and independent server verified for %s (%d tables)", driver, len(names))
}
