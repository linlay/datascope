package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidationColumnsUsesOnlyReadabilityProbe(t *testing.T) {
	for _, name := range []string{"mysql", "oceanbase", "postgresql", "oracle", "dameng"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := &probeConn{onQuery: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				calls++
				if !strings.HasPrefix(query, "SELECT * FROM ") || !strings.HasSuffix(query, " WHERE 1=0") {
					t.Fatalf("unexpected metadata query: %s", query)
				}
				return &probeRows{}, nil
			}}
			s := probeStore(name, c)
			defer s.Close()
			s.engine.Tables = map[string]string{"business.records": "business.records"}
			cols, err := s.ValidationColumns(context.Background(), "business.records")
			if err != nil || !reflect.DeepEqual(cols, []string{"value"}) || calls != 1 || !c.rolledBack {
				t.Fatalf("columns=%v err=%v calls=%d rollback=%v", cols, err, calls, c.rolledBack)
			}
			if name == "oracle" || name == "dameng" {
				if c.setup != "SET TRANSACTION READ ONLY" {
					t.Fatal("missing read-only setup")
				}
			} else if !c.opts.ReadOnly {
				t.Fatal("missing read-only transaction")
			}
			if _, err := s.ValidationColumns(context.Background(), "private.secret"); err == nil || calls != 1 {
				t.Fatal("out-of-scope table reached driver")
			}
			denied := errors.New("SELECT denied")
			c.onQuery = func(string, []driver.NamedValue) (driver.Rows, error) { return nil, denied }
			c.rolledBack = false
			if _, err := s.ValidationColumns(context.Background(), "business.records"); !errors.Is(err, denied) || !c.rolledBack {
				t.Fatalf("readability failure ignored or transaction leaked: %v", err)
			}
		})
	}
}
