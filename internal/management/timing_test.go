package management

import (
	"context"
	"datascope-server/internal/requesttiming"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLockTimingIncludesCancelledWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	locked, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- fileLock(context.Background(), path, func() error {
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-finished:
		t.Fatalf("cannot acquire initial lock: %v", err)
	}
	defer func() {
		close(release)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ctx, trace := requesttiming.New(ctx)
	err := fileLock(ctx, path, func() error { t.Error("entered contended lock"); return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected error: %v", err)
	}
	_, stages, counts := trace.Snapshot()
	if counts["lock_wait"] != 1 || counts["lock_hold"] != 0 || stages["lock_wait"] <= 0 {
		t.Fatalf("lost cancelled lock wait: %v %v", stages, counts)
	}
}
