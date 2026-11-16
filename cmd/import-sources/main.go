// Command import-sources installs declarative external connections and import
// tasks for an existing target. It never creates business tables or accounts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/datasource"
	"datascope-server/internal/envflag"
	"datascope-server/internal/platform"
	"datascope-server/internal/store"
	"github.com/dop251/goja"
)

type connection struct {
	datasource.Connection
	PasswordEnv string `json:"password_env"`
}
type manifest struct {
	Version     int               `json:"version"`
	DatabaseID  string            `json:"database_id"`
	Connections []connection      `json:"connections"`
	Tasks       []datasource.Task `json:"tasks"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	file := flag.String("file", "", "JSON connections/tasks manifest")
	check := flag.Bool("check", false, "validate scripts and inspect sources without saving configuration")
	replace := flag.Bool("replace", false, "update matching connections/tasks; preserve task checkpoints")
	discover := flag.Bool("discover", false, "refresh target schema; new tables retain deny-by-default policies")
	execute := flag.Bool("run", false, "run each imported task in rollback trial, then commit once")
	flag.Parse()
	if *check && *execute {
		return errors.New("-check and -run cannot be combined")
	}
	f, err := os.Open(*file)
	if err != nil {
		return err
	}
	defer f.Close()
	var b manifest
	decoder := json.NewDecoder(io.LimitReader(f, 8<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&b); err != nil {
		return err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected a single JSON document")
	}
	if b.Version != 1 || b.DatabaseID == "" {
		return errors.New("version: 1 and database_id are required")
	}
	aliases := map[string]bool{}
	ids := map[string]bool{}
	for _, c := range b.Connections {
		if aliases[c.ID] {
			return fmt.Errorf("duplicate source: %s", c.ID)
		}
		aliases[c.ID] = true
	}
	for _, t := range b.Tasks {
		if ids[t.ID] {
			return fmt.Errorf("duplicate task: %s", t.ID)
		}
		ids[t.ID] = true
		if _, err = goja.Compile(t.ID, t.Script, false); err != nil {
			return err
		}
		if _, err = t.Schedule.Next(time.Now()); err != nil {
			return err
		}
		for _, alias := range t.Sources {
			if !aliases[alias] {
				return fmt.Errorf("task %s references missing source %s", t.ID, alias)
			}
		}
	}
	ctx := context.Background()
	m, err := platform.Open(ctx)
	if err != nil {
		return err
	}
	defer m.Close()
	defer m.Audit.Close()
	user, err := m.ByUsername(ctx, envflag.String("ADMIN_USERNAME", "admin"))
	if err != nil || !user.Active || !user.SystemAdmin {
		return errors.New("an existing active platform administrator is required")
	}
	ctx = audit.With(ctx, audit.Metadata{ActorID: user.ID, Source: "source-configuration-import"})
	c := &catalog.Catalog{Management: m, Audit: m.Audit, AsOf: store.DefaultAsOf, Timeout: 30 * time.Second}
	if _, err = c.Get(b.DatabaseID); err != nil {
		return err
	}
	if *discover && !*check {
		if _, err = c.Discover(ctx, b.DatabaseID); err != nil {
			return err
		}
	}
	manager := datasource.New(m, c)
	defer manager.Close()
	existing, err := manager.List(ctx, b.DatabaseID)
	if err != nil {
		return err
	}
	// Inspect all requested sources before saving any configuration.
	for _, entry := range b.Connections {
		source := entry.Connection
		for _, old := range existing.Connections {
			if old.ID == source.ID {
				source.Revision = old.Revision
			}
		}
		password := ""
		if entry.PasswordEnv != "" {
			password = os.Getenv(entry.PasswordEnv)
			if password == "" {
				return fmt.Errorf("missing password environment variable %s", entry.PasswordEnv)
			}
		}
		if _, err = manager.Inspect(ctx, b.DatabaseID, source, password, ""); err != nil {
			return fmt.Errorf("inspect %s: %w", source.ID, err)
		}
	}
	if *check {
		fmt.Printf("checked %d connections and %d scripts for %s\n", len(b.Connections), len(b.Tasks), b.DatabaseID)
		return nil
	}
	for _, entry := range b.Connections {
		source := entry.Connection
		found := false
		for _, old := range existing.Connections {
			if old.ID == source.ID {
				found = true
				source.Revision = old.Revision
			}
		}
		if found && !*replace {
			fmt.Println("preserved connection", source.ID)
			continue
		}
		if _, err = manager.SaveConnection(ctx, b.DatabaseID, source, os.Getenv(entry.PasswordEnv)); err != nil {
			return err
		}
		fmt.Println("saved connection", source.ID)
	}
	for _, task := range b.Tasks {
		found := false
		for _, old := range existing.Tasks {
			if old.ID == task.ID {
				found = true
				task.Revision = old.Revision
			}
		}
		if found && !*replace {
			fmt.Println("preserved task", task.ID)
			continue
		}
		if _, err = manager.SaveTask(ctx, b.DatabaseID, task, user.ID); err != nil {
			return err
		}
		fmt.Println("saved task", task.ID)
	}
	if *execute {
		for _, task := range b.Tasks {
			for _, trial := range []bool{true, false} {
				start, err := manager.Start(ctx, b.DatabaseID, task.ID, user.ID, trial, false, nil)
				if err != nil {
					return err
				}
				deadline := time.Now().Add(3700 * time.Second)
				for {
					result, err := manager.GetRun(ctx, b.DatabaseID, start.ID)
					if err != nil {
						return err
					}
					if result.Status != "running" {
						fmt.Printf("task=%s trigger=%s status=%s read=%d written=%d run=%s\n", task.ID, result.Trigger, result.Status, result.Read, result.Written, result.ID)
						if result.Status != "succeeded" && result.Status != "rolled_back" {
							return fmt.Errorf("task %s: %s", task.ID, result.Error)
						}
						break
					}
					if time.Now().After(deadline) {
						return errors.New("timed out waiting for import")
					}
					time.Sleep(200 * time.Millisecond)
				}
			}
		}
	}
	return nil
}
