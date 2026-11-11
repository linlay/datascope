// Package sqlitelock coordinates DataScope writers across API and MCP processes.
package sqlitelock

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Acquire keys the lock by file identity, including registrations through hard links.
func Acquire(ctx context.Context, path string) (func(), error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	identity := path
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		identity = fmt.Sprintf("%d:%d", st.Dev, st.Ino)
	}
	key := sha256.Sum256([]byte(identity))
	path = filepath.Join(os.TempDir(), fmt.Sprintf("datascope-sqlite-%x.lock", key[:16]))
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
