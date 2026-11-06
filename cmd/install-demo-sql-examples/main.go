// Explicitly restore missing securities SQL examples without changing custom examples.
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
	"datascope-server/internal/demoactions"
	"datascope-server/internal/envflag"
	"datascope-server/internal/guard"
	"datascope-server/internal/platform"
	"datascope-server/internal/store"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	id := flag.String("database", envflag.String("MCP_DATABASE", "default"), "证券演示库 ID")
	source := flag.String("examples", "fixtures/databases/securities_demo/management/sql-examples.yml", "SQL 示例模板")
	check := flag.Bool("check", false, "只检查，不保存")
	flag.Parse()
	raw, err := os.ReadFile(*source)
	if err != nil {
		return err
	}
	var template struct {
		Examples []catalog.SQLExample `yaml:"sqlExamples"`
	}
	if err = yaml.Unmarshal(raw, &template); err != nil {
		return err
	}
	if len(template.Examples) == 0 {
		return fmt.Errorf("SQL 示例模板为空")
	}
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
	ctx = guard.WithUser(audit.With(ctx, audit.Metadata{ActorID: user.ID, Source: "demo-sql-examples-installer", Tool: "install_securities_sql_examples"}), user)
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
	st, err := c.OpenStore(ctx, d)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, example := range template.Examples {
		if example.Label == "" || example.SQL == "" {
			return fmt.Errorf("示例名称和 SQL 不能为空")
		}
		if _, err = st.RawQuery(ctx, example.SQL, 1); err != nil {
			return fmt.Errorf("示例 %s: %w", example.Label, err)
		}
	}
	merged, added := mergeExamples(d.SQLExamples, template.Examples)
	if !*check && added > 0 {
		d.SQLExamples = merged
		app := &application.Service{Management: m, Catalog: c}
		if _, err = app.Call(ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d}); err != nil {
			return err
		}
		if err = m.Flush(ctx); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"database_id": d.ID, "check": *check, "missing_examples": added, "total_examples": len(merged)})
}

func mergeExamples(existing, template []catalog.SQLExample) ([]catalog.SQLExample, int) {
	result := append([]catalog.SQLExample(nil), existing...)
	added := 0
	for _, example := range template {
		found := false
		for _, item := range result {
			if item.Label == example.Label || item.SQL == example.SQL {
				found = true
				break
			}
		}
		if !found {
			result = append(result, example)
			added++
		}
	}
	return result, added
}
