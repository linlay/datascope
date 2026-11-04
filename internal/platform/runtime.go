// Package platform assembles shared management and audit storage for both server processes.
package platform

import (
	"context"
	"datascope-server/internal/audit"
	"datascope-server/internal/envflag"
	"datascope-server/internal/management"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

func Open(ctx context.Context) (*management.Store, error) {
	auditPath := envflag.String("AUDIT_DB_PATH", "data/audit.db")
	managementPath := envflag.String("MANAGEMENT_DB_PATH", "data/management.db")
	if e := management.CopyLegacyState(filepath.Join(filepath.Dir(managementPath), "management.sqlite"), managementPath); e != nil {
		return nil, e
	}
	if e := management.CopyLegacyState(filepath.Join(filepath.Dir(auditPath), "audit.sqlite"), auditPath); e != nil {
		slog.Warn("legacy audit copy failed; business startup continues", "error", e)
	}
	a, e := audit.Open(auditPath)
	if e != nil {
		return nil, e
	}
	m, e := management.Open(managementPath, a)
	if e != nil {
		a.Close()
		return nil, e
	}
	for _, p := range strings.Split(envflag.String("DATABASE_ALLOWED_ROOTS", "data"), string(filepath.ListSeparator)) {
		if p != "" {
			abs, e := filepath.Abs(p)
			if e != nil {
				m.Close()
				a.Close()
				return nil, e
			}
			m.AllowedRoots = append(m.AllowedRoots, abs)
		}
	}
	if e = m.Bootstrap(ctx, envflag.String("ADMIN_USERNAME", "admin"), envflag.String("ADMIN_PASSWORD", "")); e != nil {
		m.Close()
		a.Close()
		return nil, e
	}
	if e = m.ImportOAuth(ctx, envflag.String("OAUTH_STATE_PATH", "data/oauth/state.sqlite3")); e != nil {
		m.Close()
		a.Close()
		return nil, e
	}
	return m, nil
}
func RunOutbox(ctx context.Context, m *management.Store) {
	flush := func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e := m.Flush(c); e != nil {
			slog.Error("audit outbox delivery failed", "error", e)
		}
	}
	flush()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case <-ticker.C:
			flush()
		}
	}
}
