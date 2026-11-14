package store

import (
	"context"
	"testing"
)

func TestImportNetworkSchemaScope(t *testing.T) {
	for _, name := range []string{"mysql", "postgresql", "oracle", "dameng", "oceanbase"} {
		t.Run(name, func(t *testing.T) {
			c := &probeConn{}
			s := probeStore(name, c)
			defer s.Close()
			s.engine.AllowedSchemas = []string{"business"}
			s.engine.Tables = map[string]string{"business.records": "business.records"}
			tx, err := s.BeginImport(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Query(context.Background(), "SELECT id FROM business.records", 1000); err != nil {
				t.Fatal(err)
			}
			before := c.query
			if _, err = tx.Query(context.Background(), "SELECT id FROM private.records", 1000); err == nil {
				t.Fatal("unapproved schema accepted")
			}
			if c.query != before {
				t.Fatal("unapproved query reached driver")
			}
		})
	}
}
