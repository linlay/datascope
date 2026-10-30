// Command migrate-state copies legacy metadata and moves source configuration
// into independent databases. Existing legacy files are preserved for rollback.
package main

import (
	"context"
	"datascope-server/internal/audit"
	"datascope-server/internal/management"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func run() error {
	dir := flag.String("data-dir", "data", "directory containing global management and audit databases")
	flag.Parse()
	global := filepath.Join(*dir, "management.db")
	ap := filepath.Join(*dir, "audit.db")
	if e := management.CopyLegacyState(filepath.Join(*dir, "management.sqlite"), global); e != nil {
		return e
	}
	if e := management.CopyLegacyState(filepath.Join(*dir, "audit.sqlite"), ap); e != nil {
		fmt.Fprintln(os.Stderr, "audit snapshot unavailable; business migration continues:", e)
	}
	a, e := audit.Open(ap)
	if e != nil {
		return e
	}
	defer a.Close()
	m, e := management.Open(global, a)
	if e != nil {
		return e
	}
	defer m.Close()
	type source struct {
		ID         string `json:"id"`
		Management string `json:"management"`
		Audit      string `json:"audit"`
		Policies   int    `json:"query_policies"`
		Actions    int    `json:"actions"`
		Accounts   int    `json:"business_accounts"`
		Versions   int    `json:"versions"`
	}
	report := struct {
		Global  string   `json:"global_management"`
		Sources []source `json:"sources"`
	}{m.Path, []source{}}
	rows, e := m.DB.Query("SELECT DATABASE_ID_ FROM SOURCE_REGISTRY ORDER BY DATABASE_ID_")
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		x, e := m.Source(context.Background(), id)
		if e != nil {
			return e
		}
		v := source{ID: id, Management: x.Path, Audit: x.Audit.Path}
		for table, dest := range map[string]*int{"QUERY_POLICY": &v.Policies, "ACTION_DEFINITION": &v.Actions, "BUSINESS_ACCOUNT": &v.Accounts, "CONFIG_VERSION": &v.Versions} {
			if e = x.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(dest); e != nil {
				return e
			}
		}
		var integrity string
		if e = x.DB.QueryRow("PRAGMA quick_check").Scan(&integrity); e != nil || integrity != "ok" {
			return fmt.Errorf("%s 管理库完整性检查失败", id)
		}
		rows, e := x.DB.Query("SELECT PURPOSE_ FROM CONNECTION_CONFIG WHERE PASSWORD_CIPHERTEXT_ IS NOT NULL")
		if e != nil {
			return e
		}
		purposes := []string{}
		for rows.Next() {
			var p string
			if e = rows.Scan(&p); e != nil {
				rows.Close()
				return e
			}
			purposes = append(purposes, p)
		}
		rows.Close()
		for _, purpose := range purposes {
			key := id
			if purpose == "WRITE" {
				key = management.WriteCredentialID(id)
			}
			if _, e = m.DatabasePassword(context.Background(), key); e != nil {
				return fmt.Errorf("%s %s 凭据迁移校验失败: %w", id, purpose, e)
			}
		}
		report.Sources = append(report.Sources, v)
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
