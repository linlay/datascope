package management

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

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

const actionColumns = "DATABASE_ID_,ACTOR_,REQUEST_ID_,FINGERPRINT_,REVISION_,STATE_,DATA_,TOKEN_,EXPIRES_"

func (s *Store) ReserveAction(ctx context.Context, r ActionRequest) (ActionRequest, bool, error) {
	result, e := s.sourceDB(r.DatabaseID).ExecContext(ctx, "INSERT OR IGNORE INTO ACTION_RECEIPT("+actionColumns+`) VALUES(?,?,?,?,?,?,?,?,?)`, r.DatabaseID, r.Actor, r.RequestID, r.Fingerprint, r.Revision, r.State, r.Data, r.Token, r.Expires)
	if e != nil {
		return r, false, e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return r, false, e
	}
	stored, e := scanAction(s.sourceDB(r.DatabaseID).QueryRowContext(ctx, "SELECT "+actionColumns+" FROM ACTION_RECEIPT WHERE DATABASE_ID_=? AND ACTOR_=? AND REQUEST_ID_=?", r.DatabaseID, r.Actor, r.RequestID))
	return stored, n == 1, e
}
func (s *Store) ActionToken(ctx context.Context, token string) (ActionRequest, error) {
	if len(token) != 64 {
		return ActionRequest{}, ErrDenied
	}
	ids, e := s.sourceIDs()
	if e != nil {
		return ActionRequest{}, e
	}
	for _, id := range ids {
		r, e := scanAction(s.sourceDB(id).QueryRowContext(ctx, `SELECT `+actionColumns+` FROM ACTION_RECEIPT WHERE TOKEN_=?`, token))
		if e == nil {
			return r, nil
		}
		if e != sql.ErrNoRows {
			return r, e
		}
	}
	return ActionRequest{}, sql.ErrNoRows
}
func (s *Store) TransitionAction(ctx context.Context, r ActionRequest, from, state, data, token string, expires int64) error {
	result, e := s.sourceDB(r.DatabaseID).ExecContext(ctx, "UPDATE ACTION_RECEIPT SET STATE_=?,DATA_=?,TOKEN_=?,EXPIRES_=? WHERE DATABASE_ID_=? AND ACTOR_=? AND REQUEST_ID_=? AND STATE_=?", state, data, token, expires, r.DatabaseID, r.Actor, r.RequestID, from)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return errors.New("action 请求已被处理或正在执行")
	}
	return e
}

// FinishAction uses an independent context. Unresolved running receipts are
// retained across restarts to prevent a second business commit.
func (s *Store) FinishAction(r ActionRequest, from, state, data string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.TransitionAction(ctx, r, from, state, data, "", 0)
}
