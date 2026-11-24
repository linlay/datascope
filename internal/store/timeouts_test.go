package store

import (
	"context"
	"errors"
	mysql "github.com/go-sql-driver/mysql"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestConnectionAndQueryTimeoutsAreIndependent(t *testing.T) {
	for _, driver := range []string{"mysql", "oceanbase", "postgresql", "dameng"} {
		_, dsn := connectionDSN(driver, validConnection(driver), "test-password", 90*time.Second, 4*time.Second)
		if driver == "mysql" || driver == "oceanbase" {
			c, err := mysql.ParseDSN(dsn)
			if err != nil {
				t.Fatal(err)
			}
			if c.Timeout != 4*time.Second || c.ReadTimeout != 90*time.Second || c.WriteTimeout != 90*time.Second {
				t.Fatal("dial and query limits mixed")
			}
			continue
		}
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if driver == "postgresql" && u.Query().Get("connect_timeout") != "4" {
			t.Fatal("PostgreSQL dial timeout")
		}
		if driver == "dameng" && (u.Query().Get("connectTimeout") != "4000" || u.Query().Get("socketTimeout") != "90") {
			t.Fatal("DM timeouts mixed")
		}
	}
}

func TestQueryTimeoutReportsPoolAndExecutionStages(t *testing.T) {
	s := maskingFixture(t)
	s.RegisteredSchema = nil
	s.queryTimeout = 10 * time.Millisecond
	s.db.SetMaxOpenConns(1)
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Query(context.Background(), "SELECT 1", 1)
	conn.Close()
	var timeout *OperationTimeoutError
	if !errors.As(err, &timeout) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(timeout.Stage, "连接") {
		t.Fatalf("pool timeout: %v", err)
	}
	_, err = s.RawQuery(context.Background(), "WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<100000000) SELECT sum(x) FROM n", 1)
	if !errors.As(err, &timeout) || timeout.Stage != "SQL 执行与结果读取" {
		t.Fatalf("SQL timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Query(ctx, "SELECT 1", 1)
	if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("cancellation: %v", err)
	}
}
