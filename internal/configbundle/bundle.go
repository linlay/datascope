// Package configbundle loads declarative database configuration without domain-specific installers.
package configbundle

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"datascope-server/internal/action"
	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
	"datascope-server/internal/view"
	"gopkg.in/yaml.v3"
)

type Account struct {
	Username       string   `yaml:"username"`
	Name           string   `yaml:"name"`
	BusinessUserID int64    `yaml:"businessUserId"`
	Permissions    []string `yaml:"permissions"`
}

type Knowledge struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	File        string `yaml:"file"`
	content     string
}

type Bundle struct {
	Version         int              `yaml:"version"`
	Database        catalog.Database `yaml:"database"`
	PolicyFile      string           `yaml:"policyFile"`
	ActionsFile     string           `yaml:"actionsFile"`
	ViewsFile       string           `yaml:"viewsFile"`
	SQLExamplesFile string           `yaml:"sqlExamplesFile"`
	Accounts        []Account        `yaml:"accounts"`
	Knowledge       []Knowledge      `yaml:"knowledge"`
}

func decode(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) > 8<<20 {
		return fmt.Errorf("配置文件超过 8 MiB: %s", path)
	}
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if err := d.Decode(into); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("配置必须为单个 YAML 文档: %s", path)
	}
	return nil
}

func Load(path string) (*Bundle, error) {
	var b Bundle
	if err := decode(path, &b); err != nil {
		return nil, err
	}
	if b.Version != 1 || b.Database.ID == "" || b.Database.Name == "" || b.PolicyFile == "" {
		return nil, fmt.Errorf("需要 version: 1、数据库标识、名称和 policyFile")
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return nil, err
	}
	asset := func(name string) (string, error) {
		if name == "" {
			return "", fmt.Errorf("缺少资源文件名")
		}
		resolved := filepath.Join(base, name)
		resolved, err = filepath.EvalSymlinks(resolved)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(base, resolved)
		if err != nil || filepath.IsAbs(name) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("配置资源必须位于配置包内: %s", name)
		}
		return resolved, nil
	}
	policyPath, err := asset(b.PolicyFile)
	if err != nil {
		return nil, err
	}
	b.Database.Config, err = rewrite.LoadConfig(policyPath)
	if err != nil {
		return nil, err
	}
	if b.Database.Driver == "sqlite" && !filepath.IsAbs(b.Database.Path) {
		if b.Database.Path == "" {
			return nil, fmt.Errorf("SQLite 需要数据库文件路径")
		}
		b.Database.Path = filepath.Join(base, b.Database.Path)
	}
	for _, entry := range []struct {
		name   string
		target any
	}{
		{b.ActionsFile, &struct {
			Version   int                  `yaml:"version"`
			Actions   *[]action.Definition `yaml:"actions"`
			Libraries *[]action.Library    `yaml:"actionLibraries"`
		}{Actions: &b.Database.Actions, Libraries: &b.Database.ActionLibraries}},
		{b.ViewsFile, &struct {
			Views *[]view.Definition `yaml:"views"`
		}{Views: &b.Database.Views}},
		{b.SQLExamplesFile, &struct {
			Examples *[]catalog.SQLExample `yaml:"sqlExamples"`
		}{Examples: &b.Database.SQLExamples}},
	} {
		if entry.name == "" {
			continue
		}
		file, err := asset(entry.name)
		if err != nil {
			return nil, err
		}
		if err = decode(file, entry.target); err != nil {
			return nil, err
		}
	}
	if err := action.ValidateLibraries(b.Database.ActionLibraries); err != nil {
		return nil, err
	}
	for _, a := range b.Database.Actions {
		if err := a.Validate(b.Database.ActionLibraries...); err != nil {
			return nil, err
		}
	}
	for _, v := range b.Database.Views {
		if err := v.Validate(); err != nil {
			return nil, err
		}
	}
	names := map[string]bool{}
	for _, a := range b.Accounts {
		if strings.TrimSpace(a.Username) != a.Username || a.Username == "" || len(a.Username) > 128 || a.Name == "" || a.BusinessUserID <= 0 || names[a.Username] {
			return nil, fmt.Errorf("业务账号清单无效或重名")
		}
		names[a.Username] = true
		for _, p := range a.Permissions {
			if !slices.Contains(management.BusinessPermissions, p) {
				return nil, fmt.Errorf("业务账号能力无效: %s", p)
			}
		}
	}
	names = map[string]bool{}
	for i := range b.Knowledge {
		k := &b.Knowledge[i]
		if k.ID == "" || strings.Contains(k.ID, "/") || k.Name == "" || names[k.ID] {
			return nil, fmt.Errorf("知识条目标识或名称无效")
		}
		names[k.ID] = true
		file, err := asset(k.File)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if len(raw) > 200000 {
			return nil, fmt.Errorf("知识正文超过 200000 字节")
		}
		k.content = string(raw)
	}
	return &b, nil
}

type Result struct {
	DatabaseID  string `json:"database_id"`
	Checked     bool   `json:"checked"`
	Preserved   bool   `json:"preserved"`
	Tables      int    `json:"tables"`
	Accounts    int    `json:"accounts"`
	SQLExamples int    `json:"sql_examples"`
}

// Import creates a new source. Existing sources are preserved in full, including auth and passwords.
func Import(ctx context.Context, app *application.Service, b *Bundle, password string, check bool) (*Result, error) {
	u, ok := guard.User(ctx)
	if !ok || !u.SystemAdmin {
		return nil, management.ErrDenied
	}
	if b == nil || b.Database.Config == nil {
		return nil, fmt.Errorf("缺少配置包")
	}
	d := b.Database
	out := &Result{DatabaseID: d.ID, Checked: check, Tables: len(d.Config.Tables), Accounts: len(b.Accounts), SQLExamples: len(d.SQLExamples)}
	items, err := app.Catalog.List()
	if err != nil {
		return nil, err
	}
	for _, old := range items {
		if old.ID == d.ID {
			if old.Driver != d.Driver || old.Path != d.Path {
				return nil, fmt.Errorf("已有同名数据库指向不同目标，拒绝导入")
			}
			out.Preserved = true
			return out, nil
		}
		if d.Driver == "sqlite" && old.Path == d.Path {
			return nil, fmt.Errorf("数据库文件已使用其他标识登记，保留现有登记")
		}
	}
	if len(b.Accounts) > 0 && !check && len(password) < 12 {
		return nil, fmt.Errorf("新建账号需要通过 -password-env 指定至少 12 字符的初始密码")
	}
	if d.Authentication != nil {
		if err := app.Management.ValidateDatabaseAuth(ctx, d.Authentication.Connection()); err != nil {
			return nil, err
		}
	}
	svc, err := app.Catalog.Open(ctx, &d)
	if err != nil {
		return nil, err
	}
	if d.Authentication != nil && d.Authentication.EmployeeDirectory {
		employees, err := svc.Store.Users(ctx)
		if err != nil {
			svc.Store.Close()
			return nil, err
		}
		for _, a := range b.Accounts {
			if !slices.ContainsFunc(employees, func(e store.UserSummary) bool {
				return e.ID == a.BusinessUserID && e.EmpNo == a.Username && e.Status == "active"
			}) {
				svc.Store.Close()
				return nil, fmt.Errorf("账号 %s 与在职员工目录不匹配", a.Username)
			}
		}
	}
	for _, example := range d.SQLExamples {
		validated := false
		for _, a := range b.Accounts {
			if _, err = svc.Query(ctx, example.SQL, a.BusinessUserID, 5); err != nil {
				if rewrite.IsDenied(err) {
					continue
				}
				svc.Store.Close()
				return nil, fmt.Errorf("示例 %s / %s: %w", example.Label, a.Username, err)
			}
			validated = true
		}
		if len(b.Accounts) > 0 && !validated {
			svc.Store.Close()
			return nil, fmt.Errorf("示例 %s 没有可验证的授权身份", example.Label)
		}
	}
	svc.Store.Close()
	if check {
		return out, nil
	}
	created, err := app.Catalog.Create(ctx, d, "")
	if err != nil {
		return nil, err
	}
	d.Revision = created.Revision
	d.ETag = created.ETag
	d.AuthenticationRevision = created.AuthenticationRevision
	d.Discovery = created.Discovery
	if _, err = app.Call(ctx, "save_database", application.Input{DatabaseID: d.ID, Database: &d}); err != nil {
		return nil, err
	}
	for _, a := range b.Accounts {
		account := management.BusinessAccount{DatabaseID: d.ID, Username: a.Username, Name: a.Name, BusinessUserID: a.BusinessUserID, Permissions: a.Permissions, Active: true}
		if _, err = app.Call(ctx, "save_business_account", application.Input{DatabaseID: d.ID, Account: &account, Password: password}); err != nil {
			return nil, err
		}
	}
	for _, k := range b.Knowledge {
		description := k.Description
		if _, err = app.Call(ctx, "save_knowledge", application.Input{DatabaseID: d.ID, ID: k.ID, Name: k.Name, Description: &description, Content: k.content, Published: true}); err != nil {
			return nil, err
		}
	}
	return out, app.Management.Flush(ctx)
}
