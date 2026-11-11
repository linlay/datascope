package catalog

import (
	"context"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"fmt"
)

// EditConnection changes maintenance fields only. Credential references and the
// logical database/scope always come from the saved record, never the client.
func (c *Catalog) EditConnection(ctx context.Context, id string, revision int, input *store.Connection, password string, save bool) error {
	d, err := c.Get(id)
	if err != nil {
		return err
	}
	if d.Revision != revision {
		return fmt.Errorf("配置已变更，请刷新后重试")
	}
	if d.Driver == "sqlite" || d.Connection == nil || input == nil {
		return fmt.Errorf("仅网络数据库支持编辑连接")
	}
	next := *d.Connection
	next.Host, next.Port, next.Username, next.TLSMode = input.Host, input.Port, input.Username, input.TLSMode
	d.Connection = &next
	var credentials []management.DatabaseCredential
	if password != "" {
		if c.Management == nil {
			return fmt.Errorf("直接输入密码需要使用管理数据库")
		}
		next.PasswordEnv, next.CredentialID = "", d.ID
		sealed, e := c.Management.SealDatabaseCredential(d.ID, password)
		if e != nil {
			return e
		}
		credentials = append(credentials, sealed)
	}
	if err := store.ValidateConnection(d.Driver, &next); err != nil {
		return err
	}
	var st *store.Store
	if password != "" {
		st, err = store.OpenConnectionWithPassword(ctx, d.Driver, "", &next, password, c.Timeout)
	} else {
		st, err = c.OpenStore(ctx, d)
	}
	if err != nil {
		return err
	}
	defer st.Close()
	// Read physical metadata, not the registered metadata returned to users.
	st.RegisteredSchema = nil
	if d.Discovery != nil {
		for _, table := range d.Discovery.Tables {
			actual, e := st.Describe(ctx, table.Name)
			if e != nil {
				return fmt.Errorf("连接校验失败，无法读取已登记表 %s: %w", table.Name, e)
			}
			for _, col := range table.Columns {
				found := false
				for _, current := range actual.Columns {
					if current.Name == col.Name && current.Type == col.Type && current.NotNull == col.NotNull && current.PrimaryKey == col.PrimaryKey {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("连接校验失败，已登记字段结构不兼容: %s.%s", table.Name, col.Name)
				}
			}
		}
	}
	if d.WriteConnection != nil {
		writer := *d.WriteConnection
		writer.Host, writer.Port, writer.TLSMode = next.Host, next.Port, next.TLSMode
		d.WriteConnection = &writer
		probe := *d
		probe.WriteEnabled = true
		ws, e := c.OpenWriter(ctx, &probe)
		if e != nil {
			return fmt.Errorf("写连接校验失败: %w", e)
		}
		ws.Close()
	}
	if !save {
		return nil
	}
	return c.updateContext(ctx, "database.connection.update", id, func(items *[]Database) error {
		for i, old := range *items {
			if old.ID != id {
				continue
			}
			if old.Revision != d.Revision || old.ETag != d.ETag {
				return fmt.Errorf("配置已被其他窗口修改，请刷新后重试")
			}
			d.Revision++
			(*items)[i] = *d
			return nil
		}
		return fmt.Errorf("数据库不存在")
	}, credentials...)
}
