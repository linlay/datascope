package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDatabaseCredentialsAreSealedAndShareable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "management.sqlite")
	m, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	password := "数据库 p@ss:/?# 空格"
	sealed, err := m.SealDatabaseCredential("source", password)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed.Ciphertext, []byte(password)) {
		t.Fatal("password was not encrypted")
	}
	if err = m.UpdateRecords(ctx, "databases", "database.create", "source", func([]Record) ([]Record, error) { return []Record{{Key: "source", Data: `{"id":"source"}`}}, nil }, sealed); err != nil {
		t.Fatal(err)
	}
	// A separate server process uses the shared management DB and key file.
	other, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	got, err := other.DatabasePassword(ctx, "source")
	if err != nil || got != password {
		t.Fatalf("shared credential could not be read: %v", err)
	}
	info, err := os.Stat(path + ".credentials.key")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("key permissions are not private")
	}
	for _, query := range []string{"SELECT data FROM records", "SELECT data FROM record_versions", "SELECT data FROM outbox_events"} {
		rows, err := m.DB.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw, password) {
				t.Fatal("password leaked into metadata or audit")
			}
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	raw, err := json.Marshal(sealed)
	if err != nil || bytes.Contains(raw, sealed.Ciphertext) || strings.Contains(string(raw), password) {
		t.Fatal("credential was serialized")
	}
	// Moving ciphertext to another source cannot reveal the original password.
	if _, err = m.DB.Exec("INSERT INTO database_credentials VALUES(?,?)", "other", sealed.Ciphertext); err != nil {
		t.Fatal(err)
	}
	if _, err = other.DatabasePassword(ctx, "other"); err == nil {
		t.Fatal("ciphertext was not bound to its source")
	}
	broken := append([]byte(nil), sealed.Ciphertext...)
	broken[len(broken)-1] ^= 1
	if _, err = m.DB.Exec("UPDATE database_credentials SET ciphertext=? WHERE database_id='source'", broken); err != nil {
		t.Fatal(err)
	}
	if _, err = other.DatabasePassword(ctx, "source"); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if err = os.Rename(path+".credentials.key", path+".key-backup"); err != nil {
		t.Fatal(err)
	}
	if _, err = other.SealDatabaseCredential("new", "new-password"); err == nil {
		t.Fatal("missing key was silently replaced")
	}
}

func TestCredentialCreationIsAtomicAndKeyPublicationIsConcurrent(t *testing.T) {
	ctx := context.Background()
	m, err := Open(filepath.Join(t.TempDir(), "management.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.SealDatabaseCredential("source", "test-password")
			failures <- err
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	sealed, err := m.SealDatabaseCredential("source", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	err = m.UpdateRecords(ctx, "databases", "database.create", "source", func([]Record) ([]Record, error) { return nil, errors.New("registration failed") }, sealed)
	if err == nil {
		t.Fatal("failed registration succeeded")
	}
	var count int
	if err = m.DB.QueryRow("SELECT COUNT(*) FROM database_credentials").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed registration left an orphan credential")
	}
	// Fail after credential insertion to check transaction rollback too.
	err = m.UpdateRecords(ctx, "databases", "database.create", "source", func([]Record) ([]Record, error) { return []Record{{Key: "source", Data: "invalid JSON"}}, nil }, sealed)
	if err == nil {
		t.Fatal("invalid record committed")
	}
	if err = m.DB.QueryRow("SELECT COUNT(*) FROM database_credentials").Scan(&count); err != nil || count != 0 {
		t.Fatal("credential survived a rolled-back record write")
	}
}

func TestWriteCredentialsRotateWithoutChangingReadCredentials(t *testing.T) {
	ctx := context.Background()
	m, e := Open(filepath.Join(t.TempDir(), "management.sqlite"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	save := func(action string, c DatabaseCredential) error {
		return m.UpdateRecords(ctx, "databases", action, "source", func([]Record) ([]Record, error) { return []Record{{Key: "source", Data: `{"id":"source"}`}}, nil }, c)
	}
	read, e := m.SealDatabaseCredential("source", "read-password")
	if e != nil {
		t.Fatal(e)
	}
	if e = save("database.create", read); e != nil {
		t.Fatal(e)
	}
	key := WriteCredentialID("source")
	for _, password := range []string{"write-password", "rotated-password"} {
		sealed, e := m.SealDatabaseCredential(key, password)
		if e != nil {
			t.Fatal(e)
		}
		if e = save("database.update", sealed); e != nil {
			t.Fatal(e)
		}
		got, e := m.DatabasePassword(ctx, key)
		if e != nil || got != password {
			t.Fatal(got, e)
		}
	}
	got, e := m.DatabasePassword(ctx, "source")
	if e != nil || got != "read-password" {
		t.Fatal("query credential changed", e)
	}
	foreign, e := m.SealDatabaseCredential(WriteCredentialID("foreign"), "foreign-password")
	if e != nil {
		t.Fatal(e)
	}
	if e = save("database.update", foreign); e == nil {
		t.Fatal("cross-database write credential accepted")
	}
}
