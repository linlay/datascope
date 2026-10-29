package management

import (
	"context"
	"errors"
	"time"
)

// A running receipt is never reclaimed automatically: the process may have
// committed the business transaction before it could persist its result.
func (s *Store) initActions() error {
	_, e := s.DB.Exec(`CREATE TABLE IF NOT EXISTS action_requests(
 database_id TEXT NOT NULL, actor TEXT NOT NULL, request_id TEXT NOT NULL,
 fingerprint TEXT NOT NULL, revision INTEGER NOT NULL, state TEXT NOT NULL,
 data TEXT NOT NULL, token TEXT NOT NULL DEFAULT '', expires INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(database_id,actor,request_id));
 CREATE UNIQUE INDEX IF NOT EXISTS action_confirmation_token ON action_requests(token) WHERE token<>'';`)
	return e
}

type ActionRequest struct {
	DatabaseID  string `json:"database_id"`
	Actor       string `json:"actor"`
	RequestID   string `json:"request_id"`
	Fingerprint string `json:"fingerprint"`
	Revision    int    `json:"revision"`
	State       string `json:"state"`
	Data        string `json:"data"`
	Token       string `json:"-"`
	Expires     int64  `json:"-"`
}

func scanAction(row interface{ Scan(...any) error }) (r ActionRequest, e error) {
	e = row.Scan(&r.DatabaseID, &r.Actor, &r.RequestID, &r.Fingerprint, &r.Revision, &r.State, &r.Data, &r.Token, &r.Expires)
	return
}

const actionColumns = "database_id,actor,request_id,fingerprint,revision,state,data,token,expires"

func (s *Store) ReserveAction(ctx context.Context, r ActionRequest) (ActionRequest, bool, error) {
	result, e := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO action_requests(`+actionColumns+`) VALUES(?,?,?,?,?,?,?,?,?)`, r.DatabaseID, r.Actor, r.RequestID, r.Fingerprint, r.Revision, r.State, r.Data, r.Token, r.Expires)
	if e != nil {
		return r, false, e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return r, false, e
	}
	stored, e := scanAction(s.DB.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM action_requests WHERE database_id=? AND actor=? AND request_id=?`, r.DatabaseID, r.Actor, r.RequestID))
	return stored, n == 1, e
}
func (s *Store) ActionToken(ctx context.Context, token string) (ActionRequest, error) {
	if len(token) != 64 {
		return ActionRequest{}, ErrDenied
	}
	return scanAction(s.DB.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM action_requests WHERE token=?`, token))
}
func (s *Store) TransitionAction(ctx context.Context, r ActionRequest, from, state, data, token string, expires int64) error {
	result, e := s.DB.ExecContext(ctx, `UPDATE action_requests SET state=?,data=?,token=?,expires=? WHERE database_id=? AND actor=? AND request_id=? AND state=?`, state, data, token, expires, r.DatabaseID, r.Actor, r.RequestID, from)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return errors.New("action 请求已被处理或正在执行")
	}
	return e
}

// Complete with an independent context even if the client disconnected.
func (s *Store) FinishAction(r ActionRequest, from, state, data string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.TransitionAction(ctx, r, from, state, data, "", 0)
}
