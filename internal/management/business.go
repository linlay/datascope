package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// BusinessAccount is a source-local demo identity, separate from platform users.
type BusinessAccount struct {
	ID             int64    `json:"id"`
	DatabaseID     string   `json:"database_id"`
	Username       string   `json:"username"`
	Name           string   `json:"name"`
	BusinessUserID int64    `json:"business_user_id"`
	Permissions    []string `json:"permissions"`

	Active      bool  `json:"active"`
	AuthVersion int64 `json:"revision"`
}

var BusinessPermissions = []string{"query", "knowledge.read", "raw_query", "action"}

func scanBusiness(row interface{ Scan(...any) error }) (BusinessAccount, error) {
	var a BusinessAccount
	var permissions string
	err := row.Scan(&a.ID, &a.DatabaseID, &a.Username, &a.Name, &a.BusinessUserID, &permissions, &a.Active, &a.AuthVersion)
	if err == nil {
		err = json.Unmarshal([]byte(permissions), &a.Permissions)
	}
	return a, err
}

const businessColumns = "ID_,DATABASE_ID_,USERNAME_,NAME_,BUSINESS_USER_ID_,PERMISSIONS_,ACTIVE_,AUTH_VERSION_"

func (s *Store) BusinessAccount(ctx context.Context, database, username string) (BusinessAccount, error) {
	return scanBusiness(s.sourceDB(database).QueryRowContext(ctx, "SELECT "+businessColumns+" FROM BUSINESS_ACCOUNT WHERE DATABASE_ID_=? AND USERNAME_=?", database, username))
}
func (s *Store) BusinessAccounts(ctx context.Context, database string) ([]BusinessAccount, error) {
	rows, err := s.sourceDB(database).QueryContext(ctx, "SELECT "+businessColumns+" FROM BUSINESS_ACCOUNT WHERE DATABASE_ID_=? ORDER BY USERNAME_", database)
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
	err := s.sourceDB(database).QueryRowContext(ctx, "SELECT "+businessColumns+",PASSWORD_HASH_ FROM BUSINESS_ACCOUNT WHERE DATABASE_ID_=? AND USERNAME_=?", database, username).Scan(&a.ID, &a.DatabaseID, &a.Username, &a.Name, &a.BusinessUserID, &permissions, &a.Active, &a.AuthVersion, &encoded)
	if err == nil {
		err = json.Unmarshal([]byte(permissions), &a.Permissions)
	}
	if err != nil || !verify(password, encoded) || !a.Active {
		return BusinessAccount{}, errors.New("工号或密码错误")
	}
	return a, nil
}

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
	err = s.sourceChange(ctx, a.DatabaseID, "business_account.save", map[string]any{"username": a.Username, "business_user_id": a.BusinessUserID, "permissions": a.Permissions, "active": a.Active}, func(tx *sql.Tx) error {
		if version == 0 {
			_, e := tx.ExecContext(ctx, "INSERT INTO BUSINESS_ACCOUNT(DATABASE_ID_,USERNAME_,NAME_,BUSINESS_USER_ID_,PASSWORD_HASH_,PERMISSIONS_,ACTIVE_) VALUES(?,?,?,?,?,?,?)", a.DatabaseID, a.Username, a.Name, a.BusinessUserID, encoded, string(permissions), a.Active)
			return e
		}
		result, e := tx.ExecContext(ctx, "UPDATE BUSINESS_ACCOUNT SET NAME_=?,BUSINESS_USER_ID_=?,PERMISSIONS_=?,ACTIVE_=?,PASSWORD_HASH_=CASE WHEN ?='' THEN PASSWORD_HASH_ ELSE ? END,AUTH_VERSION_=AUTH_VERSION_+1 WHERE DATABASE_ID_=? AND USERNAME_=? AND AUTH_VERSION_=?", a.Name, a.BusinessUserID, string(permissions), a.Active, encoded, encoded, a.DatabaseID, a.Username, version)
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

func (s *Store) BusinessAccountID(ctx context.Context, database string, id int64) (BusinessAccount, error) {
	return scanBusiness(s.sourceDB(database).QueryRowContext(ctx, "SELECT "+businessColumns+" FROM BUSINESS_ACCOUNT WHERE DATABASE_ID_=? AND ID_=?", database, id))
}
