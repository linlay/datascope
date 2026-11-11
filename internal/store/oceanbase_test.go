package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func TestOceanBaseDSNAndSchemaTarget(t *testing.T) {
	input := Connection{Host: "::1", Username: "reader@tenant#cluster", Database: "sales", PasswordEnv: "DATASCOPE_DB_OB_PASSWORD"}
	c := ConnectionDefaults("oceanbase", &input)
	if c.Port != 2881 || c.Schema != "sales" || input.Schema != "" {
		t.Fatal(c, input)
	}
	if err := ValidateConnection("oceanbase", c); err != nil {
		t.Fatal(err)
	}
	name, dsn := connectionDSN("oceanbase", *c, "p@ss:/?# 空格", 3*time.Second)
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if name != "mysql" || cfg.User != input.Username || cfg.Passwd != "p@ss:/?# 空格" || cfg.Addr != "[::1]:2881" || cfg.DBName != "sales" || cfg.TLSConfig != "true" || !cfg.ParseTime || cfg.MultiStatements || cfg.Params["sql_mode"] != "'ANSI_QUOTES,NO_BACKSLASH_ESCAPES'" {
		t.Fatalf("unexpected OceanBase connection settings")
	}
	changed := *c
	changed.Schema, changed.Database = "crm", "crm"
	if !SameConnectionTarget("oceanbase", c, &changed) {
		t.Fatal("schema scope cannot change")
	}
	changed.Username = "reader@other#cluster"
	if SameConnectionTarget("oceanbase", c, &changed) {
		t.Fatal("tenant target changed silently")
	}
	changed = *c
	changed.Database = "other"
	if ValidateConnection("oceanbase", &changed) == nil {
		t.Fatal("mismatching database/schema accepted")
	}
}

func TestOceanBaseModeValidation(t *testing.T) {
	for _, mode := range []string{"MYSQL", "mysql", "ORACLE", "", "unknown", "query-error"} {
		t.Run(mode, func(t *testing.T) {
			c := &probeConn{onQuery: func(q string, _ []driver.NamedValue) (driver.Rows, error) {
				if q != "SELECT @@ob_compatibility_mode" {
					t.Fatal(q)
				}
				if mode == "query-error" {
					return nil, errors.New("sensitive server details")
				}
				return fixtureResult([]string{"mode"}, []driver.Value{mode}), nil
			}}
			s := probeStore("oceanbase", c)
			defer s.Close()
			err := validateOceanBaseMode(context.Background(), s.db)
			if (err == nil) != strings.EqualFold(mode, "mysql") {
				t.Fatalf("mode=%q err=%v", mode, err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive") {
				t.Fatal("server details leaked")
			}
		})
	}
}

func TestOceanBaseMetadataAndBrowse(t *testing.T) {
	queries := []string{}
	c := &probeConn{onQuery: func(q string, args []driver.NamedValue) (driver.Rows, error) {
		queries = append(queries, q)
		switch {
		case strings.Contains(q, "TABLE_TYPE='BASE TABLE'"):
			return fixtureResult([]string{"schema", "table"}, []driver.Value{"business", "records"}, []driver.Value{"private", "records"}), nil
		case strings.Contains(q, "information_schema."):
			if len(args) != 2 || args[0].Value != "business" || args[1].Value != "records" {
				t.Fatalf("metadata scope: %v", args)
			}
			switch {
			case strings.Contains(q, "information_schema.COLUMNS"):
				return fixtureResult([]string{"name", "type", "nullable", "default", "comment", "pk"}, []driver.Value{"id", "int", "NO", nil, "identifier", int64(1)}), nil
			case strings.Contains(q, "information_schema.STATISTICS"):
				return fixtureResult([]string{"index"}, []driver.Value{"PRIMARY"}), nil
			case strings.Contains(q, "information_schema.KEY_COLUMN_USAGE"):
				return fixtureResult([]string{"column", "target", "ref"}, []driver.Value{"id", "business.parent", "id"}), nil
			case strings.Contains(q, "TABLE_COMMENT"):
				return fixtureResult([]string{"comment"}, []driver.Value{"records comment"}), nil
			}
		case strings.Contains(q, "WHERE 1=0"):
			if !strings.Contains(q, "`business`.`records`") {
				t.Fatal(q)
			}
			return fixtureResult([]string{"id"}), nil
		case strings.Contains(q, "CAST(`id` AS CHAR) LIKE ?"):
			if len(args) != 1 || args[0].Value != "%a!%%" {
				t.Fatalf("browse bindings: %v", args)
			}
			if strings.Contains(q, "COUNT(*)") {
				return fixtureResult([]string{"total"}, []driver.Value{int64(3)}), nil
			}
			if !strings.Contains(q, "LIMIT 1 OFFSET 1") {
				t.Fatal(q)
			}
			return fixtureResult([]string{"id"}, []driver.Value{int64(2)}), nil
		}
		return nil, errors.New("unexpected fixture query: " + q)
	}}
	s := probeStore("oceanbase", c)
	defer s.Close()
	s.engine.AllowedSchemas = []string{"business"}
	names, err := s.TableNames(context.Background())
	if err != nil || !reflect.DeepEqual(names, []string{"business.records"}) {
		t.Fatal(names, err)
	}
	schema, err := s.Describe(context.Background(), "business.records")
	if err != nil {
		t.Fatal(err)
	}
	if schema.Comment != "records comment" || schema.Columns[0].Comment != "identifier" || !schema.Columns[0].PrimaryKey || schema.Columns[0].References != "business.parent.id" || !reflect.DeepEqual(schema.Indexes, []string{"PRIMARY"}) {
		t.Fatalf("metadata: %+v", schema)
	}
	page, err := s.Browse(context.Background(), BrowseOptions{Table: "business.records", Search: "a%", Limit: 1, Offset: 1})
	if err != nil || page.Total != 3 || len(page.Rows) != 1 {
		t.Fatal(page, err)
	}
	if !c.opts.ReadOnly || !c.rolledBack || s.Driver() != "oceanbase" {
		t.Fatal("identity or read-only transaction lost")
	}
	before := len(queries)
	if _, err = s.Describe(context.Background(), "private.records"); err == nil || len(queries) != before {
		t.Fatal("out-of-scope metadata reached driver")
	}
}
