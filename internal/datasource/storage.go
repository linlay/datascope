package datasource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
)

type Manager struct {
	Management *management.Store
	Catalog    *catalog.Catalog
	mu         sync.Mutex
	closed     bool
	active     map[string]context.CancelFunc
	wg         sync.WaitGroup
}

func New(m *management.Store, c *catalog.Catalog) *Manager {
	return &Manager{Management: m, Catalog: c, active: map[string]context.CancelFunc{}}
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, cancel := range m.active {
		cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *Manager) directory(id string) string {
	return filepath.Join(filepath.Dir(m.Management.Path), "databases", id, "data-sources")
}
func (m *Manager) open(ctx context.Context, id string) (*sql.DB, error) {
	if _, err := m.Catalog.Get(id); err != nil {
		return nil, err
	}
	dir := m.directory(id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "state.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"}).String())
	if err != nil {
		return nil, err
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS SOURCE_CONNECTION(ID_ TEXT PRIMARY KEY, DATA_ TEXT NOT NULL, SECRET_ BLOB);
 CREATE TABLE IF NOT EXISTS IMPORT_TASK(ID_ TEXT PRIMARY KEY, DATA_ TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS IMPORT_RUN(ID_ TEXT PRIMARY KEY,TASK_ID_ TEXT NOT NULL,DATA_ TEXT NOT NULL,SCRIPT_ TEXT NOT NULL,CANCEL_ INTEGER NOT NULL DEFAULT 0);
 CREATE INDEX IF NOT EXISTS IMPORT_RUN_TASK ON IMPORT_RUN(TASK_ID_);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
func (m *Manager) List(ctx context.Context, id string) (*Overview, error) {
	db, err := m.open(ctx, id)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := &Overview{Connections: []Connection{}, Tasks: []Task{}, Runs: []Run{}}
	read := func(query string, visit func(string) error) error {
		rows, e := db.QueryContext(ctx, query)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if e = rows.Scan(&raw); e != nil {
				return e
			}
			if e = visit(raw); e != nil {
				return e
			}
		}
		return rows.Err()
	}
	if err = read("SELECT DATA_ FROM SOURCE_CONNECTION ORDER BY ID_", func(raw string) error {
		var c Connection
		e := json.Unmarshal([]byte(raw), &c)
		out.Connections = append(out.Connections, c)
		return e
	}); err != nil {
		return nil, err
	}
	if err = read("SELECT DATA_ FROM IMPORT_TASK ORDER BY ID_", func(raw string) error {
		var t Task
		e := json.Unmarshal([]byte(raw), &t)
		out.Tasks = append(out.Tasks, t)
		return e
	}); err != nil {
		return nil, err
	}
	if err = read("SELECT DATA_ FROM IMPORT_RUN ORDER BY rowid DESC LIMIT 100", func(raw string) error {
		var r Run
		e := json.Unmarshal([]byte(raw), &r)
		out.Runs = append(out.Runs, r)
		return e
	}); err != nil {
		return nil, err
	}
	for i := range out.Tasks {
		for j := range out.Runs {
			if out.Runs[j].TaskID == out.Tasks[i].ID {
				out.Tasks[i].LastRun = &out.Runs[j]
				break
			}
		}
	}
	// A free task lock proves no process is still executing the persisted run.
	// A crash near commit has an unknown outcome and must never be auto-replayed.
	for i := range out.Runs {
		r := &out.Runs[i]
		if r.Status != "running" {
			continue
		}
		lock, e := m.lock(id, r.TaskID)
		if e != nil {
			continue
		}
		r.Status = "interrupted"
		r.Error = "执行进程已退出；提交结果可能未知，请核对数据后再运行"
		now := time.Now().UTC()
		r.Finished = &now
		_, e = db.ExecContext(ctx, "UPDATE IMPORT_RUN SET DATA_=? WHERE ID_=?", encode(r), r.ID)
		unlock(lock)
		if e != nil {
			return nil, e
		}
	}
	// Polling the overview must not repeatedly transfer full logs and parameters.
	for i := range out.Runs {
		out.Runs[i].Logs = nil
		out.Runs[i].Parameters = nil
		out.Runs[i].Checkpoint = nil
	}
	return out, nil
}

func (m *Manager) GetRun(ctx context.Context, owner, id string) (*Run, error) {
	db, err := m.open(ctx, owner)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var raw, script string
	if err = db.QueryRowContext(ctx, "SELECT DATA_,SCRIPT_ FROM IMPORT_RUN WHERE ID_=?", id).Scan(&raw, &script); err != nil {
		return nil, err
	}
	var run Run
	if err = json.Unmarshal([]byte(raw), &run); err != nil {
		return nil, err
	}
	run.Script = script
	return &run, nil
}

func (m *Manager) normalize(c Connection) (Connection, error) {
	if !identifier.MatchString(c.ID) || len(c.Name) == 0 || len(c.Name) > 200 {
		return c, errors.New("连接别名或名称无效")
	}
	if c.Driver == "sqlite" {
		if c.Connection != nil {
			return c, errors.New("SQLite 不接受网络配置")
		}
		real, err := filepath.EvalSymlinks(c.Path)
		if err != nil {
			return c, err
		}
		real, err = filepath.Abs(real)
		if err != nil {
			return c, err
		}
		if err = m.Management.ValidatePath(real); err != nil {
			return c, err
		}
		c.Path = real
	} else {
		if c.Path != "" {
			return c, errors.New("网络连接不能配置文件路径")
		}
		c.Connection = store.ConnectionDefaults(c.Driver, c.Connection)
		if c.Connection != nil {
			c.Connection.PasswordEnv = ""
			c.Connection.CredentialID = "external"
		}
	}
	return c, store.ValidateConnection(c.Driver, c.Connection)
}
func (m *Manager) source(ctx context.Context, owner string, c Connection, secret []byte, password string) (*store.Store, error) {
	var err error
	c, err = m.normalize(c)
	if err != nil {
		return nil, err
	}
	if c.Driver == "sqlite" {
		return store.Open(c.Path, 30*time.Second)
	}
	if password == "" {
		password, err = m.Management.OpenDatabaseCredential(owner, secret)
		if err != nil {
			return nil, err
		}
	}
	return store.OpenConnectionWithPassword(ctx, c.Driver, "", c.Connection, password, 30*time.Second)
}
func loadConnection(ctx context.Context, db *sql.DB, id string) (c Connection, secret []byte, err error) {
	var raw string
	err = db.QueryRowContext(ctx, "SELECT DATA_,SECRET_ FROM SOURCE_CONNECTION WHERE ID_=?", id).Scan(&raw, &secret)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &c)
	}
	return
}
func (m *Manager) Inspect(ctx context.Context, owner string, c Connection, password, table string) (any, error) {
	var secret []byte
	db, err := m.open(ctx, owner)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if c.Driver == "" {
		c, secret, err = loadConnection(ctx, db, c.ID)
		if err != nil {
			return nil, err
		}
	}
	if password == "" && c.Driver != "sqlite" && secret == nil {
		_, secret, _ = loadConnection(ctx, db, c.ID)
	}
	st, err := m.source(ctx, owner, c, secret, password)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	if table != "" {
		return st.Describe(ctx, table)
	}
	names, err := st.TableNames(ctx)
	if names == nil {
		names = []string{}
	}
	return map[string]any{"tables": names}, err
}
func (m *Manager) SaveConnection(ctx context.Context, owner string, c Connection, password string) (*Connection, error) {
	var err error
	c, err = m.normalize(c)
	if err != nil {
		return nil, err
	}
	db, err := m.open(ctx, owner)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw string
	var secret []byte
	var old Connection
	err = tx.QueryRowContext(ctx, "SELECT DATA_,SECRET_ FROM SOURCE_CONNECTION WHERE ID_=?", c.ID).Scan(&raw, &secret)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			return nil, err
		}
	}
	if old.Revision != c.Revision {
		return nil, errors.New("连接已变化，请刷新后重试")
	}
	if c.Driver != "sqlite" {
		if password != "" {
			// Do not regenerate a lost key when external sources are the only
			// credentials using this database's key.
			var existing []byte
			e := tx.QueryRowContext(ctx, "SELECT SECRET_ FROM SOURCE_CONNECTION WHERE length(SECRET_)>0 LIMIT 1").Scan(&existing)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			if len(existing) > 0 {
				if _, e = m.Management.OpenDatabaseCredential(owner, existing); e != nil {
					return nil, e
				}
			}
			sealed, e := m.Management.SealDatabaseCredential(owner, password)
			if e != nil {
				return nil, e
			}
			secret = sealed.Ciphertext
		}
		if len(secret) == 0 {
			return nil, errors.New("请填写来源数据库密码")
		}
	} else {
		secret = nil
	}
	c.Revision++
	c.HasPassword = len(secret) > 0
	_, err = tx.ExecContext(ctx, "INSERT INTO SOURCE_CONNECTION VALUES(?,?,?) ON CONFLICT(ID_) DO UPDATE SET DATA_=excluded.DATA_,SECRET_=excluded.SECRET_", c.ID, encode(c), secret)
	if err == nil {
		err = tx.Commit()
	}
	return &c, err
}
func (m *Manager) SaveTask(ctx context.Context, owner string, t Task, actor int64) (*Task, error) {
	if err := t.validate(time.Now()); err != nil {
		return nil, err
	}
	if len(encode(t.Parameters)) > 64<<10 {
		return nil, errors.New("参数最多 64 KiB")
	}
	db, err := m.open(ctx, owner)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw string
	var old Task
	err = tx.QueryRowContext(ctx, "SELECT DATA_ FROM IMPORT_TASK WHERE ID_=?", t.ID).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			return nil, err
		}
	}
	if t.Revision != old.Revision {
		return nil, errors.New("任务已变化，请刷新后重试")
	}
	for _, id := range t.Sources {
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM SOURCE_CONNECTION WHERE ID_=?", id).Scan(&n); err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, fmt.Errorf("来源连接不存在: %s", id)
		}
	}
	t.Revision++
	t.Owner = actor
	t.Checkpoint = old.Checkpoint
	t.LastRun = nil
	_, err = tx.ExecContext(ctx, "INSERT INTO IMPORT_TASK VALUES(?,?) ON CONFLICT(ID_) DO UPDATE SET DATA_=excluded.DATA_", t.ID, encode(t))
	if err == nil {
		err = tx.Commit()
	}
	return &t, err
}
func (m *Manager) Delete(ctx context.Context, owner, id string, connection bool) error {
	if !identifier.MatchString(id) {
		return errors.New("无效 ID")
	}
	db, err := m.open(ctx, owner)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if connection {
		rows, e := tx.QueryContext(ctx, "SELECT DATA_ FROM IMPORT_TASK")
		if e != nil {
			return e
		}
		for rows.Next() {
			var raw string
			var t Task
			if e = rows.Scan(&raw); e == nil {
				e = json.Unmarshal([]byte(raw), &t)
			}
			if e != nil {
				rows.Close()
				return e
			}
			for _, source := range t.Sources {
				if source == id {
					rows.Close()
					return fmt.Errorf("连接正在被任务 %s 使用", t.Name)
				}
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM SOURCE_CONNECTION WHERE ID_=?", id)
	} else {
		lock, e := m.lock(owner, id)
		if e != nil {
			return e
		}
		defer unlock(lock)
		_, err = tx.ExecContext(ctx, "DELETE FROM IMPORT_TASK WHERE ID_=?", id)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}
func (m *Manager) lock(owner, id string) (*os.File, error) {
	if !identifier.MatchString(id) {
		return nil, errors.New("无效任务 ID")
	}
	f, err := os.OpenFile(filepath.Join(m.directory(owner), id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("任务正在运行，请等待完成或停止")
	}
	return f, nil
}
func unlock(f *os.File) { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }
