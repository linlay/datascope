package catalog

import (
	"context"
	"crypto/sha256"
	"datascope-server/internal/management"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Authentication selects the identity realm for a database MCP. An absent
// value preserves legacy platform bindings until explicitly migrated.
type Authentication struct {
	Type              string `json:"type" yaml:"type"`
	Label             string `json:"label,omitempty" yaml:"label,omitempty"`
	EmployeeDirectory bool   `json:"employee_directory,omitempty" yaml:"employee_directory,omitempty"`
}

func (d Database) LocalAuthentication() bool {
	return d.Authentication != nil && d.Authentication.Type == "local"
}
func (d Database) AuthenticationVersion() string {
	raw, _ := json.Marshal(struct {
		Config   *Authentication
		Revision int64
	}{d.Authentication, d.AuthenticationRevision})
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
}
func (d Database) AuthorizationIssuer(base string) string {
	if d.LocalAuthentication() {
		return strings.TrimRight(base, "/") + "/auth/databases/" + url.PathEscape(d.ID)
	}
	return strings.TrimRight(base, "/")
}
func (d Database) ValidateAuthentication() error {
	if d.Authentication == nil {
		return nil
	}
	if d.Authentication.Type != "local" && d.Authentication.Type != "platform" {
		return fmt.Errorf("认证来源必须为 local（逐库业务账号）或 platform（兼容模式）")
	}
	if len(d.Authentication.Label) > 120 {
		return fmt.Errorf("认证来源名称不能超过 120 字节")
	}
	if d.Authentication.EmployeeDirectory && d.Driver != "sqlite" {
		return fmt.Errorf("员工目录校验仅适用于 SQLite 证券演示库")
	}
	if d.Authentication.EmployeeDirectory && !d.LocalAuthentication() {
		return fmt.Errorf("员工目录校验仅用于 local 认证来源")
	}
	return nil
}
func (c *Catalog) BusinessAccount(ctx context.Context, d *Database, username string) (management.BusinessAccount, error) {
	if !d.LocalAuthentication() {
		return management.BusinessAccount{}, fmt.Errorf("未配置独立业务认证")
	}
	a, err := c.Management.BusinessAccount(ctx, d.ID, username)
	if err != nil || !a.Active {
		return a, management.ErrDenied
	}
	if d.Authentication.EmployeeDirectory {
		svc, e := c.Open(ctx, d)
		if e != nil {
			return a, e
		}
		defer svc.Store.Close()
		users, e := svc.Store.Users(ctx)
		if e != nil {
			return a, e
		}
		for _, u := range users {
			if u.ID == a.BusinessUserID && u.EmpNo == a.Username && u.Status == "active" {
				return a, nil
			}
		}
		return a, management.ErrDenied
	}
	return a, nil
}
