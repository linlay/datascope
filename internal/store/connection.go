package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"datascope-server/internal/dialect"
	_ "gitee.com/chunanyong/dm"
	mysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	ora "github.com/sijms/go-ora/v2"
)

// Connection contains no credentials. Both server processes resolve PasswordEnv
// locally; secrets never enter database records, history, HTTP or MCP responses.
type Connection struct {
	Host        string `json:"host" yaml:"host"`
	Port        int    `json:"port" yaml:"port"`
	Database    string `json:"database,omitempty" yaml:"database,omitempty"`
	ServiceName string `json:"service_name,omitempty" yaml:"service_name,omitempty"`
	Schema      string `json:"schema" yaml:"schema"`
	Username    string `json:"username" yaml:"username"`
	PasswordEnv string `json:"password_env" yaml:"password_env"`
	TLSMode     string `json:"tls_mode,omitempty" yaml:"tls_mode,omitempty"`
}

var passwordEnvName = regexp.MustCompile(`^DATASCOPE_DB_[A-Z0-9_]+$`)

func ValidateConnection(driver string, c *Connection) error {
	if !SupportedDriver(driver) {
		return fmt.Errorf("不支持数据库驱动 %s", driver)
	}
	if driver == "sqlite" {
		if c != nil {
			return errors.New("SQLite 不接受网络连接配置")
		}
		return nil
	}
	if c == nil {
		return errors.New("需要 connection 网络连接配置")
	}
	if strings.TrimSpace(c.Host) == "" || strings.ContainsAny(c.Host, "/\\?#@\x00\r\n") || c.Port < 1 || c.Port > 65535 || c.Username == "" || c.Schema == "" {
		return errors.New("需要合法 host、port、username 和 schema")
	}
	if !passwordEnvName.MatchString(c.PasswordEnv) {
		return errors.New("password_env 必须为 DATASCOPE_DB_ 开头的大写环境变量名称")
	}
	if driver == "mysql" && (c.Database == "" || c.Database != c.Schema) {
		return errors.New("MySQL database 必须与 schema 相同")
	}
	if driver == "postgresql" && c.Database == "" {
		return errors.New("PostgreSQL 需要 database")
	}
	if driver == "oracle" && c.ServiceName == "" {
		return errors.New("Oracle 需要 service_name")
	}
	switch driver {
	case "mysql":
		if c.TLSMode != "" && c.TLSMode != "verify-full" && c.TLSMode != "disable" {
			return errors.New("MySQL tls_mode 支持 verify-full 或 disable")
		}
	case "postgresql":
		if c.TLSMode != "" && c.TLSMode != "verify-full" && c.TLSMode != "require" && c.TLSMode != "disable" {
			return errors.New("PostgreSQL tls_mode 支持 verify-full、require 或 disable")
		}
	default:
		if c.TLSMode != "" && c.TLSMode != "disable" {
			return errors.New("当前 Oracle/达梦适配器暂不支持 TLS 配置")
		}
	}
	return nil
}

func SupportedDriver(driver string) bool {
	switch driver {
	case "sqlite", "mysql", "postgresql", "oracle", "dameng":
		return true
	}
	return false
}

func OpenConnection(ctx context.Context, driver, path string, c *Connection, timeout time.Duration) (*Store, error) {
	if err := ValidateConnection(driver, c); err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		return Open(path, timeout)
	}
	if path != "" {
		return nil, errors.New("网络数据库不能设置 path")
	}
	password, ok := os.LookupEnv(c.PasswordEnv)
	if !ok || password == "" {
		return nil, fmt.Errorf("服务端未配置密码环境变量 %s", c.PasswordEnv)
	}
	if timeout <= 0 {
		timeout = DefaultQueryTimeout
	}
	driverName, dsn := connectionDSN(driver, *c, password, timeout)
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("%s 连接配置无效", driver)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s 连接失败（请检查地址、凭据和网络）", driver)
	}
	s := &Store{db: db, queryTimeout: timeout, engine: &dialect.Engine{Driver: driver, Schema: c.Schema, Tables: map[string]string{}}}
	// Verify that a read-only session can actually be established. In particular,
	// go-ora does not implement driver.TxOptions.ReadOnly; DM ignores errors
	// from its session-readonly toggle. Both use a checked SQL transaction setup.
	tx, err := s.beginReadOnly(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("%s 无法建立只读事务", driver)
	}
	if err = tx.Rollback(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s 无法结束只读事务", driver)
	}
	if _, err = s.TableNames(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func connectionDSN(driver string, c Connection, password string, timeout time.Duration) (string, string) {
	address := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	switch driver {
	case "mysql":
		cfg := mysql.NewConfig()
		cfg.User = c.Username
		cfg.Passwd = password
		cfg.Net = "tcp"
		cfg.Addr = address
		cfg.DBName = c.Database
		cfg.Timeout = timeout
		cfg.ReadTimeout = timeout
		cfg.WriteTimeout = timeout
		cfg.ParseTime = true
		cfg.TLSConfig = "true"
		if c.TLSMode == "disable" {
			cfg.TLSConfig = "false"
		}
		// Match our parser's literal rules on every pooled connection.
		cfg.Params = map[string]string{"sql_mode": "'ANSI_QUOTES,NO_BACKSLASH_ESCAPES'"}
		return "mysql", cfg.FormatDSN()
	case "postgresql":
		u := url.URL{Scheme: "postgres", User: url.UserPassword(c.Username, password), Host: address, Path: "/" + c.Database}
		q := url.Values{}
		mode := c.TLSMode
		if mode == "" {
			mode = "verify-full"
		}
		q.Set("sslmode", mode)
		q.Set("connect_timeout", strconv.Itoa(max(1, int(timeout.Seconds()))))
		q.Set("default_transaction_read_only", "on")
		q.Set("search_path", "pg_catalog")
		u.RawQuery = q.Encode()
		return "pgx", u.String()
	case "oracle":
		return "oracle", ora.BuildUrl(c.Host, c.Port, c.ServiceName, c.Username, password, map[string]string{"CONNECTION TIMEOUT": strconv.Itoa(max(1, int(timeout.Seconds())))})
	default:
		u := url.URL{Scheme: "dm", User: url.UserPassword(c.Username, password), Host: address}
		q := url.Values{}
		q.Set("schema", c.Schema)
		q.Set("connectTimeout", strconv.FormatInt(timeout.Milliseconds(), 10))
		q.Set("socketTimeout", strconv.Itoa(max(1, int(timeout.Seconds()))))
		q.Set("logLevel", "off")
		u.RawQuery = q.Encode()
		return "dm", u.String()
	}
}

func (s *Store) Driver() string {
	if s.engine == nil {
		return "sqlite"
	}
	return s.engine.Driver
}

func (s *Store) beginReadOnly(ctx context.Context) (*sql.Tx, error) {
	opts := &sql.TxOptions{ReadOnly: true}
	if s.Driver() == "oracle" || s.Driver() == "dameng" {
		opts.ReadOnly = false
	}
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if s.Driver() == "oracle" || s.Driver() == "dameng" {
		query := "SET TRANSACTION READ ONLY"
		if _, err = tx.ExecContext(ctx, query); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	return tx, nil
}

func (s *Store) Prepare(query string, args ...any) (string, []any, error) {
	if s.engine == nil {
		return query, args, nil
	}
	return s.engine.Compile(query, args...)
}

// ValidatePortable is also used while saving policies and configured tools,
// before the query can become visible to clients.
func (s *Store) ValidatePortable(query string, names []string) error {
	args := make([]any, 0, len(names))
	for _, n := range names {
		args = append(args, sql.Named(n, int64(1)))
	}
	_, _, err := s.Prepare(query, args...)
	return err
}
