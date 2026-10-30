// Package metaschema migrates DataScope-owned metadata only. Business schemas
// are never passed to this package.
package metaschema

import (
	"database/sql"
	"fmt"
	"strings"
)

var Table = map[string]string{"users": "PLATFORM_USER", "sessions": "PLATFORM_SESSION", "user_system_permissions": "PLATFORM_PERMISSION", "database_grants": "PLATFORM_GRANT", "database_credentials": "CONNECTION_SECRET", "records": "CONFIG_RECORD", "record_versions": "CONFIG_VERSION", "outbox_events": "AUDIT_OUTBOX", "schema_migrations": "SCHEMA_VERSION", "business_accounts": "BUSINESS_ACCOUNT", "action_requests": "ACTION_RECEIPT", "clients": "OAUTH_CLIENT", "grants": "OAUTH_GRANT", "audit_events": "AUDIT_EVENT"}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func Migrate(db *sql.DB) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for old, name := range Table {
		var n int
		if e = tx.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name=?", old).Scan(&n); e != nil {
			return e
		}
		if n == 0 {
			continue
		}
		if _, e = tx.Exec("ALTER TABLE " + quote(old) + " RENAME TO " + quote(name)); e != nil {
			return e
		}
		rows, e := tx.Query("PRAGMA table_info(" + quote(name) + ")")
		if e != nil {
			return e
		}
		cols := []string{}
		for rows.Next() {
			var cid, nn, pk int
			var col, typ string
			var def any
			if e = rows.Scan(&cid, &col, &typ, &nn, &def, &pk); e != nil {
				rows.Close()
				return e
			}
			cols = append(cols, col)
		}
		rows.Close()
		for _, col := range cols {
			target := strings.ToUpper(strings.TrimSuffix(col, "_")) + "_"
			if target != col {
				if _, e = tx.Exec("ALTER TABLE " + quote(name) + " RENAME COLUMN " + quote(col) + " TO " + quote(target)); e != nil {
					return fmt.Errorf("%s.%s: %w", name, col, e)
				}
			}
		}
		if name == "PLATFORM_USER" && !contains(cols, "auth_version") {
			if _, e = tx.Exec("ALTER TABLE PLATFORM_USER ADD COLUMN AUTH_VERSION_ INTEGER NOT NULL DEFAULT 0"); e != nil {
				return e
			}
		}
	}
	// Full-text state is derived and is rebuilt after records are moved.
	for _, q := range []string{"DROP TRIGGER IF EXISTS knowledge_search_insert", "DROP TRIGGER IF EXISTS knowledge_search_update", "DROP TABLE IF EXISTS knowledge_search"} {
		if _, e = tx.Exec(q); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func contains(v []string, s string) bool {
	for _, x := range v {
		if strings.EqualFold(strings.TrimSuffix(x, "_"), s) {
			return true
		}
	}
	return false
}
