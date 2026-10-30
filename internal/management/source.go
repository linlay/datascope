package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"datascope-server/internal/audit"
)

type Source struct {
	DB    *sql.DB
	Path  string
	Audit *audit.Store
}

func (s *Source) Close() {
	s.DB.Close()
	if s.Audit != nil {
		s.Audit.Close()
	}
}

var sourceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

const sourceDDL = `
CREATE TABLE IF NOT EXISTS SOURCE_CONFIG(ID_ TEXT PRIMARY KEY,DATA_ TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS CONNECTION_CONFIG(PURPOSE_ TEXT PRIMARY KEY CHECK(PURPOSE_ IN('READ','WRITE')),CONFIG_JSON_ TEXT NOT NULL DEFAULT '{}',PASSWORD_CIPHERTEXT_ BLOB);
CREATE TABLE IF NOT EXISTS PLATFORM_GRANT(USER_ID_ INTEGER NOT NULL,DATABASE_ID_ TEXT NOT NULL,PERMISSIONS_ TEXT NOT NULL,BUSINESS_USER_ID_ INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(USER_ID_,DATABASE_ID_));
CREATE TABLE IF NOT EXISTS BUSINESS_ACCOUNT(ID_ INTEGER PRIMARY KEY,DATABASE_ID_ TEXT NOT NULL,USERNAME_ TEXT NOT NULL,NAME_ TEXT NOT NULL,BUSINESS_USER_ID_ INTEGER NOT NULL CHECK(BUSINESS_USER_ID_>0),PASSWORD_HASH_ TEXT NOT NULL,PERMISSIONS_ TEXT NOT NULL,ACTIVE_ INTEGER NOT NULL DEFAULT 1,AUTH_VERSION_ INTEGER NOT NULL DEFAULT 1,UNIQUE(DATABASE_ID_,USERNAME_));
CREATE TABLE IF NOT EXISTS ACTION_RECEIPT(DATABASE_ID_ TEXT NOT NULL,ACTOR_ TEXT NOT NULL,REQUEST_ID_ TEXT NOT NULL,FINGERPRINT_ TEXT NOT NULL,REVISION_ INTEGER NOT NULL,STATE_ TEXT NOT NULL,DATA_ TEXT NOT NULL,TOKEN_ TEXT NOT NULL DEFAULT '',EXPIRES_ INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(DATABASE_ID_,ACTOR_,REQUEST_ID_));
CREATE UNIQUE INDEX IF NOT EXISTS IX_ACTION_RECEIPT_TOKEN ON ACTION_RECEIPT(TOKEN_) WHERE TOKEN_<>'';
CREATE TABLE IF NOT EXISTS QUERY_POLICY(TABLE_NAME_ TEXT PRIMARY KEY,SQL_ TEXT NOT NULL,DATA_ TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS QUERY_FRAGMENT(NAME_ TEXT PRIMARY KEY,SQL_ TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS QUERY_TOOL(NAME_ TEXT PRIMARY KEY,DESCRIPTION_ TEXT NOT NULL,SQL_ TEXT NOT NULL,PARAMETER_JSON_ TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS ACTION_DEFINITION(NAME_ TEXT PRIMARY KEY,TITLE_ TEXT NOT NULL,DESCRIPTION_ TEXT NOT NULL,CONFIRM_ INTEGER NOT NULL,PARAMETER_JSON_ TEXT NOT NULL,SCRIPT_ TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS KNOWLEDGE(KEY_ TEXT PRIMARY KEY,DATA_ TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS CONFIG_RECORD(BUCKET_ TEXT NOT NULL,KEY_ TEXT NOT NULL,DATA_ TEXT NOT NULL,PRIMARY KEY(BUCKET_,KEY_));
CREATE TABLE IF NOT EXISTS CONFIG_VERSION(BUCKET_ TEXT NOT NULL,KEY_ TEXT NOT NULL,REVISION_ INTEGER NOT NULL,DATA_ TEXT NOT NULL,CREATED_AT_ TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,PRIMARY KEY(BUCKET_,KEY_,REVISION_));
CREATE TABLE IF NOT EXISTS SCHEMA_VERSION(VERSION_ INTEGER PRIMARY KEY,APPLIED_AT_ TEXT NOT NULL);
INSERT OR IGNORE INTO SCHEMA_VERSION VALUES(1,CURRENT_TIMESTAMP);

`

func (s *Store) source(ctx context.Context, id string, create bool) (*Source, error) {
	if !sourceID.MatchString(id) {
		return nil, fmt.Errorf("非法数据源 ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if x := s.sources[id]; x != nil {
		return x, nil
	}
	path := filepath.Join(filepath.Dir(s.Path), "databases", id, "management.db")
	if !create {
		var n int
		if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM SOURCE_REGISTRY WHERE DATABASE_ID_=?", id).Scan(&n); e != nil {
			return nil, e
		}
		if n == 0 {
			return nil, sql.ErrNoRows
		}
		if _, e := os.Stat(path); e != nil {
			return nil, fmt.Errorf("数据源管理库不可用: %w", e)
		}
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(4)
	if _, e = db.ExecContext(ctx, sourceDDL); e != nil {
		db.Close()
		return nil, e
	}
	if e = migrateKnowledgeDescriptions(ctx, db); e != nil {
		db.Close()
		return nil, e
	}
	auditPath := filepath.Join(filepath.Dir(path), "audit.db")
	managementInfo, _ := os.Stat(path)
	auditInfo, _ := os.Stat(auditPath)
	if managementInfo != nil && auditInfo != nil && os.SameFile(managementInfo, auditInfo) {
		db.Close()
		return nil, fmt.Errorf("业务管理库与审计库必须分开")
	}
	a, _ := audit.Open(auditPath)
	x := &Source{DB: db, Path: path, Audit: a}
	s.sources[id] = x
	return x, nil
}
func (s *Store) Source(ctx context.Context, id string) (*Source, error) {
	return s.source(ctx, id, false)
}
func (s *Store) sourceDB(id string) *sql.DB {
	x, e := s.source(context.Background(), id, true)
	if e != nil {
		return s.DB
	}
	return x.DB
}
func (s *Store) AuditFor(id string) *audit.Store {
	x, e := s.Source(context.Background(), id)
	if e != nil {
		return s.Audit
	}
	return x.Audit
}
func (s *Store) sourceIDs() ([]string, error) {
	rows, e := s.DB.Query("SELECT DATABASE_ID_ FROM SOURCE_REGISTRY ORDER BY DATABASE_ID_")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) sourceChange(ctx context.Context, id, kind string, detail any, fn func(*sql.Tx) error) error {
	x, e := s.source(ctx, id, true)
	if e != nil {
		return e
	}
	tx, e := x.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	ev := audit.Event{Metadata: audit.From(ctx), ID: audit.ID(), Kind: kind, Status: "success", StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Detail: detail}
	ev.DatabaseID = id
	return x.Audit.Put(ctx, ev)
}

func (s *Source) load(ctx context.Context) (map[string]any, error) {
	var raw string
	if e := s.DB.QueryRowContext(ctx, "SELECT DATA_ FROM SOURCE_CONFIG LIMIT 1").Scan(&raw); e != nil {
		return nil, e
	}
	var d map[string]any
	if e := json.Unmarshal([]byte(raw), &d); e != nil {
		return nil, e
	}
	cfg, _ := d["config"].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
		d["config"] = cfg
	}
	tables := []any{}
	rows, e := s.DB.QueryContext(ctx, "SELECT DATA_ FROM QUERY_POLICY ORDER BY TABLE_NAME_")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			rows.Close()
			return nil, e
		}
		var p any
		if e = json.Unmarshal([]byte(b), &p); e != nil {
			rows.Close()
			return nil, e
		}
		tables = append(tables, p)
	}
	rows.Close()
	cfg["tables"] = tables
	fragments := map[string]string{}
	rows, e = s.DB.QueryContext(ctx, "SELECT NAME_,SQL_ FROM QUERY_FRAGMENT ORDER BY NAME_")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var n, v string
		rows.Scan(&n, &v)
		fragments[n] = v
	}
	rows.Close()
	cfg["fragments"] = fragments
	for _, purpose := range []string{"READ", "WRITE"} {
		var b string
		if e = s.DB.QueryRowContext(ctx, "SELECT CONFIG_JSON_ FROM CONNECTION_CONFIG WHERE PURPOSE_=?", purpose).Scan(&b); e == nil {
			var c any
			if e = json.Unmarshal([]byte(b), &c); e != nil {
				return nil, e
			}
			if purpose == "READ" {
				d["connection"] = c
			} else {
				d["write_connection"] = c
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
	}
	actions := []any{}
	rows, e = s.DB.QueryContext(ctx, "SELECT NAME_,TITLE_,DESCRIPTION_,CONFIRM_,PARAMETER_JSON_,SCRIPT_ FROM ACTION_DEFINITION ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var n, t, desc, p, script string
		var confirm bool
		if e = rows.Scan(&n, &t, &desc, &confirm, &p, &script); e != nil {
			rows.Close()
			return nil, e
		}
		var params any
		json.Unmarshal([]byte(p), &params)
		actions = append(actions, map[string]any{"name": n, "title": t, "description": desc, "confirm": confirm, "parameters": params, "script": script})
	}
	rows.Close()
	if len(actions) > 0 {
		d["actions"] = actions
	}
	tools := []any{}
	rows, e = s.DB.QueryContext(ctx, "SELECT NAME_,DESCRIPTION_,SQL_,PARAMETER_JSON_ FROM QUERY_TOOL ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var n, desc, q, p string
		rows.Scan(&n, &desc, &q, &p)
		var params any
		json.Unmarshal([]byte(p), &params)
		tools = append(tools, map[string]any{"name": n, "description": desc, "sql": q, "parameters": params})
	}
	rows.Close()
	if len(tools) > 0 {
		d["business_tools"] = tools
	}
	return d, nil
}

// Only rewrite policy compilation is derived. Action parameter names such as
// "params" remain valid user configuration and must never be removed.
func cleanDerived(v map[string]any) {
	cfg, _ := v["config"].(map[string]any)
	policies, _ := cfg["tables"].([]any)
	for _, v := range policies {
		if p, ok := v.(map[string]any); ok {
			delete(p, "compiled_sql")
			delete(p, "compiled_deny_unless")
			delete(p, "params")
		}
	}
}
func (s *Source) save(ctx context.Context, id string, d map[string]any, credentials []DatabaseCredential) error {
	return s.saveState(ctx, id, d, credentials, true)
}
func (s *Source) saveState(ctx context.Context, id string, d map[string]any, credentials []DatabaseCredential, recordHistory bool) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	cleanDerived(d)
	raw, e := json.Marshal(d)
	if e != nil {
		return e
	}
	var old string
	_ = tx.QueryRowContext(ctx, "SELECT DATA_ FROM SOURCE_CONFIG WHERE ID_=?", id).Scan(&old)
	if recordHistory && old != string(raw) {
		if _, e = tx.ExecContext(ctx, "INSERT INTO CONFIG_VERSION(BUCKET_,KEY_,REVISION_,DATA_) SELECT 'databases',?,COALESCE(MAX(REVISION_),0)+1,? FROM CONFIG_VERSION WHERE BUCKET_='databases' AND KEY_=?", id, string(raw), id); e != nil {
			return e
		}
	}
	cfg, _ := d["config"].(map[string]any)
	for _, table := range []string{"QUERY_POLICY", "QUERY_FRAGMENT", "ACTION_DEFINITION", "QUERY_TOOL"} {
		if _, e = tx.ExecContext(ctx, "DELETE FROM "+table); e != nil {
			return e
		}
	}
	if cfg != nil {
		if list, ok := cfg["tables"].([]any); ok {
			for _, v := range list {
				p := v.(map[string]any)
				b, _ := json.Marshal(p)
				q, _ := p["sql"].(string)
				if _, e = tx.ExecContext(ctx, "INSERT INTO QUERY_POLICY VALUES(?,?,?)", p["table"], q, string(b)); e != nil {
					return e
				}
			}
		}
		if fs, ok := cfg["fragments"].(map[string]any); ok {
			for n, v := range fs {
				if _, e = tx.ExecContext(ctx, "INSERT INTO QUERY_FRAGMENT VALUES(?,?)", n, v); e != nil {
					return e
				}
			}
		}
		delete(cfg, "tables")
		delete(cfg, "fragments")
	}
	for _, field := range []string{"actions", "business_tools"} {
		if list, ok := d[field].([]any); ok {
			for _, v := range list {
				a := v.(map[string]any)
				params, _ := json.Marshal(a["parameters"])
				if field == "actions" {
					_, e = tx.ExecContext(ctx, "INSERT INTO ACTION_DEFINITION VALUES(?,?,?,?,?,?)", a["name"], a["title"], a["description"], a["confirm"], string(params), a["script"])
				} else {
					_, e = tx.ExecContext(ctx, "INSERT INTO QUERY_TOOL VALUES(?,?,?,?)", a["name"], a["description"], a["sql"], string(params))
				}
				if e != nil {
					return e
				}
			}
		}
		delete(d, field)
	}
	for field, purpose := range map[string]string{"connection": "READ", "write_connection": "WRITE"} {
		if v, ok := d[field]; ok && v != nil {
			b, _ := json.Marshal(v)
			if _, e = tx.ExecContext(ctx, "INSERT INTO CONNECTION_CONFIG(PURPOSE_,CONFIG_JSON_) VALUES(?,?) ON CONFLICT(PURPOSE_) DO UPDATE SET CONFIG_JSON_=excluded.CONFIG_JSON_", purpose, string(b)); e != nil {
				return e
			}
		} else {
			if _, e = tx.ExecContext(ctx, "DELETE FROM CONNECTION_CONFIG WHERE PURPOSE_=?", purpose); e != nil {
				return e
			}
		}
		delete(d, field)
	}
	for _, c := range credentials {
		purpose := "READ"
		if c.DatabaseID == WriteCredentialID(id) {
			purpose = "WRITE"
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO CONNECTION_CONFIG(PURPOSE_,PASSWORD_CIPHERTEXT_) VALUES(?,?) ON CONFLICT(PURPOSE_) DO UPDATE SET PASSWORD_CIPHERTEXT_=excluded.PASSWORD_CIPHERTEXT_", purpose, c.Ciphertext); e != nil {
			return e
		}
	}
	b, _ := json.Marshal(d)
	if _, e = tx.ExecContext(ctx, "INSERT INTO SOURCE_CONFIG VALUES(?,?) ON CONFLICT(ID_) DO UPDATE SET DATA_=excluded.DATA_", id, string(b)); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) records(ctx context.Context, bucket string) ([]Record, error) {
	ids, e := s.sourceIDs()
	if e != nil {
		return nil, e
	}
	out := []Record{}
	for _, id := range ids {
		x, e := s.Source(ctx, id)
		if e != nil {
			return nil, e
		}
		if bucket == "databases" {
			d, e := x.load(ctx)
			if e != nil {
				return nil, e
			}
			var name, route string
			if e = s.DB.QueryRowContext(ctx, "SELECT NAME_,MCP_PATH_ FROM SOURCE_REGISTRY WHERE DATABASE_ID_=?", id).Scan(&name, &route); e != nil {
				return nil, e
			}
			d["name"], d["mcp_path"] = name, route
			b, _ := json.Marshal(d)
			out = append(out, Record{Key: id, Data: string(b)})
			continue
		}
		q := "SELECT KEY_,DATA_ FROM CONFIG_RECORD WHERE BUCKET_=? ORDER BY KEY_"
		args := []any{bucket}
		if bucket == "knowledge" {
			q = "SELECT KEY_,DATA_ FROM KNOWLEDGE ORDER BY KEY_"
			args = nil
		}
		rows, e := x.DB.QueryContext(ctx, q, args...)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var r Record
			if e = rows.Scan(&r.Key, &r.Data); e != nil {
				rows.Close()
				return nil, e
			}
			out = append(out, r)
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return nil, e
		}
		rows.Close()
	}
	return out, nil
}
func (s *Store) updateRecords(ctx context.Context, bucket, kind, database string, fn func([]Record) ([]Record, error), credentials ...DatabaseCredential) error {
	lock, e := os.OpenFile(s.Path+".config.lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); e != nil {
		return e
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if len(credentials) > 1 {
		return fmt.Errorf("一次只接受一个连接凭据")
	}
	if len(credentials) == 1 {
		c := credentials[0]
		if bucket != "databases" || !(kind == "database.create" && c.DatabaseID == database || kind == "database.update" && c.DatabaseID == WriteCredentialID(database)) {
			return fmt.Errorf("凭据只能随对应数据源创建或写连接更新保存")
		}
	}
	before, e := s.records(ctx, bucket)
	if e != nil {
		return e
	}
	updated, e := fn(before)
	if e != nil {
		return e
	}
	if bucket == "databases" {
		if len(credentials) > 0 {
			found := false
			for _, r := range updated {
				found = found || r.Key == database
			}
			if !found {
				return fmt.Errorf("缺少凭据所属的数据源配置")
			}
		}
		routes := map[string]bool{}
		oldRecords := map[string]string{}
		for _, r := range before {
			oldRecords[r.Key] = r.Data
		}
		for _, r := range updated {
			var v map[string]any
			if e = json.Unmarshal([]byte(r.Data), &v); e != nil {
				return e
			}
			route, _ := v["mcp_path"].(string)
			if route == "" {
				route = r.Key
			}
			if routes[route] {
				return fmt.Errorf("MCP 路径重复")
			}
			routes[route] = true
		}
		for _, r := range updated {
			if oldRecords[r.Key] == r.Data && !(len(credentials) > 0 && r.Key == database) {
				continue
			}
			var d map[string]any
			if e = json.Unmarshal([]byte(r.Data), &d); e != nil {
				return e
			}
			if !sourceID.MatchString(r.Key) {
				return fmt.Errorf("非法数据源 ID")
			}
			for _, c := range credentials {
				if c.DatabaseID != r.Key && c.DatabaseID != WriteCredentialID(r.Key) {
					if r.Key == database {
						return fmt.Errorf("凭据不属于目标数据源")
					}
				}
			}
			x, e := s.source(ctx, r.Key, true)
			if e != nil {
				return e
			}
			cred := []DatabaseCredential{}
			for _, v := range credentials {
				if v.DatabaseID == r.Key || v.DatabaseID == WriteCredentialID(r.Key) {
					cred = append(cred, v)
				}
			}
			if e = x.save(ctx, r.Key, d, cred); e != nil {
				return e
			}
			name, _ := d["name"].(string)
			if name == "" {
				name = r.Key
			}
			route, _ := d["mcp_path"].(string)
			if route == "" {
				route = r.Key
			}
			if _, e = s.DB.ExecContext(ctx, "INSERT INTO SOURCE_REGISTRY(DATABASE_ID_,NAME_,MCP_PATH_,STATE_) VALUES(?,?,?,'ACTIVE') ON CONFLICT(DATABASE_ID_) DO UPDATE SET NAME_=excluded.NAME_,MCP_PATH_=excluded.MCP_PATH_", r.Key, name, route); e != nil {
				return e
			}
			if kind == "database.create" {
				actor := audit.From(ctx).ActorID
				if actor > 0 {
					p, _ := json.Marshal([]string{"config.read", "config.write", "policy.write", "mcp.write", "knowledge.write", "audit.read", "grant"})
					if _, e = x.DB.ExecContext(ctx, "INSERT OR IGNORE INTO PLATFORM_GRANT VALUES(?,?,?,0)", actor, r.Key, string(p)); e != nil {
						return e
					}
				}
			}
		}
	} else {
		if database == "" {
			return fmt.Errorf("逐库记录必须指定数据库")
		}
		x, e := s.Source(ctx, database)
		if e != nil {
			return e
		}
		tx, e := x.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		for _, r := range updated {
			if bucket == "knowledge" {
				r.Data, e = canonicalKnowledgeJSON(r.Data)
				if e != nil {
					return e
				}
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO CONFIG_VERSION(BUCKET_,KEY_,REVISION_,DATA_) SELECT ?,?,COALESCE(MAX(REVISION_),0)+1,? FROM CONFIG_VERSION WHERE BUCKET_=? AND KEY_=?", bucket, r.Key, r.Data, bucket, r.Key); e != nil {
				return e
			}
			if bucket == "knowledge" {
				_, e = tx.ExecContext(ctx, "INSERT INTO KNOWLEDGE VALUES(?,?) ON CONFLICT(KEY_) DO UPDATE SET DATA_=excluded.DATA_", r.Key, r.Data)
			} else {
				_, e = tx.ExecContext(ctx, "INSERT INTO CONFIG_RECORD VALUES(?,?,?) ON CONFLICT(BUCKET_,KEY_) DO UPDATE SET DATA_=excluded.DATA_", bucket, r.Key, r.Data)
			}
			if e != nil {
				return e
			}
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	ev := audit.Event{Metadata: audit.From(ctx), ID: audit.ID(), Kind: kind, Status: "success", StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Detail: map[string]any{"bucket": bucket}}
	ev.DatabaseID = database
	return s.Audit.Put(ctx, ev)
}
func (s *Store) internalPaths() []string {
	root := filepath.Dir(s.Path)
	paths := []string{s.Path, s.AuditPath(), s.Path + ".credentials.key", filepath.Join(root, "management.db"), filepath.Join(root, "management.db.credentials.key"), filepath.Join(root, "management.sqlite"), filepath.Join(root, "management.sqlite.credentials.key"), filepath.Join(root, "audit.sqlite")}
	// Retained backups contain the same private data as live metadata. Include
	// their file identities too, so a hard link cannot bypass path exclusion.
	for _, dir := range []string{"databases", "legacy", "oauth"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				paths = append(paths, path)
			}
			return nil
		})
	}
	for _, path := range append([]string(nil), paths...) {
		paths = append(paths, path+"-wal", path+"-shm")
	}
	return paths
}
