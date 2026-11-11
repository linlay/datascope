// Command import-config loads a declarative configuration bundle into a local deployment.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/configbundle"
	"datascope-server/internal/envflag"
	"datascope-server/internal/guard"
	"datascope-server/internal/platform"
	"datascope-server/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	file := flag.String("file", "", "配置包 YAML 路径")
	check := flag.Bool("check", false, "只验证配置和查询，不导入")
	passwordEnv := flag.String("password-env", "DATASCOPE_EXAMPLE_PASSWORD", "新账号初始密码的环境变量名；不接收明文命令行密码")
	flag.Parse()
	b, err := configbundle.Load(*file)
	if err != nil {
		return err
	}
	ctx := context.Background()
	m, err := platform.Open(ctx)
	if err != nil {
		return err
	}
	defer m.Close()
	defer m.Audit.Close()
	u, err := m.ByUsername(ctx, envflag.String("ADMIN_USERNAME", "admin"))
	if err != nil || !u.Active || !u.SystemAdmin {
		return fmt.Errorf("需要现有在职平台管理员")
	}
	ctx = guard.WithUser(audit.With(ctx, audit.Metadata{ActorID: u.ID, Source: "configuration-import"}), u)
	c := &catalog.Catalog{Management: m, Audit: m.Audit, AsOf: store.DefaultAsOf, Timeout: 60 * time.Second}
	result, err := configbundle.Import(ctx, &application.Service{Management: m, Catalog: c}, b, os.Getenv(*passwordEnv), *check)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
