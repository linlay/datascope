// Package audit asynchronously records events independently of business transactions.
package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"datascope-server/internal/metaschema"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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
type queuedEvent struct {
	event   Event
	raw     []byte
	barrier chan struct{}
}
type Store struct {
	queueMu sync.RWMutex
	db      *sql.DB
	Path    string
	queue   chan queuedEvent
	done    chan struct{}
	once    sync.Once
	Dropped atomic.Int64
	Closed  atomic.Bool
	Source  func(string) *Store
	Sources func() []*Store
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
func From(ctx context.Context) Metadata {
	m, _ := ctx.Value(contextKey{}).(Metadata)
	return m
}
func Open(path string) (*Store, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	s := &Store{Path: abs, queue: make(chan queuedEvent, 2048), done: make(chan struct{})}
	if e = os.MkdirAll(filepath.Dir(abs), 0700); e == nil {
		var f *os.File
		f, e = os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
		if e == nil {
			f.Close()
			s.db, e = sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: "_pragma=busy_timeout(250)&_pragma=journal_mode(WAL)"}).String())
			if e == nil {
				s.db.SetMaxOpenConns(1)
				e = metaschema.Migrate(s.db)
			}
			if e == nil {
				_, e = s.db.Exec("CREATE TABLE IF NOT EXISTS AUDIT_EVENT(ID_ TEXT PRIMARY KEY,REQUEST_ID_ TEXT NOT NULL,ACTOR_ID_ INTEGER NOT NULL,DATABASE_ID_ TEXT NOT NULL,KIND_ TEXT NOT NULL,STARTED_AT_ TEXT NOT NULL,DATA_ TEXT NOT NULL);CREATE INDEX IF NOT EXISTS IX_AUDIT_EVENT_SOURCE_TIME ON AUDIT_EVENT(DATABASE_ID_,STARTED_AT_);CREATE INDEX IF NOT EXISTS IX_AUDIT_EVENT_REQUEST ON AUDIT_EVENT(REQUEST_ID_);CREATE INDEX IF NOT EXISTS IX_AUDIT_EVENT_ACTOR_TIME ON AUDIT_EVENT(ACTOR_ID_,STARTED_AT_);CREATE INDEX IF NOT EXISTS IX_AUDIT_EVENT_KIND_TIME ON AUDIT_EVENT(KIND_,STARTED_AT_);")
			}
		}
	}
	if e != nil {
		slog.Warn("audit storage unavailable; business remains enabled", "error", e)
		if s.db != nil {
			s.db.Close()
			s.db = nil
		}
	}
	go s.run()
	return s, nil
}
func (s *Store) run() {
	defer close(s.done)
	for msg := range s.queue {
		if msg.barrier != nil {
			close(msg.barrier)
			continue
		}
		if s.db == nil {
			s.Dropped.Add(1)
			continue
		}
		e := msg.event
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := s.db.ExecContext(ctx, "INSERT INTO AUDIT_EVENT VALUES(?,?,?,?,?,?,?) ON CONFLICT(ID_) DO UPDATE SET DATA_=excluded.DATA_", e.ID, e.RequestID, e.ActorID, e.DatabaseID, e.Kind, e.StartedAt, string(msg.raw))
		cancel()
		if err != nil {
			s.Dropped.Add(1)
			slog.Warn("audit event dropped", "error", err)
		}
	}
}
func (s *Store) Flush(ctx context.Context) {
	if s == nil {
		return
	}
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()
	if s == nil || s.Closed.Load() {
		return
	}
	ready := make(chan struct{})
	select {
	case s.queue <- queuedEvent{barrier: ready}:
		select {
		case <-ready:
		case <-ctx.Done():
		}
	case <-ctx.Done():
	}
}
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		s.queueMu.Lock()
		s.Closed.Store(true)
		close(s.queue)
		s.queueMu.Unlock()
		<-s.done
		if s.db != nil {
			s.db.Close()
		}
	})
	return nil
}
func (s *Store) Put(ctx context.Context, e Event) error {
	if s == nil {
		return nil
	}
	if e.DatabaseID != "" && s.Source != nil {
		target := s.Source(e.DatabaseID)
		if target != nil && target != s {
			return target.Put(ctx, e)
		}
	}
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()
	if s.Closed.Load() {
		s.Dropped.Add(1)
		return nil
	}
	if e.ID == "" {
		e.ID = ID()
	}
	if e.RequestID == "" {
		e.RequestID = e.ID
	}
	if e.StartedAt == "" {
		e.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(e)
	if err != nil || len(raw) > 2*1024*1024 {
		s.Dropped.Add(1)
		return nil
	}
	select {
	case s.queue <- queuedEvent{event: e, raw: raw}:
	default:
		s.Dropped.Add(1)
	}
	return nil
}

// Start allocates an in-memory event. Audit availability never gates business.
func (s *Store) Start(ctx context.Context, kind, original string) (*Event, error) {
	if s == nil {
		return nil, nil
	}
	m := From(ctx)
	if m.RequestID == "" {
		m.RequestID = ID()
	}
	return &Event{Metadata: m, ID: ID(), Kind: kind, OriginalSQL: original, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
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
	return s.Put(context.Background(), *e)
}
func (s *Store) List(ctx context.Context, database, request string, limit, offset int) ([]Event, error) {
	page, err := s.search(ctx, Filter{DatabaseID: database, RequestID: request, Limit: limit, Offset: offset}, false)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}
func Join(err, other error) error {
	return errors.Join(err, other)
}

type sqlKey struct{}

func WithSQL(ctx context.Context, original, rewritten string) context.Context {
	return context.WithValue(ctx, sqlKey{}, [2]string{original, rewritten})
}
func SQL(ctx context.Context) (string, string) {
	v, _ := ctx.Value(sqlKey{}).([2]string)
	return v[0], v[1]
}

func (s *Store) MoveSource(id string, target *Store) error {
	if s == nil || s.db == nil || target == nil || target.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s.Flush(ctx)
	rows, e := s.db.QueryContext(ctx, "SELECT DATA_ FROM AUDIT_EVENT WHERE DATABASE_ID_=?", id)
	if e != nil {
		return e
	}
	defer rows.Close()
	tx, e := target.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return e
		}
		var event Event
		if e = json.Unmarshal([]byte(raw), &event); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO AUDIT_EVENT VALUES(?,?,?,?,?,?,?) ON CONFLICT(ID_) DO UPDATE SET DATA_=excluded.DATA_", event.ID, event.RequestID, event.ActorID, event.DatabaseID, event.Kind, event.StartedAt, raw); e != nil {
			return e
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	rows.Close()
	if e = tx.Commit(); e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, "DELETE FROM AUDIT_EVENT WHERE DATABASE_ID_=?", id)
	return e
}
