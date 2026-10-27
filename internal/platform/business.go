package platform

import (
	"context"
	"database/sql"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"errors"
	"fmt"
)

// InitializeDemoRealm is a one-time migration for the initial securities demo.
// Other databases get empty independent local realms and explicit accounts.
// It never resets existing business passwords or creates platform users.
func InitializeDemoRealm(ctx context.Context, c *catalog.Catalog, password string) error {
	items, e := c.List()
	if e != nil {
		return e
	}
	var d *catalog.Database
	for i := range items {
		if items[i].ID == "default" {
			d = &items[i]
			break
		}
	}
	if d == nil || d.Driver != "sqlite" {
		return nil
	}
	if d.Authentication != nil {
		return nil
	}
	svc, e := c.Open(ctx, d)
	if e != nil {
		return e
	}
	defer svc.Store.Close()
	// An arbitrary initial database must not inherit the demo employee directory.
	users, e := svc.Store.Users(ctx)
	if e != nil {
		return nil
	}
	if len(password) < 12 {
		return fmt.Errorf("证券 demo 员工认证初始化需要 OAUTH_DEMO_PASSWORD（至少 12 字符）")
	}
	for _, u := range users {
		if _, e = c.Management.BusinessAccount(ctx, d.ID, u.EmpNo); e == nil {
			continue
		} else if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		_, e = c.Management.SaveBusinessAccount(ctx, management.BusinessAccount{DatabaseID: d.ID, Username: u.EmpNo, Name: u.Name, BusinessUserID: u.ID, Active: u.Status == "active", Permissions: []string{"query", "knowledge.read"}}, password, 0)
		if e != nil {
			// The two server processes may initialize concurrently. Only a committed
			// competing insert is safe to accept; never overwrite its credentials.
			if _, readErr := c.Management.BusinessAccount(ctx, d.ID, u.EmpNo); readErr != nil {
				return e
			}
		}
	}
	current, e := c.Get(d.ID)
	if e != nil {
		return e
	}
	if current.Authentication != nil {
		return nil
	}
	current.Authentication = &catalog.Authentication{Type: "local", Label: "证券 demo 员工认证", EmployeeDirectory: true}
	if e = c.Save(ctx, *current); e != nil {
		latest, err := c.Get(d.ID)
		if err == nil && latest.LocalAuthentication() {
			return nil
		}
	}
	return e
}
