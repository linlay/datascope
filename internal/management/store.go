// Package management owns platform identity, direct grants and versioned configuration.
package management

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"datascope-server/internal/audit"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var ErrDenied = errors.New("无权执行此操作")
var Permissions = []string{"config.read", "config.write", "policy.write", "mcp.write", "knowledge.read", "knowledge.write", "query", "raw_query", "audit.read", "grant", "impersonate"}
var SystemPermissions = []string{"users.manage", "databases.create"}

type Store struct {
	DB           *sql.DB
	Path         string
	Audit        *audit.Store
	AllowedRoots []string
}
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	Active      bool   `json:"active"`
	SystemAdmin bool   `json:"system_admin"`
	AuthVersion int64  `json:"-"`
}
type Grant struct {
	UserID         int64    `json:"user_id"`
	DatabaseID     string   `json:"database_id"`
	Permissions    []string `json:"permissions"`
	BusinessUserID int64    `json:"business_user_id"`
}
type Record struct {
	Key  string
	Data string
}

func Open(path string, a *audit.Store) (*Store, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if a != nil {
		mainInfo, _ := os.Stat(abs)
		auditInfo, _ := os.Stat(a.Path)
		if abs == a.Path || (mainInfo != nil && auditInfo != nil && os.SameFile(mainInfo, auditInfo)) {
			return nil, errors.New("管理库与审计库必须是两个独立的 SQLite 文件")
		}
	}
	if e = os.MkdirAll(filepath.Dir(abs), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	dsn := (&url.URL{Scheme: "file", Path: abs, RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"}).String()
	db, e := sql.Open("sqlite", dsn)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(4)
	_, e = db.Exec(`CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY,username TEXT NOT NULL UNIQUE,name TEXT NOT NULL,password_hash TEXT NOT NULL,active INTEGER NOT NULL DEFAULT 1,system_admin INTEGER NOT NULL DEFAULT 0,auth_version INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS sessions(token_hash TEXT PRIMARY KEY,user_id INTEGER NOT NULL REFERENCES users(id),expires INTEGER NOT NULL,resource TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS user_system_permissions(user_id INTEGER NOT NULL REFERENCES users(id),permission TEXT NOT NULL,PRIMARY KEY(user_id,permission));
 CREATE TABLE IF NOT EXISTS database_grants(user_id INTEGER NOT NULL REFERENCES users(id),database_id TEXT NOT NULL,permissions TEXT NOT NULL,business_user_id INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(user_id,database_id));
 CREATE TABLE IF NOT EXISTS records(bucket TEXT NOT NULL,key TEXT NOT NULL,data TEXT NOT NULL,PRIMARY KEY(bucket,key));
 CREATE TABLE IF NOT EXISTS record_versions(bucket TEXT NOT NULL,key TEXT NOT NULL,revision INTEGER NOT NULL,data TEXT NOT NULL,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,PRIMARY KEY(bucket,key,revision));
 CREATE TABLE IF NOT EXISTS outbox_events(id TEXT PRIMARY KEY,data TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY,applied_at TEXT NOT NULL);
 INSERT OR IGNORE INTO schema_migrations VALUES(1,CURRENT_TIMESTAMP);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	var hasVersion int
	if e = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='auth_version'").Scan(&hasVersion); e != nil {
		db.Close()
		return nil, e
	}
	if hasVersion == 0 {
		if _, e = db.Exec("ALTER TABLE users ADD COLUMN auth_version INTEGER NOT NULL DEFAULT 0"); e != nil {
			db.Close()
			return nil, e
		}
	}
	m := &Store{DB: db, Path: abs, Audit: a}
	if e = m.initBusinessAccounts(); e != nil {
		db.Close()
		return nil, e
	}
	if e = m.initSearch(); e != nil {
		db.Close()
		return nil, e
	}
	return m, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func passwordHash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, 210000, 32)
	if e != nil {
		return "", e
	}
	return base64.RawStdEncoding.EncodeToString(salt) + ":" + base64.RawStdEncoding.EncodeToString(key), nil
}
func verify(password, hash string) bool {
	p := strings.Split(hash, ":")
	if len(p) != 2 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(p[0])
	if e != nil {
		return false
	}
	want, e := base64.RawStdEncoding.DecodeString(p[1])
	if e != nil {
		return false
	}
	got, e := pbkdf2.Key(sha256.New, password, salt, 210000, 32)
	return e == nil && subtle.ConstantTimeCompare(want, got) == 1
}
func tokenHash(token string) string {
	b := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Change commits the mutation and its audit outbox entry in the same transaction.
func (s *Store) Change(ctx context.Context, action, database string, detail any, fn func(*sql.Tx) error) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	ev := audit.Event{Metadata: audit.From(ctx), ID: audit.ID(), Kind: action, Status: "success", StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Detail: detail}
	ev.DatabaseID = database
	raw, e := json.Marshal(ev)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO outbox_events VALUES(?,?)", ev.ID, string(raw)); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Flush(ctx context.Context) error {
	if s.Audit == nil {
		return nil
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT id,data FROM outbox_events LIMIT 200")
	if e != nil {
		return e
	}
	var events []audit.Event
	for rows.Next() {
		var id, raw string
		var ev audit.Event
		if e = rows.Scan(&id, &raw); e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal([]byte(raw), &ev); e != nil {
			rows.Close()
			return e
		}
		events = append(events, ev)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, ev := range events {
		if e = s.Audit.Put(ctx, ev); e != nil {
			return e
		}
		if _, e = s.DB.ExecContext(ctx, "DELETE FROM outbox_events WHERE id=?", ev.ID); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) Bootstrap(ctx context.Context, username, password string) error {
	var n int
	if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	if username == "" || len(password) < 12 {
		return errors.New("首次启动需设置 ADMIN_USERNAME 和至少 12 字符的 ADMIN_PASSWORD")
	}
	hash, e := passwordHash(password)
	if e != nil {
		return e
	}
	return s.Change(ctx, "user.bootstrap", "", map[string]any{"username": username}, func(tx *sql.Tx) error {
		var count int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return nil
		}
		_, e := tx.ExecContext(ctx, "INSERT INTO users(username,name,password_hash,system_admin) VALUES(?,?,?,1)", username, username, hash)
		return e
	})
}
func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	e := row.Scan(&u.ID, &u.Username, &u.Name, &u.Active, &u.SystemAdmin, &u.AuthVersion)
	return u, e
}

const userColumns = "id,username,name,active,system_admin,auth_version"

func (s *Store) User(ctx context.Context, id int64) (User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", id))
}
func (s *Store) ByUsername(ctx context.Context, name string) (User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE username=?", name))
}
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) Authenticate(ctx context.Context, name, password string) (User, error) {
	var hash string
	u, e := s.ByUsername(ctx, name)
	if e == nil {
		e = s.DB.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id=?", u.ID).Scan(&hash)
	}
	if e != nil || !u.Active {
		hash = "c29tZS1kdW1teS1zYWx0LTEyMw:qGxj+NkGIzvZOMz9ogEY5MWeSVvFY3krDkZ93HnTyf0"
	}
	ok := verify(password, hash)
	if e != nil || !u.Active || !ok {
		return User{}, errors.New("用户名或密码错误")
	}
	return u, nil
}
func (s *Store) CreateUser(ctx context.Context, name, display, password string) (User, error) {
	if len(name) == 0 || len(name) > 80 || strings.TrimSpace(name) != name || len(password) < 12 {
		return User{}, errors.New("需要用户名和至少 12 字符的密码")
	}
	hash, e := passwordHash(password)
	if e != nil {
		return User{}, e
	}
	var id int64
	e = s.Change(ctx, "user.create", "", map[string]any{"username": name}, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "INSERT INTO users(username,name,password_hash) VALUES(?,?,?)", name, display, hash)
		if e == nil {
			id, e = r.LastInsertId()
		}
		return e
	})
	if e != nil {
		return User{}, e
	}
	return s.User(ctx, id)
}
func (s *Store) DisableUser(ctx context.Context, id int64) error {
	return s.Change(ctx, "user.disable", "", map[string]any{"user_id": id}, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "UPDATE users SET active=0,auth_version=auth_version+1 WHERE id=? AND system_admin=0", id)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return errors.New("用户不存在，或不能禁用初始化管理员")
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", id)
		return e
	})
}
func (s *Store) ResetPassword(ctx context.Context, id int64, password string) error {
	if len(password) < 12 {
		return errors.New("密码至少 12 字符")
	}
	h, e := passwordHash(password)
	if e != nil {
		return e
	}
	return s.Change(ctx, "user.password.reset", "", map[string]any{"user_id": id}, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "UPDATE users SET password_hash=?,auth_version=auth_version+1 WHERE id=?", h, id)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return errors.New("用户不存在")
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", id)
		return e
	})
}
func (s *Store) NewSession(ctx context.Context, u User, resource string, duration time.Duration) (string, error) {
	token := audit.ID() + audit.ID()
	r, e := s.DB.ExecContext(ctx, "INSERT INTO sessions SELECT ?,id,?,? FROM users WHERE id=? AND active=1 AND auth_version=?", tokenHash(token), time.Now().Add(duration).Unix(), resource, u.ID, u.AuthVersion)
	if e != nil {
		return "", e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return "", e
	}
	if n != 1 {
		return "", ErrDenied
	}
	return token, nil
}
func (s *Store) Session(ctx context.Context, token, resource string) (User, error) {
	var id int64
	e := s.DB.QueryRowContext(ctx, "SELECT user_id FROM sessions WHERE token_hash=? AND resource=? AND expires>?", tokenHash(token), resource, time.Now().Unix()).Scan(&id)
	if e != nil {
		return User{}, ErrDenied
	}
	u, e := s.User(ctx, id)
	if e != nil || !u.Active {
		return User{}, ErrDenied
	}
	return u, nil
}
func (s *Store) Logout(ctx context.Context, token string) error {
	_, e := s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", tokenHash(token))
	return e
}
func (s *Store) Can(ctx context.Context, id int64, database, permission string) bool {
	u, e := s.User(ctx, id)
	if e != nil || !u.Active {
		return false
	}
	if u.SystemAdmin {
		return true
	}
	if database == "" {
		var n int
		return s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_system_permissions WHERE user_id=? AND permission=?", id, permission).Scan(&n) == nil && n > 0
	}
	g, e := s.Grant(ctx, id, database)
	return e == nil && slices.Contains(g.Permissions, permission)
}
func (s *Store) Grant(ctx context.Context, id int64, database string) (Grant, error) {
	g := Grant{UserID: id, DatabaseID: database}
	var raw string
	e := s.DB.QueryRowContext(ctx, "SELECT permissions,business_user_id FROM database_grants WHERE user_id=? AND database_id=?", id, database).Scan(&raw, &g.BusinessUserID)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &g.Permissions)
	}
	return g, e
}
func (s *Store) Grants(ctx context.Context, database string) ([]Grant, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT user_id,permissions,business_user_id FROM database_grants WHERE database_id=? ORDER BY user_id", database)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		g := Grant{DatabaseID: database}
		var raw string
		if e = rows.Scan(&g.UserID, &raw, &g.BusinessUserID); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &g.Permissions); e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Store) SaveGrant(ctx context.Context, actor int64, g Grant) error {
	if !s.Can(ctx, actor, g.DatabaseID, "grant") {
		return ErrDenied
	}
	u, e := s.User(ctx, actor)
	if e != nil {
		return e
	}
	target, e := s.User(ctx, g.UserID)
	if e != nil || !target.Active {
		return errors.New("目标用户不存在或已禁用")
	}
	for _, p := range g.Permissions {
		if !slices.Contains(Permissions, p) {
			return fmt.Errorf("未知权限 %s", p)
		}
		if !u.SystemAdmin && !s.Can(ctx, actor, g.DatabaseID, p) {
			return ErrDenied
		}
	}
	old, _ := s.Grant(ctx, g.UserID, g.DatabaseID)
	if !u.SystemAdmin {
		if old.BusinessUserID != g.BusinessUserID {
			return errors.New("业务身份绑定仅系统管理员可修改")
		}
		for _, p := range old.Permissions {
			if !s.Can(ctx, actor, g.DatabaseID, p) {
				return ErrDenied
			}
		}
	}
	if g.BusinessUserID < 0 {
		return errors.New("业务身份必须为非负编号")
	}
	raw, _ := json.Marshal(g.Permissions)
	return s.Change(ctx, "database.grant", g.DatabaseID, map[string]any{"before": old, "after": g}, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "INSERT INTO database_grants VALUES(?,?,?,?) ON CONFLICT(user_id,database_id) DO UPDATE SET permissions=excluded.permissions,business_user_id=excluded.business_user_id", g.UserID, g.DatabaseID, string(raw), g.BusinessUserID)
		return e
	})
}
func (s *Store) BusinessID(ctx context.Context, id int64, database string) (int64, error) {
	g, e := s.Grant(ctx, id, database)
	if e != nil {
		return 0, errors.New("尚未绑定此数据库的业务身份")
	}
	if g.BusinessUserID <= 0 {
		return 0, errors.New("尚未绑定此数据库的业务身份")
	}
	return g.BusinessUserID, nil
}
func (s *Store) Records(ctx context.Context, bucket string) ([]Record, error) {
	return readRecords(ctx, s.DB, bucket)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readRecords(ctx context.Context, q queryer, bucket string) ([]Record, error) {
	rows, e := q.QueryContext(ctx, "SELECT key,data FROM records WHERE bucket=? ORDER BY key", bucket)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		if e = rows.Scan(&r.Key, &r.Data); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) UpdateRecords(ctx context.Context, bucket, action, database string, fn func([]Record) ([]Record, error)) error {
	detail := map[string]any{"bucket": bucket, "changes": []any{}}
	return s.Change(ctx, action, database, detail, func(tx *sql.Tx) error {
		records, e := readRecords(ctx, tx, bucket)
		if e != nil {
			return e
		}
		updated, e := fn(records)
		if e != nil {
			return e
		}
		for _, r := range updated {
			var old string
			_ = tx.QueryRowContext(ctx, "SELECT data FROM records WHERE bucket=? AND key=?", bucket, r.Key).Scan(&old)
			if old != r.Data {
				before := any(nil)
				if old != "" {
					before = json.RawMessage(old)
				}
				detail["changes"] = append(detail["changes"].([]any), map[string]any{"key": r.Key, "before": before, "after": json.RawMessage(r.Data)})
				if _, e = tx.ExecContext(ctx, "INSERT INTO record_versions(bucket,key,revision,data) SELECT ?,?,COALESCE(MAX(revision),0)+1,? FROM record_versions WHERE bucket=? AND key=?", bucket, r.Key, r.Data, bucket, r.Key); e != nil {
					return e
				}
			}
			if action == "database.create" && old == "" {
				actor := audit.From(ctx).ActorID
				if actor > 0 {
					perms, _ := json.Marshal([]string{"config.read", "config.write", "policy.write", "mcp.write", "knowledge.write", "audit.read", "grant"})
					if _, e = tx.ExecContext(ctx, "INSERT INTO database_grants VALUES(?,?,?,0)", actor, r.Key, string(perms)); e != nil {
						return e
					}
				}
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?) ON CONFLICT(bucket,key) DO UPDATE SET data=excluded.data", bucket, r.Key, r.Data); e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Store) ValidatePath(path string) error {
	real, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	abs, e := filepath.Abs(real)
	if e != nil {
		return e
	}
	for _, p := range []string{s.Path, s.AuditPath()} {
		if p == "" {
			continue
		}
		rp, _ := filepath.EvalSymlinks(p)
		a, _ := os.Stat(abs)
		b, _ := os.Stat(rp)
		if abs == rp || (a != nil && b != nil && os.SameFile(a, b)) {
			return errors.New("内部管理库和审计库不能注册为业务数据库")
		}
	}
	for _, root := range s.AllowedRoots {
		r, e := filepath.EvalSymlinks(root)
		if e != nil {
			continue
		}
		r, _ = filepath.Abs(r)
		rel, e := filepath.Rel(r, abs)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return errors.New("数据库文件不在 DATABASE_ALLOWED_ROOTS 允许的目录内")
}
func (s *Store) AuditPath() string {
	if s.Audit != nil {
		return s.Audit.Path
	}
	return ""
}

func (s *Store) EnableUser(ctx context.Context, id int64) error {
	return s.Change(ctx, "user.enable", "", map[string]any{"user_id": id}, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "UPDATE users SET active=1,auth_version=auth_version+1 WHERE id=?", id)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return errors.New("用户不存在")
		}
		return nil
	})
}
