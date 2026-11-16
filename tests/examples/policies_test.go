package examples_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"datascope-server/internal/rewrite"
	_ "modernc.org/sqlite"
)

// Parse against the real schema: parsing a SQL template alone cannot detect
// invalid columns inside authorization subqueries.
func TestDemoPoliciesMatchSchemas(t *testing.T) {
	for _, name := range []string{"securities", "regional-sales", "academic", "channel-operations"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join("../../examples", name)
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "schema.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ddl, err := os.ReadFile(filepath.Join(root, "schema.sqlite.sql"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(string(ddl)); err != nil {
				t.Fatal(err)
			}
			cfg, err := rewrite.LoadConfig(filepath.Join(root, "rewrite.yml"))
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, len(cfg.Tables))
			for i, p := range cfg.Tables {
				names[i] = p.Table
			}
			rw, err := rewrite.New(cfg, names)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range cfg.Tables {
				t.Run(p.Table, func(t *testing.T) {
					result, err := rw.Rewrite(`SELECT * FROM "` + p.Table + `"`)
					if err != nil {
						t.Fatal(err)
					}
					check := func(query string, params []string) {
						t.Helper()
						args := make([]any, 0, len(params))
						for _, param := range params {
							var value any = int64(1)
							if param == "asOf" {
								value = "2026-11-13T04:00:00Z"
							}
							args = append(args, sql.Named(param, value))
						}
						rows, err := db.Query("EXPLAIN "+query, args...)
						if err != nil {
							t.Fatalf("invalid permission SQL: %v", err)
						}
						rows.Close()
					}
					check(result.RewrittenSQL, result.Params)
					for _, guard := range result.Guards {
						check(rewrite.GuardQuery(guard.SQL), guard.Params)
					}
				})
			}
		})
	}
}
