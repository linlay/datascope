package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"datascope-server/internal/audit"
)

func TestLegacySourceMigrationPreservesConfiguration(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "management.sqlite")
	db, e := sql.Open("sqlite", old)
	if e != nil {
		t.Fatal(e)
	}
	ddl := `CREATE TABLE users(id INTEGER PRIMARY KEY,username TEXT UNIQUE,name TEXT,password_hash TEXT,active INTEGER,system_admin INTEGER,auth_version INTEGER);
 CREATE TABLE records(bucket TEXT,key TEXT,data TEXT,PRIMARY KEY(bucket,key));
 CREATE TABLE database_grants(user_id INTEGER,database_id TEXT,permissions TEXT,business_user_id INTEGER,PRIMARY KEY(user_id,database_id));
 CREATE TABLE business_accounts(id INTEGER PRIMARY KEY,database_id TEXT,username TEXT,name TEXT,business_user_id INTEGER,password_hash TEXT,permissions TEXT,active INTEGER,auth_version INTEGER);
 CREATE TABLE action_requests(database_id TEXT,actor TEXT,request_id TEXT,fingerprint TEXT,revision INTEGER,state TEXT,data TEXT,token TEXT,expires INTEGER,PRIMARY KEY(database_id,actor,request_id));
 CREATE TABLE record_versions(bucket TEXT,key TEXT,revision INTEGER,data TEXT,created_at TEXT,PRIMARY KEY(bucket,key,revision));`
	if _, e = db.Exec(ddl); e != nil {
		t.Fatal(e)
	}
	hash, e := passwordHash("legacy-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO users VALUES(1,'admin','Admin',?,1,1,3);INSERT INTO business_accounts VALUES(7,'source','E0001','Employee',10,?,'[\"query\",\"action\"]',1,4)", hash, hash); e != nil {
		t.Fatal(e)
	}
	d := map[string]any{"id": "source", "name": "迁移源", "driver": "sqlite", "path": "business.db", "mcp_path": "finance", "revision": 9, "config": map[string]any{"version": 1, "tables": []any{map[string]any{"table": "items", "sql": "SELECT * FROM items WHERE owner_id=:user_id", "compiled_sql": "runtime", "params": []string{"user_id"}}}}, "actions": []any{map[string]any{"name": "edit", "title": "Edit", "description": "Edit", "confirm": true, "parameters": map[string]any{}, "script": "function run(args,ctx){return {ok:true}}"}}}
	raw, _ := json.Marshal(d)
	for _, q := range []string{"INSERT INTO records VALUES('databases','source',?)", "INSERT INTO record_versions VALUES('databases','source',9,?,'2026-01-01')"} {
		if _, e = db.Exec(q, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(`INSERT INTO database_grants VALUES(1,'source','["query"]',10);INSERT INTO action_requests VALUES('source','business:7','durable','fingerprint',9,'running','{}','',0);INSERT INTO records VALUES('knowledge','source/guide','{"name":"Guide","summary":"业务摘要","content":"查询案例"}');`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO record_versions VALUES('databases','source',1,'{\"name\":\"original version\"}','2025-01-01')"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	target := filepath.Join(dir, "management.db")
	if e = CopyLegacyState(old, target); e != nil {
		t.Fatal(e)
	}
	a, _ := audit.Open(filepath.Join(dir, "audit.db"))
	defer a.Close()
	m, e := Open(target, a)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	ctx := context.Background()
	account, e := m.AuthenticateBusiness(ctx, "source", "E0001", "legacy-password")
	if e != nil || account.ID != 7 || account.AuthVersion != 4 {
		t.Fatalf("account migration: %+v %v", account, e)
	}
	x, e := m.Source(ctx, "source")
	if e != nil {
		t.Fatal(e)
	}
	var receipts int
	if e = x.DB.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='ACTION_RECEIPT'").Scan(&receipts); e != nil || receipts != 0 {
		t.Fatalf("runtime receipt table created: %d %v", receipts, e)
	}
	var original string
	if e = x.DB.QueryRow("SELECT DATA_ FROM CONFIG_VERSION WHERE BUCKET_='databases' AND KEY_='source' AND REVISION_=1").Scan(&original); e != nil || !strings.Contains(original, "original version") {
		t.Fatalf("original history overwritten: %s %v", original, e)
	}
	var data string
	if e = x.DB.QueryRow("SELECT DATA_ FROM QUERY_POLICY").Scan(&data); e != nil || strings.Contains(data, "compiled_sql") || strings.Contains(data, "params") {
		t.Fatalf("runtime SQL persisted: %s %v", data, e)
	}
	for _, table := range []string{"BUSINESS_ACCOUNT", "PLATFORM_GRANT", "ACTION_RECEIPT", "CONFIG_RECORD", "CONFIG_VERSION", "PLATFORM_SESSION", "OAUTH_GRANT", "AUDIT_OUTBOX"} {
		var n int
		if e = m.DB.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name=?", table).Scan(&n); e != nil || n != 0 {
			t.Fatalf("per-source/runtime table remains global: %s", table)
		}
	}
	for _, handle := range []*sql.DB{m.DB, x.DB} {
		rows, e := handle.Query("SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'KNOWLEDGE_SEARCH%'")
		if e != nil {
			t.Fatal(e)
		}
		names := []string{}
		for rows.Next() {
			var n string
			rows.Scan(&n)
			names = append(names, n)
		}
		rows.Close()
		for _, name := range names {
			if name != strings.ToUpper(name) {
				t.Fatal(name)
			}
			cols, e := handle.Query("PRAGMA table_info(" + name + ")")
			if e != nil {
				t.Fatal(e)
			}
			for cols.Next() {
				var index, notnull, pk int
				var col, typ string
				var def any
				cols.Scan(&index, &col, &typ, &notnull, &def, &pk)
				if col != strings.ToUpper(col) || !strings.HasSuffix(col, "_") {
					t.Fatalf("invalid field %s.%s", name, col)
				}
			}
			cols.Close()
		}
	}
	m.AllowedRoots = []string{dir}
	for _, private := range []string{old, target, x.Path, x.Audit.Path} {
		if e = m.ValidatePath(private); e == nil {
			t.Fatalf("private metadata registered: %s", private)
		}
		link := filepath.Join(dir, "business-link.db")
		if e = os.Link(private, link); e != nil {
			t.Fatal(e)
		}
		if e = m.ValidatePath(link); e == nil {
			t.Fatalf("private hard link registered: %s", private)
		}
		os.Remove(link)
	}
	// Original files remain available; a second process opens the migrated state.
	if _, e = os.Stat(old); e != nil {
		t.Fatal(e)
	}
	second, e := Open(target, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	a2, e := second.BusinessAccount(ctx, "source", "E0001")
	if e != nil || a2.ID != account.ID {
		t.Fatal(e)
	}
}

func TestDerivedCompilationDoesNotEraseActionParameters(t *testing.T) {
	d := map[string]any{"actions": []any{map[string]any{"parameters": map[string]any{"params": map[string]any{"type": "string"}}}}, "config": map[string]any{"tables": []any{map[string]any{"params": []any{"user_id"}, "compiled_sql": "SELECT 1"}}}}
	cleanDerived(d)
	params := d["actions"].([]any)[0].(map[string]any)["parameters"].(map[string]any)
	if _, ok := params["params"]; !ok {
		t.Fatal("action parameter removed")
	}
	policy := d["config"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	if _, ok := policy["params"]; ok {
		t.Fatal("derived policy parameters persisted")
	}
}
