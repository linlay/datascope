// Package audit persists request and SQL execution events independently of business databases.
package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Metadata struct {
	RequestID     string `json:"request_id"`
	ActorID       int64  `json:"actor_id"`
	SubjectID     int64  `json:"subject_id"`
	DatabaseID    string `json:"database_id"`
	Source        string `json:"source"`
	Tool          string `json:"tool"`
	PolicyVersion int    `json:"policy_version"`
}
type Event struct {
	Metadata
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	OriginalSQL  string `json:"original_sql,omitempty"`
	RewrittenSQL string `json:"rewritten_sql,omitempty"`
	ExecutedSQL  string `json:"executed_sql,omitempty"`
	Binds        any    `json:"binds,omitempty"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at,omitempty"`
	Rows         int    `json:"rows"`
	Truncated    bool   `json:"truncated"`
	Detail       any    `json:"detail,omitempty"`
}
type Store struct {
	db   *sql.DB
	Path string
}
type contextKey struct{}

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func With(ctx context.Context, m Metadata) context.Context {
	if m.RequestID == "" {
		m.RequestID = ID()
	}
	return context.WithValue(ctx, contextKey{}, m)
}
func From(ctx context.Context) Metadata { m, _ := ctx.Value(contextKey{}).(Metadata); return m }
func Open(path string) (*Store, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Dir(abs), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"}).String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`CREATE TABLE IF NOT EXISTS audit_events(id TEXT PRIMARY KEY, request_id TEXT NOT NULL, actor_id INTEGER NOT NULL, database_id TEXT NOT NULL, kind TEXT NOT NULL, started_at TEXT NOT NULL, data TEXT NOT NULL); CREATE INDEX IF NOT EXISTS audit_database_time ON audit_events(database_id,started_at); CREATE INDEX IF NOT EXISTS audit_request ON audit_events(request_id); CREATE INDEX IF NOT EXISTS audit_actor_time ON audit_events(actor_id,started_at); CREATE INDEX IF NOT EXISTS audit_kind_time ON audit_events(kind,started_at);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db: db, Path: abs}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Put(ctx context.Context, e Event) error {
	if e.ID == "" {
		e.ID = ID()
	}
	if e.RequestID == "" {
		e.RequestID = e.ID
	}
	if e.StartedAt == "" {
		e.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_events VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, e.ID, e.RequestID, e.ActorID, e.DatabaseID, e.Kind, e.StartedAt, string(b))
	return err
}

// Start is synchronous: callers must not execute SQL if this fails.
func (s *Store) Start(ctx context.Context, kind, original string) (*Event, error) {
	if s == nil {
		return nil, nil
	}
	m := From(ctx)
	if m.RequestID == "" {
		m.RequestID = ID()
	}
	e := &Event{Metadata: m, ID: ID(), Kind: kind, OriginalSQL: original, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	return e, s.Put(ctx, *e)
}
func (s *Store) Finish(e *Event, err error) error {
	if s == nil || e == nil {
		return nil
	}
	e.Status = "success"
	if err != nil {
		e.Status = "failed"
		e.Error = err.Error()
	}
	e.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	var last error
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		last = s.Put(ctx, *e)
		cancel()
		if last == nil {
			return nil
		}
	}
	return fmt.Errorf("审计完成记录写入失败: %w", last)
}
func (s *Store) List(ctx context.Context, database, request string, limit, offset int) ([]Event, error) {
	page, err := s.search(ctx, Filter{DatabaseID: database, RequestID: request, Limit: limit, Offset: offset}, false)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}
func Join(err, other error) error { return errors.Join(err, other) }

type sqlKey struct{}

func WithSQL(ctx context.Context, original, rewritten string) context.Context {
	return context.WithValue(ctx, sqlKey{}, [2]string{original, rewritten})
}
func SQL(ctx context.Context) (string, string) {
	v, _ := ctx.Value(sqlKey{}).([2]string)
	return v[0], v[1]
}
