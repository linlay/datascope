package management

import (
	"context"
	"database/sql"
	"datascope-server/internal/audit"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func (s *Store) initSources() error {
	_, e := s.DB.Exec(`CREATE TABLE IF NOT EXISTS OAUTH_CLIENT(ID_ TEXT PRIMARY KEY,DATA_ TEXT NOT NULL);CREATE TABLE IF NOT EXISTS SOURCE_REGISTRY(DATABASE_ID_ TEXT PRIMARY KEY,NAME_ TEXT NOT NULL,MCP_PATH_ TEXT NOT NULL UNIQUE,STATE_ TEXT NOT NULL DEFAULT 'ACTIVE');`)
	if e != nil {
		return e
	}
	var n int
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM CONFIG_RECORD WHERE BUCKET_='databases'").Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		if e = s.migrateSources(); e != nil {
			return e
		}
	}
	for _, table := range []string{"PLATFORM_GRANT", "CONNECTION_SECRET", "BUSINESS_ACCOUNT", "ACTION_RECEIPT", "CONFIG_RECORD", "CONFIG_VERSION", "AUDIT_OUTBOX", "PLATFORM_SESSION", "OAUTH_GRANT"} {
		if _, e = s.DB.Exec("DROP TABLE IF EXISTS " + table); e != nil {
			return e
		}
	}
	if s.Audit != nil {
		s.Audit.Source = func(id string) *audit.Store {
			x, e := s.Source(context.Background(), id)
			if e != nil {
				return nil
			}
			return x.Audit
		}
		s.Audit.Sources = func() []*audit.Store {
			ids, _ := s.sourceIDs()
			list := []*audit.Store{}
			for _, id := range ids {
				if x, e := s.Source(context.Background(), id); e == nil {
					list = append(list, x.Audit)
				}
			}
			return list
		}
	}
	return nil
}
func (s *Store) migrateSources() error {
	ctx := context.Background()
	records, e := readRecords(ctx, s.DB, "databases")
	if e != nil {
		return e
	}
	for _, r := range records {
		var d map[string]any
		if e = json.Unmarshal([]byte(r.Data), &d); e != nil {
			return e
		}
		x, e := s.source(ctx, r.Key, true)
		if e != nil {
			return e
		}
		if e = x.saveState(ctx, r.Key, d, nil, false); e != nil {
			return e
		}
		route, _ := d["mcp_path"].(string)
		if route == "" {
			route = r.Key
		}
		if _, e = s.DB.Exec("INSERT OR REPLACE INTO SOURCE_REGISTRY VALUES(?,?,?,'ACTIVE')", r.Key, d["name"], route); e != nil {
			return e
		}
		for _, table := range []string{"PLATFORM_GRANT", "BUSINESS_ACCOUNT"} {
			var exists int
			if e = s.DB.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name=?", table).Scan(&exists); e != nil {
				return e
			}
			if exists == 0 {
				continue
			}
			rows, e := s.DB.Query("SELECT * FROM "+table+" WHERE DATABASE_ID_=?", r.Key)
			if e != nil {
				return e
			}
			cols, _ := rows.Columns()
			for rows.Next() {
				v := make([]any, len(cols))
				p := make([]any, len(cols))
				for i := range v {
					p[i] = &v[i]
				}
				if e = rows.Scan(p...); e != nil {
					rows.Close()
					return e
				}
				q := "INSERT OR REPLACE INTO " + table + " VALUES("
				for i := range v {
					if i > 0 {
						q += ","
					}
					q += "?"
				}
				q += ")"
				if _, e = x.DB.Exec(q, v...); e != nil {
					rows.Close()
					return e
				}
			}
			if e = rows.Err(); e != nil {
				rows.Close()
				return e
			}
			rows.Close()
		}
		for _, bucket := range []string{"knowledge", "discovery_checks", "discovery_pending"} {
			rs, e := readRecords(ctx, s.DB, bucket)
			if e != nil {
				return e
			}
			for _, v := range rs {
				if v.Key != r.Key && !strings.HasPrefix(v.Key, r.Key+"/") {
					continue
				}
				if bucket == "knowledge" {
					data, err := canonicalKnowledgeJSON(v.Data)
					if err != nil {
						return err
					}
					_, e = x.DB.Exec("INSERT OR REPLACE INTO KNOWLEDGE VALUES(?,?)", v.Key, data)
				} else {
					_, e = x.DB.Exec("INSERT OR REPLACE INTO CONFIG_RECORD VALUES(?,?,?)", bucket, v.Key, v.Data)
				}
				if e != nil {
					return e
				}
			}
		}
		versions, e := s.DB.Query("SELECT BUCKET_,KEY_,REVISION_,DATA_,CREATED_AT_ FROM CONFIG_VERSION WHERE KEY_=? OR KEY_ LIKE ?", r.Key, r.Key+"/%")
		if e != nil {
			return e
		}
		for versions.Next() {
			var b, k, data, at string
			var rev int
			versions.Scan(&b, &k, &rev, &data, &at)
			if _, e = x.DB.Exec("INSERT OR IGNORE INTO CONFIG_VERSION VALUES(?,?,?,?,?)", b, k, rev, data, at); e != nil {
				versions.Close()
				return e
			}
		}
		versions.Close()
		for _, key := range []string{r.Key, WriteCredentialID(r.Key)} {
			var encrypted []byte
			if e = s.DB.QueryRow("SELECT CIPHERTEXT_ FROM CONNECTION_SECRET WHERE DATABASE_ID_=?", key).Scan(&encrypted); e == sql.ErrNoRows {
				continue
			} else if e != nil {
				return e
			}
			plain, e := s.openLegacyCredential(key, encrypted)
			if e != nil {
				return e
			}
			sealed, e := s.SealDatabaseCredential(key, plain)
			if e != nil {
				return e
			}
			purpose := "READ"
			if key != r.Key {
				purpose = "WRITE"
			}
			if _, e = x.DB.Exec("UPDATE CONNECTION_CONFIG SET PASSWORD_CIPHERTEXT_=? WHERE PURPOSE_=?", sealed.Ciphertext, purpose); e != nil {
				return e
			}
		}
		if s.Audit != nil {
			if e = s.Audit.MoveSource(r.Key, x.Audit); e != nil {
				slog.Warn("legacy audit migration deferred; original audit retained", "error", e)
			}
		}
	}
	return nil
}

// CopyLegacyState never modifies the old files; publication uses a complete
// SQLite snapshot and keeps the original available for rollback.
func CopyLegacyState(old, new string) error {
	if e := os.MkdirAll(filepath.Dir(new), 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(new+".copy.lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); e != nil {
		return e
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if _, e := os.Stat(new); e == nil {
		return nil
	}
	if _, e := os.Stat(old); os.IsNotExist(e) {
		return nil
	}
	if e := os.MkdirAll(filepath.Dir(new), 0700); e != nil {
		return e
	}
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(old)+"?mode=ro")
	if e != nil {
		return e
	}
	defer db.Close()
	staged := new + fmt.Sprintf(".migrate-%d", time.Now().UnixNano())
	if _, e = db.Exec("VACUUM main INTO ?", staged); e != nil {
		return e
	}
	if e = os.Chmod(staged, 0600); e != nil {
		return e
	}
	if b, e := os.ReadFile(old + ".credentials.key"); e == nil {
		if e = os.WriteFile(new+".credentials.key", b, 0600); e != nil {
			return e
		}
	}
	return os.Rename(staged, new)
}
