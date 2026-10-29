package store

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func validConnection(driver string) Connection {
	c := Connection{Host: "127.0.0.1", Port: 5236, Database: "business", ServiceName: "FREEPDB1", Schema: "business", Username: "reader", PasswordEnv: "DATASCOPE_DB_TEST_PASSWORD", TLSMode: "disable"}
	return c
}

func TestConnectionConfiguration(t *testing.T) {
	for _, driver := range []string{"mysql", "postgresql", "oracle", "dameng"} {
		c := validConnection(driver)
		if err := ValidateConnection(driver, &c); err != nil {
			t.Fatal(driver, err)
		}
		bad := c
		bad.PasswordEnv = "ADMIN_PASSWORD"
		if err := ValidateConnection(driver, &bad); err == nil {
			t.Fatal("unrelated environment secret accepted")
		}
		bad = c
		bad.Port = 70000
		if err := ValidateConnection(driver, &bad); err == nil {
			t.Fatal("invalid port accepted")
		}
		bad = c
		bad.TLSMode = "skip-verify"
		if err := ValidateConnection(driver, &bad); err == nil {
			t.Fatal("unsupported TLS accepted")
		}
	}
	if err := ValidateConnection("unknown", nil); err == nil {
		t.Fatal("unknown driver accepted")
	}
	if err := ValidateConnection("sqlite", &Connection{}); err == nil {
		t.Fatal("network settings accepted for SQLite")
	}
}

func TestDSNCredentialsAreEscaped(t *testing.T) {
	password := "p@ss:/?#&=_% 空格"
	for _, driver := range []string{"mysql", "postgresql", "oracle", "dameng"} {
		c := validConnection(driver)
		c.Host = "::1"
		_, dsn := connectionDSN(driver, c, password, time.Second*3)
		if driver == "mysql" {
			cfg, err := mysql.ParseDSN(dsn)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Passwd != password || cfg.MultiStatements {
				t.Fatal("credential/multi-statement mismatch")
			}
			continue
		}
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := u.User.Password()
		if got != password {
			t.Fatalf("%s password was not escaped", driver)
		}
	}
}

func TestMissingSecretFailsBeforeNetwork(t *testing.T) {
	c := validConnection("dameng")
	c.PasswordEnv = "DATASCOPE_DB_MISSING_TEST_PASSWORD"
	_, err := OpenConnection(context.Background(), "dameng", "", &c, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), c.PasswordEnv) {
		t.Fatalf("missing secret: %v", err)
	}
}

func TestDefaultPortsAndCredentialReferences(t *testing.T) {
	for driver, port := range map[string]int{"mysql": 3306, "postgresql": 5432, "oracle": 1521, "dameng": 5236} {
		input := validConnection(driver)
		input.Port = 0
		input.PasswordEnv = ""
		input.CredentialID = "db_auto"
		output := ConnectionDefaults(driver, &input)
		if output.Port != port || input.Port != 0 {
			t.Fatalf("%s default port or request immutability failed", driver)
		}
		if err := ValidateConnection(driver, output); err != nil {
			t.Fatal(err)
		}
		output.Port = 12345
		if ConnectionDefaults(driver, output).Port != 12345 {
			t.Fatal("custom port overwritten")
		}
		output.PasswordEnv = "DATASCOPE_DB_PASSWORD"
		if err := ValidateConnection(driver, output); err == nil {
			t.Fatal("ambiguous credential sources accepted")
		}
	}
}
