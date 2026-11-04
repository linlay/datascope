// Command install-demo-masking adds field masking examples to the registered
// synthetic securities database. Starting servers never overwrites these rules.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
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

type fieldRule struct {
	Table   string        `json:"table"`
	Column  string        `json:"column"`
	Masking store.Masking `json:"masking"`
}

var examples = []fieldRule{
	{"customers", "name", store.Masking{Type: "partial", KeepPrefix: 1}},
	{"customers", "mobile", store.Masking{Type: "partial", KeepPrefix: 3, KeepSuffix: 4}},
	{"customers", "id_no", store.Masking{Type: "hmac"}},
	{"customers", "birth_date", store.Masking{Type: "omit"}},
	{"employees", "name", store.Masking{Type: "partial", KeepPrefix: 1}},
	{"employees", "mobile", store.Masking{Type: "full"}},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	id := flag.String("database", "default", "证券演示库 ID")
	check := flag.Bool("check", false, "仅检查目标库并列出缺少规则的字段，不修改配置")
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
	ctx = guard.WithUser(audit.With(ctx, audit.Metadata{ActorID: user.ID, Source: "demo-masking-installer", Tool: "install_securities_masking"}), user)
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
	if d.Discovery == nil {
		return fmt.Errorf("请先登记证券库的表结构")
	}
	added := []fieldRule{}
	preserved := []string{}
	for _, example := range examples {
		found := false
		for i := range d.Discovery.Tables {
			table := &d.Discovery.Tables[i]
			if table.Name != example.Table {
				continue
			}
			for j := range table.Columns {
				column := &table.Columns[j]
				if column.Name != example.Column {
					continue
				}
				found = true
				// Even an explicit 'none' is a user decision and must be preserved.
				if column.Masking != nil {
					preserved = append(preserved, example.Table+"."+example.Column)
					break
				}
				rule := example.Masking
				column.Masking = &rule
				added = append(added, example)
				break
			}
		}
		if !found {
			return fmt.Errorf("证券库缺少已登记字段 %s.%s", example.Table, example.Column)
		}
	}
	if !*check && len(added) > 0 {
		app := &application.Service{Management: m, Catalog: c}
		if _, err = app.Call(ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d}); err != nil {
			return err
		}
		if err = m.Flush(ctx); err != nil {
			return err
		}
	}
	saved, err := c.Get(d.ID)
	if err != nil {
		return err
	}
	cleaned := []string{}
	for _, field := range []struct{ table, column, old string }{
		{"customers", "id_no", "证件号码（预先脱敏）"},
		{"customers", "mobile", "手机号码（预先脱敏）"},
		{"employees", "mobile", "手机号码（预先脱敏）"},
	} {
		annotation := saved.Discovery.Annotations[field.table].Columns[field.column]
		comment := ""
		for _, table := range saved.Discovery.Tables {
			if table.Name == field.table {
				for _, column := range table.Columns {
					if column.Name == field.column {
						comment = column.Comment
					}
				}
			}
		}
		if annotation.Comment != nil {
			comment = *annotation.Comment
		}
		if comment != field.old {
			continue
		}
		cleaned = append(cleaned, field.table+"."+field.column)
		if !*check {
			plain := strings.TrimSuffix(comment, "（预先脱敏）")
			annotation.Comment = &plain
			app := &application.Service{Management: m, Catalog: c}
			if _, err = app.Call(ctx, "save_annotation", application.Input{DatabaseID: saved.ID, Table: field.table, Column: field.column, Revision: saved.Revision, Annotation: &annotation}); err != nil {
				return err
			}
			saved, err = c.Get(d.ID)
			if err != nil {
				return err
			}
		}
	}
	if !*check {
		if err = m.Flush(ctx); err != nil {
			return err
		}
	}
	result := map[string]any{"database_id": d.ID, "database_name": d.Name, "check": *check, "comments_cleaned": cleaned, "rules": added, "preserved": preserved, "revision": saved.Revision}
	if !*check {
		st, err := c.OpenStore(ctx, saved)
		if err != nil {
			return err
		}
		defer st.Close()
		if _, err = st.RawQuery(ctx, "SELECT customer_no, name, mobile, id_no, birth_date FROM customers ORDER BY customer_no", 2); err != nil {
			return err
		}
		if _, err = st.RawQuery(ctx, "SELECT employee_id, name, mobile FROM employees ORDER BY employee_id", 2); err != nil {
			return err
		}
		if _, err = st.Browse(ctx, store.BrowseOptions{Table: "customers", Limit: 1}); err != nil {
			return err
		}
		result["queries_verified"] = true
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
