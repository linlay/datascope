// Command install-demo-views explicitly adds securities-specific view examples.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/demoactions"
	"datascope-server/internal/envflag"
	"datascope-server/internal/guard"
	"datascope-server/internal/platform"
	"datascope-server/internal/store"
	"datascope-server/internal/view"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	id := flag.String("database", envflag.String("MCP_DATABASE", "default"), "证券演示库 ID")
	check := flag.Bool("check", false, "检查待新增视图，不保存")
	replace := flag.Bool("replace", false, "替换已有同名证券视图；默认保留已有视图")
	backupDir := flag.String("backup-dir", "data/legacy/view-install", "安装前配置备份目录")
	flag.Parse()
	ctx := context.Background()
	m, err := platform.Open(ctx)
	if err != nil {
		return err
	}
	defer m.Close()
	defer m.Audit.Close()
	user, err := m.ByUsername(ctx, envflag.String("ADMIN_USERNAME", "admin"))
	if err != nil || !user.Active || !user.SystemAdmin {
		return fmt.Errorf("需要现有在职平台管理员执行安装")
	}
	ctx = guard.WithUser(audit.With(ctx, audit.Metadata{ActorID: user.ID, Source: "demo-views-installer", Tool: "install_securities_views"}), user)
	asOf, err := time.Parse(time.RFC3339, envflag.String("REWRITE_AS_OF", store.DefaultAsOf.Format(time.RFC3339)))
	if err != nil {
		return err
	}
	c := &catalog.Catalog{Management: m, Audit: m.Audit, AsOf: asOf, Timeout: 5 * time.Second}
	d, err := c.Get(*id)
	if err != nil {
		return err
	}
	if err = demoactions.ValidateSource(ctx, c, d); err != nil {
		return err
	}
	added, replaced, preserved := []string{}, []string{}, []string{}
	for _, v := range view.SecuritiesViews() {
		if err = v.Validate(); err != nil {
			return err
		}
		index := slices.IndexFunc(d.Views, func(old view.Definition) bool { return old.ID == v.ID })
		if index < 0 {
			d.Views = append(d.Views, v)
			added = append(added, v.ID)
		} else if !demoactions.EqualJSON(d.Views[index], v) {
			if *replace {
				d.Views[index] = v
				replaced = append(replaced, v.ID)
			} else {
				preserved = append(preserved, v.ID)
			}
		}
	}
	backup := ""
	if !*check && len(added)+len(replaced) > 0 {
		source, err := m.Source(ctx, d.ID)
		if err != nil {
			return err
		}
		backup, err = filepath.Abs(filepath.Join(*backupDir, time.Now().Format("20060102-150405.000000000")))
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
			return err
		}
		if err = source.Backup(ctx, backup); err != nil {
			return err
		}
		app := &application.Service{Management: m, Catalog: c}
		if _, err = app.Call(ctx, "configure_views", application.Input{DatabaseID: d.ID, Database: d}); err != nil {
			return err
		}
		if err = m.Flush(ctx); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"database_id": d.ID, "check": *check, "added": added, "replaced": replaced, "preserved": preserved, "backup": backup})
}
