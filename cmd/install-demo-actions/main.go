// Command install-demo-actions explicitly enables the securities action suite.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/demoactions"
	"datascope-server/internal/envflag"
	"datascope-server/internal/guard"
	"datascope-server/internal/platform"
	"datascope-server/internal/store"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	id := flag.String("database", envflag.String("MCP_DATABASE", "default"), "证券演示库 ID")
	names := flag.String("accounts", "E0001,E0121,E0175", "仅向指定的已有业务账号追加 action 权限，逗号分隔；空值不授权")
	replace := flag.Bool("replace", false, "显式替换同名的已修改演示 action")
	backupRoot := flag.String("backup-dir", "data/legacy/action-install", "安装前管理库的一致性备份目录")
	preview := flag.Bool("check", false, "仅检查案例定义、目标库和已有账号，不修改配置")
	migrate := flag.Bool("migrate-scripts", false, "仅将已安装的18个证券 Action和3个公共库脚本升级为 qiuer 写法，保留其他配置和账号")
	flag.Parse()
	ctx := context.Background()
	b, e := demoactions.Load()
	if e != nil {
		return e
	}
	m, e := platform.Open(ctx)
	if e != nil {
		return e
	}
	defer m.Close()
	defer m.Audit.Close()
	u, e := m.ByUsername(ctx, envflag.String("ADMIN_USERNAME", "admin"))
	if e != nil || !u.Active || !u.SystemAdmin {
		return fmt.Errorf("需要现有在职平台管理员执行安装")
	}
	ctx = guard.WithUser(audit.With(ctx, audit.Metadata{ActorID: u.ID, Source: "demo-actions-installer", Tool: "install_securities_actions"}), u)
	asOf, e := time.Parse(time.RFC3339, envflag.String("REWRITE_AS_OF", store.DefaultAsOf.Format(time.RFC3339)))
	if e != nil {
		return e
	}
	c := &catalog.Catalog{Management: m, Audit: m.Audit, AsOf: asOf, Timeout: 5 * time.Second}
	d, e := c.Get(*id)
	if e != nil {
		return e
	}
	if e = demoactions.ValidateSource(ctx, c, d); e != nil {
		return e
	}
	app := &application.Service{Management: m, Catalog: c}
	if *migrate && *preview {
		result, err := demoactions.MigrateScripts(ctx, app, *id, true)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"checked": true, "result": result})
	}
	accounts := []string{}
	grantNames := *names
	if *migrate {
		grantNames = ""
	}
	for _, name := range strings.Split(grantNames, ",") {
		if name = strings.TrimSpace(name); name != "" {
			if _, e = c.BusinessAccount(ctx, d, name); e != nil {
				return e
			}
			accounts = append(accounts, name)
		}
	}
	if *preview {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"checked": true, "database_id": d.ID, "actions": len(b.Actions), "accounts": accounts})
	}
	if e = os.MkdirAll(*backupRoot, 0700); e != nil {
		return e
	}
	path, e := filepath.Abs(filepath.Join(*backupRoot, time.Now().Format("20060102-150405.000000000")+"-management"))
	if e != nil {
		return e
	}
	source, e := m.Source(ctx, d.ID)
	if e != nil {
		return e
	}
	if e = source.Backup(ctx, path); e != nil {
		return fmt.Errorf("管理配置备份失败: %w", e)
	}
	var result demoactions.InstallResult
	if *migrate {
		result, e = demoactions.MigrateScripts(ctx, app, *id, false)
	} else {
		result, e = demoactions.Install(ctx, app, *id, accounts, *replace)
	}
	if e != nil {
		return e
	}
	if e = m.Flush(ctx); e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"result": result, "backup": path, "reauthorize": result.GrantedAccounts})
}
