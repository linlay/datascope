package dialect_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"datascope-server/internal/dialect"
	"datascope-server/internal/rewrite"
	_ "modernc.org/sqlite"
)

func engine(driver string) dialect.Engine {
	return dialect.Engine{Driver: driver, Schema: "business", Tables: map[string]string{"records": "Records"}}
}

func TestBindingsAndLiteralBoundaries(t *testing.T) {
	for _, driver := range []string{"mysql", "postgresql", "oracle", "dameng"} {
		t.Run(driver, func(t *testing.T) {
			out, args, err := engine(driver).Compile(`SELECT id, ':who; LIMIT 8' AS literal FROM main.records AS r WHERE owner=:who OR id=:other OR owner=:who`, sql.Named("other", 9), sql.Named("who", 2))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(args, []any{2, 9, 2}) {
				t.Fatalf("binding occurrence order: %v", args)
			}
			if !strings.Contains(out, "':who; LIMIT 8'") {
				t.Fatalf("literal changed: %s", out)
			}
			if strings.Contains(out, "main") || !strings.Contains(out, "Records") {
				t.Fatalf("wrong physical table: %s", out)
			}
			switch driver {
			case "postgresql":
				if !strings.Contains(out, "$3") {
					t.Fatal(out)
				}
			case "oracle", "dameng":
				if !strings.Contains(out, ":3") {
					t.Fatal(out)
				}
			default:
				if strings.Count(out, "?") != 3 {
					t.Fatal(out)
				}
			}
		})
	}
}

func TestOracleNestedLimitsAliasesAndCTE(t *testing.T) {
	out, _, err := engine("oracle").Compile(`WITH x AS (SELECT id FROM main.records LIMIT 3) SELECT r.id AS result FROM x AS r LIMIT 2 OFFSET 1`)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"X" AS (`, `FETCH NEXT 3 ROWS ONLY`, `OFFSET 1 ROWS FETCH NEXT 2 ROWS ONLY`, `"R" . "ID" "RESULT"`} {
		if !strings.Contains(out, part) {
			t.Fatalf("missing %s: %s", part, out)
		}
	}
	if strings.Contains(out, `"X" AS "R"`) {
		t.Fatalf("Oracle table AS alias: %s", out)
	}
	constant, _, err := engine("oracle").Compile(`SELECT CASE WHEN EXISTS(SELECT 1 FROM main.records) THEN 1 ELSE 0 END`)
	if err != nil || !strings.Contains(constant, `FROM "SYS" . "DUAL"`) {
		t.Fatalf("constant SELECT: %s %v", constant, err)
	}
}

func TestRejectUnreviewedOrMutatingSQL(t *testing.T) {
	queries := []string{
		`DELETE FROM records`, `SELECT 1; DELETE FROM records`,
		`WITH x AS (DELETE FROM records RETURNING *) SELECT * FROM x`,
		`SELECT * INTO OUTFILE '/tmp/x' FROM records`,
		`SELECT load_file('/etc/passwd')`, `SELECT pg_sleep(5)`,
		`SELECT nextval('seq')`, `SELECT custom_fn(id) FROM records`,
		`SELECT * FROM other.records`, `SELECT * FROM unregistered`,
		`SELECT * FROM records FOR UPDATE`, `SELECT * FROM pragma_table_info('records')`,
		`SELECT id COLLATE NOCASE FROM records`, `SELECT id || owner FROM records`,
		`SELECT * FROM records LIMIT -1`, `SELECT * FROM records LIMIT :limit`,
		`SELECT ROW_NUMBER() OVER () FROM records`,
		`WITH RECURSIVE x AS (SELECT 1) SELECT * FROM x`,
	}
	for _, driver := range []string{"mysql", "postgresql", "oracle", "dameng"} {
		for _, query := range queries {
			t.Run(driver+"/"+query, func(t *testing.T) {
				if out, _, err := engine(driver).Compile(query); err == nil {
					t.Fatalf("unsafe/unsupported SQL accepted: %s", out)
				}
			})
		}
	}
}

// Execute translated MySQL/PostgreSQL subset SQL against an independent SQLite
// fixture to verify rewrite semantics. Vendor protocol tests live in store's
// opt-in integration suite; this is not a replacement for those tests.
func TestPolicyRewriteCompilationPreservesRowAndColumnScopes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE Records(id INTEGER,owner INTEGER,secret TEXT); INSERT INTO Records VALUES(1,1,'a'),(2,2,'b'),(3,1,'c')`); err != nil {
		t.Fatal(err)
	}
	cfg, err := rewrite.ParseConfig([]byte(`version: 1
schemes: [{scheme: CUSTOM, name: Custom}]
tables:
 - table: records
   scheme: CUSTOM
   columns: [id, owner]
   sql: SELECT * FROM main.records WHERE owner = ${userId}
`))
	if err != nil {
		t.Fatal(err)
	}
	rw, err := rewrite.New(cfg, []string{"Records"})
	if err != nil {
		t.Fatal(err)
	}
	queries := []string{`SELECT * FROM records ORDER BY id`, `WITH x AS (SELECT * FROM records) SELECT * FROM x ORDER BY id`, `SELECT a.id,a.owner FROM records a JOIN records b ON a.id=b.id ORDER BY a.id`}
	for _, driver := range []string{"mysql", "postgresql", "oracle", "dameng"} {
		for _, query := range queries {
			e := engine(driver)
			e.Schema = "main"
			r, err := rw.Rewrite(query)
			if err != nil {
				t.Fatal(err)
			}
			out, args, err := e.Compile(r.RewrittenSQL, sql.Named("userId", 1))
			if err != nil {
				t.Fatalf("%s: %v", driver, err)
			}
			// SQLite requires different binding names for Oracle/DM's numeric binds.
			referenceSQL := out
			if driver == "oracle" || driver == "dameng" {
				referenceSQL = regexp.MustCompile(`:[0-9]+`).ReplaceAllString(out, "?")
			}
			rows, err := db.QueryContext(context.Background(), referenceSQL, args...)
			if err != nil {
				t.Fatalf("%s: %s: %v", driver, out, err)
			}
			cols, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			if len(cols) != 2 {
				t.Fatalf("column leak: %v", cols)
			}
			ids := []string{}
			for rows.Next() {
				var id, owner int64
				if err := rows.Scan(&id, &owner); err != nil {
					t.Fatal(err)
				}
				if owner != 1 {
					t.Fatalf("row scope escaped: %v", owner)
				}
				ids = append(ids, fmt.Sprint(id))
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ids, []string{"1", "3"}) {
				t.Fatalf("%s scope mismatch: %v", driver, ids)
			}
		}
	}
}

func TestExecutableCommentsAreNotForwarded(t *testing.T) {
	out, _, err := engine("mysql").Compile(`SELECT 1 /*! INTO OUTFILE '/tmp/x' */`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "OUTFILE") || strings.Contains(out, "/*") {
		t.Fatalf("comment forwarded: %s", out)
	}
}
