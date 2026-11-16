package configbundle

import (
	"context"
	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeclarativeExamplesImportAndPreserveExistingState(t *testing.T) {
	for _, name := range []string{"shop", "securities", "academic", "channel-operations"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "business.sqlite3")
			args := []string{"../../examples/create_sqlite.py", "--db", path, "../../examples/" + name + "/schema.sqlite.sql"}
			if name == "securities" {
				args = append(args, "../../examples/securities/seed.sql")
			}
			if name == "academic" {
				args = []string{"../../examples/academic/generate.py", "--db", path}
			}
			if name == "channel-operations" {
				args = []string{"../../examples/channel-operations/generate.py", "--db", path}
			}
			if out, err := exec.Command("python3", args...).CombinedOutput(); err != nil {
				t.Fatalf("SQL creation: %s %v", out, err)
			}
			b, err := Load("../../examples/" + name + "/bundle.yml")
			if err != nil {
				t.Fatal(err)
			}
			b.Database.Path = path
			ctx := context.Background()
			a, err := audit.Open(filepath.Join(dir, "audit.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m, err := management.Open(filepath.Join(dir, "management.db"), a)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			m.AllowedRoots = []string{dir}
			if err = m.Bootstrap(ctx, "admin", "configuration-test-password"); err != nil {
				t.Fatal(err)
			}
			u, err := m.ByUsername(ctx, "admin")
			if err != nil {
				t.Fatal(err)
			}
			ctx = guard.WithUser(ctx, u)
			c := &catalog.Catalog{Management: m, Audit: a, AsOf: store.DefaultAsOf}
			app := &application.Service{Management: m, Catalog: c}
			if _, err = Import(context.Background(), app, b, "configuration-test-password", false); err == nil {
				t.Fatal("missing administrator accepted")
			}
			if result, err := Import(ctx, app, b, "", true); err != nil || !result.Checked {
				t.Fatalf("check: %+v %v", result, err)
			}
			if sources, err := c.List(); err != nil || len(sources) != 0 {
				t.Fatalf("check wrote config: %+v %v", sources, err)
			}
			if _, err = Import(ctx, app, b, "weak", false); err == nil {
				t.Fatal("weak account password accepted")
			}
			if _, err = Import(ctx, app, b, "configuration-test-password", false); err != nil {
				t.Fatal(err)
			}
			d, err := c.Get(b.Database.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !d.MCPEnabled || len(d.Config.Tables) == 0 || name == "securities" && (len(d.Actions) != 18 || len(d.Views) != 6) {
				t.Fatalf("incomplete import: %+v", d)
			}
			accounts, err := m.BusinessAccounts(ctx, d.ID)
			if err != nil || len(accounts) != len(b.Accounts) {
				t.Fatalf("accounts: %+v %v", accounts, err)
			}
			account := accounts[0]
			account.Active = false
			if _, err = m.SaveBusinessAccount(ctx, account, "updated-account-password", account.AuthVersion); err != nil {
				t.Fatal(err)
			}
			d.MCPEnabled = false
			d.Name = "管理员自定义名称"
			d.Authentication = &catalog.Authentication{Type: "local", Source: "GLOBAL", Access: &management.AuthPolicy{ID: "custom", Name: "自定义认证", Mode: "NONE", BusinessUserID: 10, Permissions: []string{"query"}}}
			if err = c.Save(ctx, *d); err != nil {
				t.Fatal(err)
			}
			before, err := c.Get(d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := Import(ctx, app, b, "", false); err != nil || !result.Preserved {
				t.Fatalf("preserve: %+v %v", result, err)
			}
			after, err := c.Get(d.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("existing configuration overwritten: %v", err)
			}
			saved, err := m.BusinessAccount(ctx, d.ID, account.Username)
			if err != nil || saved.Active || saved.AuthVersion != account.AuthVersion+1 {
				t.Fatalf("account overwritten: %+v %v", saved, err)
			}
			other := *b
			other.Database.Path = filepath.Join(dir, "other.sqlite3")
			if _, err = Import(ctx, app, &other, "configuration-test-password", false); err == nil {
				t.Fatal("same ID accepted a different database target")
			}
		})
	}
}

func TestLoadRejectsUnknownFieldsAndEscapingAssets(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bundle.yml")
	for _, body := range []string{
		"version: 1\nunknown: true\n",
		"version: 1\ndatabase: {id: test, name: Test}\npolicyFile: ../outside.yml\n",
	} {
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(file); err == nil {
			t.Fatal("invalid bundle accepted")
		}
	}
}
