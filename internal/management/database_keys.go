package management

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DatabaseAuthDir keeps credential material outside management YAML generations
// and inside the owning database's backup boundary.
func (s *Store) DatabaseAuthDir(ctx context.Context, id string) (string, error) {
	source, err := s.Source(ctx, id)
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(source.Path), "auth")
	if err = os.MkdirAll(path, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("数据库认证目录必须是仅当前用户可访问的普通目录")
	}
	return path, nil
}

func (s *Store) SaveDatabaseAPIKey(ctx context.Context, id, token string) error {
	if len(token) < 16 || len(token) > 65536 {
		return errors.New("API Key 长度无效")
	}
	dir, err := s.DatabaseAuthDir(ctx, id)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(token))
	err = fileLock(ctx, filepath.Join(filepath.Dir(dir), ".auth.lock"), func() error {
		file, err := os.CreateTemp(dir, ".api-key-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err = file.WriteString(hex.EncodeToString(digest[:]) + "\n"); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err = os.Rename(file.Name(), filepath.Join(dir, "api-key.sha256")); err != nil {
			return err
		}
		return syncDir(dir)
	})
	if err == nil {
		s.auditChange(ctx, s.AuditFor(id), id, "authentication.api-key.rotate", map[string]string{"storage": "auth/api-key.sha256"})
	}
	return err
}
func (s *Store) GenerateDatabaseAPIKey(ctx context.Context, id string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := "ds_" + base64.RawURLEncoding.EncodeToString(raw)
	if err := s.SaveDatabaseAPIKey(ctx, id, token); err != nil {
		return "", err
	}
	return token, nil
}
func (s *Store) RevokeDatabaseAPIKey(ctx context.Context, id string) error {
	dir, err := s.DatabaseAuthDir(ctx, id)
	if err != nil {
		return err
	}
	err = fileLock(ctx, filepath.Join(filepath.Dir(dir), ".auth.lock"), func() error {
		if err := os.Remove(filepath.Join(dir, "api-key.sha256")); err != nil && !os.IsNotExist(err) {
			return err
		}
		return syncDir(dir)
	})
	if err == nil {
		s.auditChange(ctx, s.AuditFor(id), id, "authentication.api-key.revoke", nil)
	}
	return err
}
func (s *Store) verifyDatabaseAPIKey(ctx context.Context, id, token string) bool {
	if len(token) < 16 || len(token) > 65536 {
		return false
	}
	dir, err := s.DatabaseAuthDir(ctx, id)
	if err != nil {
		return false
	}
	path := filepath.Join(dir, "api-key.sha256")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(want, got[:]) == 1
}

// WithDatabaseAuthLock coordinates first-time signing-key creation across API
// processes without mixing secret files into published management generations.
func (s *Store) WithDatabaseAuthLock(ctx context.Context, id string, fn func(string) error) error {
	dir, err := s.DatabaseAuthDir(ctx, id)
	if err != nil {
		return err
	}
	return fileLock(ctx, filepath.Join(filepath.Dir(dir), ".auth.lock"), func() error { return fn(dir) })
}
