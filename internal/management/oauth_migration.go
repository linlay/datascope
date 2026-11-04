package management

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
)

// ImportOAuth preserves registrations; legacy grants require reauthorization.
func (s *Store) ImportOAuth(ctx context.Context, path string) error {
	abs, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	if abs == s.Path {
		return nil
	}
	if _, e = os.Stat(abs); errors.Is(e, os.ErrNotExist) {
		return nil
	} else if e != nil {
		return e
	}
	var n int
	if e = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM SCHEMA_VERSION WHERE VERSION_=2").Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	old, e := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}).String())
	if e != nil {
		return e
	}
	defer old.Close()
	rows, e := old.QueryContext(ctx, "SELECT id,data FROM clients")
	if e != nil {
		rows, e = old.QueryContext(ctx, "SELECT ID_,DATA_ FROM OAUTH_CLIENT")
	}
	if e != nil {
		return e
	}
	clients := []Record{}
	for rows.Next() {
		var r Record
		if e = rows.Scan(&r.Key, &r.Data); e != nil {
			rows.Close()
			return e
		}
		clients = append(clients, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	return s.Change(ctx, "oauth.import", "", map[string]any{"clients": len(clients), "reauthorization_required": true}, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS OAUTH_CLIENT(ID_ TEXT PRIMARY KEY,DATA_ TEXT NOT NULL);"); e != nil {
			return e
		}
		var done int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM SCHEMA_VERSION WHERE VERSION_=2").Scan(&done); e != nil {
			return e
		}
		if done > 0 {
			return nil
		}
		for _, r := range clients {
			if _, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO OAUTH_CLIENT VALUES(?,?)", r.Key, r.Data); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, "INSERT INTO SCHEMA_VERSION VALUES(2,CURRENT_TIMESTAMP)")
		return e
	})
}
