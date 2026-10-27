package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"datascope-server/internal/dialect"
)

type probeConnector struct{ conn *probeConn }

func (c probeConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c probeConnector) Driver() driver.Driver                        { return probeDriver{c.conn} }

type probeDriver struct{ conn *probeConn }

func (d probeDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

type probeConn struct {
	opts         driver.TxOptions
	setup, query string
	binds        []driver.NamedValue
	rolledBack   bool
	setupErr     error
}

func (c *probeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *probeConn) Close() error              { return nil }
func (c *probeConn) Begin() (driver.Tx, error) { return c, nil }
func (c *probeConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.opts = opts
	return c, nil
}
func (c *probeConn) Commit() error   { return errors.New("read queries must roll back") }
func (c *probeConn) Rollback() error { c.rolledBack = true; return nil }
func (c *probeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.setup = query
	return driver.RowsAffected(0), c.setupErr
}
func (c *probeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.query = query
	c.binds = args
	return &probeRows{}, nil
}

type probeRows struct{ done bool }

func (r *probeRows) Columns() []string { return []string{"value"} }
func (r *probeRows) Close() error      { return nil }
func (r *probeRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0] = int64(1)
	return nil
}

func probeStore(driverName string, c *probeConn) *Store {
	return &Store{db: sql.OpenDB(probeConnector{c}), queryTimeout: time.Second, engine: &dialect.Engine{Driver: driverName, Schema: "business", Tables: map[string]string{}}}
}

func TestNetworkExecutionUsesNativeBindingsAndReadOnlyTransaction(t *testing.T) {
	for _, driverName := range []string{"mysql", "postgresql", "oracle", "dameng"} {
		t.Run(driverName, func(t *testing.T) {
			c := &probeConn{}
			s := probeStore(driverName, c)
			defer s.Close()
			out, err := s.Query(context.Background(), "SELECT :userId", 1, sql.Named("userId", int64(7)))
			if err != nil {
				t.Fatal(err)
			}
			if out.RowCount != 1 || !c.rolledBack {
				t.Fatal("read transaction was not rolled back")
			}
			if len(c.binds) != 1 || c.binds[0].Value != int64(7) || strings.Contains(c.query, "userId") {
				t.Fatalf("binding mismatch: %s %+v", c.query, c.binds)
			}
			if driverName == "oracle" || driverName == "dameng" {
				if c.opts.ReadOnly || c.setup != "SET TRANSACTION READ ONLY" {
					t.Fatal("explicit read-only SQL was not checked")
				}
			} else if !c.opts.ReadOnly {
				t.Fatal("missing read-only driver option")
			}
		})
	}
}

func TestFailedReadOnlySetupNeverExecutesQuery(t *testing.T) {
	for _, driverName := range []string{"oracle", "dameng"} {
		t.Run(driverName, func(t *testing.T) {
			c := &probeConn{setupErr: errors.New("read-only setup failed")}
			s := probeStore(driverName, c)
			defer s.Close()
			if _, err := s.RawQuery(context.Background(), "SELECT 1", 1); err == nil {
				t.Fatal("read-only setup failure ignored")
			}
			if c.query != "" || !c.rolledBack {
				t.Fatal("query executed or failed transaction leaked")
			}
		})
	}
}

func TestUnsupportedRawFunctionNeverReachesDriver(t *testing.T) {
	c := &probeConn{}
	s := probeStore("postgresql", c)
	defer s.Close()
	if _, err := s.RawQuery(context.Background(), "SELECT nextval('private_sequence')", 1); err == nil {
		t.Fatal("function accepted")
	}
	if c.query != "" {
		t.Fatal("unsupported SQL reached driver")
	}
}
