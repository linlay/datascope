package oauth

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
)

func (s *Server) databaseKey(ctx context.Context, id string) (*rsa.PrivateKey, error) {
	if s.cfg.Management == nil {
		return nil, errors.New("未配置数据库管理存储")
	}
	var key *rsa.PrivateKey
	err := s.cfg.Management.WithDatabaseAuthLock(ctx, id, func(dir string) error {
		path := filepath.Join(dir, "signing.pem")
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				return errors.New("数据库签名私钥必须是仅当前用户可读写的普通文件")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		var err error
		key, err = loadKey(path)
		if err != nil {
			return err
		}
		raw, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return err
		}
		public := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: raw})
		path = filepath.Join(dir, "public.pem")
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, public) {
			return nil
		}
		file, err := os.CreateTemp(dir, ".public-key-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err = file.Write(public); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Rename(file.Name(), path)
	})
	return key, err
}
