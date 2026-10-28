package management

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

// DatabaseCredential is sealed before a source is registered. Only the opaque
// reference goes into versioned configuration and operation audit records.
type DatabaseCredential struct {
	DatabaseID string `json:"-"`
	Ciphertext []byte `json:"-"`
}

func (s *Store) credentialKey(create bool) ([]byte, error) {
	path := s.Path + ".credentials.key"
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && create {
		var count int
		if err := s.DB.QueryRow("SELECT COUNT(*) FROM database_credentials").Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, errors.New("数据库凭据加密密钥缺失，请恢复密钥备份")
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".credential-key-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(file.Name())
		if _, err = file.Write(key); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		// Publish a complete key atomically; another process may win this race.
		if err = os.Link(file.Name(), path); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	info, err = os.Lstat(path)
	if err != nil {
		return nil, errors.New("无法读取数据库凭据加密密钥，请检查服务端密钥文件")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("数据库凭据密钥必须是仅当前用户可读写的普通文件")
	}
	key, err := os.ReadFile(path)
	if err != nil || len(key) != 32 {
		return nil, errors.New("数据库凭据加密密钥无效")
	}
	return key, nil
}

func (s *Store) credentialCipher(create bool) (cipher.AEAD, error) {
	key, err := s.credentialKey(create)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Store) SealDatabaseCredential(id, password string) (DatabaseCredential, error) {
	if id == "" || len(password) == 0 || len(password) > 16384 {
		return DatabaseCredential{}, errors.New("需要数据库密码，最多 16384 字节")
	}
	aead, err := s.credentialCipher(true)
	if err != nil {
		return DatabaseCredential{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return DatabaseCredential{}, err
	}
	return DatabaseCredential{DatabaseID: id, Ciphertext: aead.Seal(nonce, nonce, []byte(password), []byte(id))}, nil
}

func (s *Store) DatabasePassword(ctx context.Context, id string) (string, error) {
	var sealed []byte
	if err := s.DB.QueryRowContext(ctx, "SELECT ciphertext FROM database_credentials WHERE database_id=?", id).Scan(&sealed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("数据库连接密码尚未保存")
		}
		return "", err
	}
	aead, err := s.credentialCipher(false)
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize() {
		return "", errors.New("数据库连接密码密文无效")
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(id))
	if err != nil {
		return "", errors.New("无法解密数据库连接密码，请检查密钥备份")
	}
	return string(plain), nil
}
