package sqlitelock

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterLockAcrossProcessesAndFileAliases(t *testing.T) {
	if path := os.Getenv("DATASCOPE_LOCK_TEST_FILE"); path != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		unlock, err := Acquire(ctx, path)
		if err == nil {
			unlock()
			os.Exit(3)
		}
		if ctx.Err() == nil {
			os.Exit(4)
		}
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "database.sqlite")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.sqlite")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	unlock, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestWriterLockAcrossProcessesAndFileAliases$")
	cmd.Env = append(os.Environ(), "DATASCOPE_LOCK_TEST_FILE="+alias)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child did not wait for file lock: %v %s", err, output)
	}
}
