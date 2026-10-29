package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// BusinessAccount belongs to one database's simulated identity provider. It is
// never a platform User and cannot obtain a management session or management grant.
type BusinessAccount struct {
	ID             int64    `json:"id"`
	DatabaseID     string   `json:"database_id"`
	Username       string   `json:"username"`
	Name           string   `json:"name"`
	BusinessUserID int64    `json:"business_user_id"`
	Permissions    []string `json:"permissions"`
	Active         bool     `json:"active"`
	AuthVersion    int64    `json:"revision"`
}

var BusinessPermissions = []string{"query", "knowledge.read", "raw_query", "action"}

func (s *Store) initBusinessAccounts() error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS business_accounts(
 id INTEGER PRIMARY KEY, database_id TEXT NOT NULL, username TEXT NOT NULL,
 name TEXT NOT NULL, business_user_id INTEGER NOT NULL CHECK(business_user_id>0),
 password_hash TEXT NOT NULL, permissions TEXT NOT NULL, active INTEGER NOT NULL DEFAULT 1,
 auth_version INTEGER NOT NULL DEFAULT 1, UNIQUE(database_id,username));`)
	return err
}
func scanBusiness(row interface{ Scan(...any) error }) (BusinessAccount, error) {
	var a BusinessAccount
	var permissions string
	err := row.Scan(&a.ID, &a.DatabaseID, &a.Username, &a.Name, &a.BusinessUserID, &permissions, &a.Active, &a.AuthVersion)
	if err == nil {
		err = json.Unmarshal([]byte(permissions), &a.Permissions)
	}
	return a, err
}

const businessColumns = "id,database_id,username,name,business_user_id,permissions,active,auth_version"

func (s *Store) BusinessAccount(ctx context.Context, database, username string) (BusinessAccount, error) {
	return scanBusiness(s.DB.QueryRowContext(ctx, "SELECT "+businessColumns+" FROM business_accounts WHERE database_id=? AND username=?", database, username))
}
func (s *Store) BusinessAccounts(ctx context.Context, database string) ([]BusinessAccount, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+businessColumns+" FROM business_accounts WHERE database_id=? ORDER BY username", database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BusinessAccount{}
	for rows.Next() {
		a, e := scanBusiness(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) AuthenticateBusiness(ctx context.Context, database, username, password string) (BusinessAccount, error) {
	var a BusinessAccount
	var encoded, permissions string
	err := s.DB.QueryRowContext(ctx, "SELECT "+businessColumns+",password_hash FROM business_accounts WHERE database_id=? AND username=?", database, username).Scan(&a.ID, &a.DatabaseID, &a.Username, &a.Name, &a.BusinessUserID, &permissions, &a.Active, &a.AuthVersion, &encoded)
	if err == nil {
		err = json.Unmarshal([]byte(permissions), &a.Permissions)
	}
	if err != nil || !verify(password, encoded) || !a.Active {
		return BusinessAccount{}, errors.New("工号或密码错误")
	}
	return a, nil
}

// SaveBusinessAccount uses auth_version as an optimistic lock. Every change
// invalidates access tokens, pending authorization codes and refresh tokens.
func (s *Store) SaveBusinessAccount(ctx context.Context, a BusinessAccount, password string, version int64) (BusinessAccount, error) {
	if a.DatabaseID == "" || strings.TrimSpace(a.Username) == "" || len(a.Username) > 128 || a.Name == "" || a.BusinessUserID <= 0 {
		return a, errors.New("需要数据库、账号、姓名和正整数业务身份")
	}
	for _, p := range a.Permissions {
		if !slices.Contains(BusinessPermissions, p) {
			return a, errors.New("业务账号只能获得 query、knowledge.read、raw_query 或 action 能力")
		}
	}
	if password != "" && len(password) < 6 {
		return a, errors.New("密码至少 6 字符")
	}
	if version == 0 && password == "" {
		return a, errors.New("新建业务账号需要初始密码")
	}
	encoded := ""
	var err error
	if password != "" {
		encoded, err = passwordHash(password)
		if err != nil {
			return a, err
		}
	}
	permissions, _ := json.Marshal(a.Permissions)
	// Password material is deliberately excluded from audit details.
	err = s.Change(ctx, "business_account.save", a.DatabaseID, map[string]any{"username": a.Username, "business_user_id": a.BusinessUserID, "permissions": a.Permissions, "active": a.Active}, func(tx *sql.Tx) error {
		if version == 0 {
			_, e := tx.ExecContext(ctx, `INSERT INTO business_accounts(database_id,username,name,business_user_id,password_hash,permissions,active) VALUES(?,?,?,?,?,?,?)`, a.DatabaseID, a.Username, a.Name, a.BusinessUserID, encoded, string(permissions), a.Active)
			return e
		}
		result, e := tx.ExecContext(ctx, `UPDATE business_accounts SET name=?,business_user_id=?,permissions=?,active=?,password_hash=CASE WHEN ?='' THEN password_hash ELSE ? END,auth_version=auth_version+1 WHERE database_id=? AND username=? AND auth_version=?`, a.Name, a.BusinessUserID, string(permissions), a.Active, encoded, encoded, a.DatabaseID, a.Username, version)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e == nil && n != 1 {
			return errors.New("业务账号已变更，请刷新后重试")
		}
		return e
	})
	if err != nil {
		return a, err
	}
	return s.BusinessAccount(ctx, a.DatabaseID, a.Username)
}
