package catalog

import (
	"datascope-server/internal/store"
	"fmt"
	"time"
)

func (d *Database) ValidateTimeouts() error {
	for _, setting := range []struct {
		name  string
		value *int
		max   int
	}{{"query_timeout_seconds", d.QueryTimeoutSeconds, 120}, {"connect_timeout_seconds", d.ConnectTimeoutSeconds, 60}} {
		if setting.value != nil && (*setting.value < 0 || *setting.value > setting.max) {
			return fmt.Errorf("%s 必须为 0–%d 秒；0 表示继承服务默认值", setting.name, setting.max)
		}
	}
	return nil
}

func (c *Catalog) QueryTimeout(d *Database) time.Duration {
	if d.QueryTimeoutSeconds != nil && *d.QueryTimeoutSeconds > 0 {
		return time.Duration(*d.QueryTimeoutSeconds) * time.Second
	}
	if c.Timeout > 0 {
		return c.Timeout
	}
	return store.DefaultQueryTimeout
}

func (c *Catalog) ConnectTimeout(d *Database) time.Duration {
	if d.ConnectTimeoutSeconds != nil && *d.ConnectTimeoutSeconds > 0 {
		return time.Duration(*d.ConnectTimeoutSeconds) * time.Second
	}
	if c.ConnectionTimeout > 0 {
		return c.ConnectionTimeout
	}
	return store.DefaultConnectTimeout
}
