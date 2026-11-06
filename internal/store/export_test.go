package store

import (
	"database/sql/driver"
	"datascope-server/internal/dialect"
	"fmt"
	"io"
	"strings"
)

type FixtureProbe struct {
	Queries       []string
	ColumnReads   int
	BusinessReads []string
}
type fixtureRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *fixtureRows) Columns() []string { return r.columns }
func (r *fixtureRows) Close() error      { return nil }
func (r *fixtureRows) Next(values []driver.Value) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	copy(values, r.values[r.index])
	r.index++
	return nil
}
func fixtureResult(columns []string, values ...[]driver.Value) driver.Rows {
	return &fixtureRows{columns: columns, values: values}
}

// Protocol-level fixture only. Never report this as real Oracle verification.
func NewOracleSchemaFixture(allowed []string) (*Store, *FixtureProbe) {
	probe := &FixtureProbe{}
	conn := &probeConn{}
	conn.onQuery = func(query string, args []driver.NamedValue) (driver.Rows, error) {
		probe.Queries = append(probe.Queries, query)
		switch {
		case strings.Contains(query, "FROM ALL_TABLES"):
			return fixtureResult([]string{"OWNER", "TABLE_NAME"}, []driver.Value{"A", "RECORDS"}, []driver.Value{"B", "RECORDS"}), nil
		case strings.Contains(query, "FROM ALL_TAB_COLUMNS"):
			probe.ColumnReads++
			return fixtureResult([]string{"COLUMN_NAME", "DATA_TYPE", "NULLABLE", "DEFAULT", "COMMENT", "PK"}, []driver.Value{"ID", "NUMBER", "NO", nil, "id", int64(1)}, []driver.Value{"VALUE", "VARCHAR2", "YES", nil, "value", int64(0)}), nil
		case strings.Contains(query, "FROM ALL_INDEXES"):
			return fixtureResult([]string{"INDEX_NAME"}), nil
		case strings.Contains(query, "FROM ALL_CONSTRAINTS"):
			return fixtureResult([]string{"COLUMN_NAME", "TABLE_NAME", "COLUMN_NAME"}), nil
		case strings.Contains(query, "FROM ALL_TAB_COMMENTS"):
			return fixtureResult([]string{"COMMENTS"}, []driver.Value{fmt.Sprint(args[0].Value) + " table"}), nil
		case strings.Contains(query, "WHERE 1=0"):
			return fixtureResult([]string{"ID", "VALUE"}), nil
		default:
			owner := ""
			for _, candidate := range []string{"A", "B"} {
				if strings.Contains(query, `"`+candidate+`"`) {
					owner = candidate
				}
			}
			if owner == "" {
				return nil, fmt.Errorf("unexpected fixture SQL: %s", query)
			}
			probe.BusinessReads = append(probe.BusinessReads, owner)
			if strings.Contains(query, "COUNT(*)") {
				return fixtureResult([]string{"total"}, []driver.Value{int64(1)}), nil
			}
			return fixtureResult([]string{"ID", "VALUE"}, []driver.Value{int64(1), owner + " value"}), nil
		}
	}
	st := probeStore("oracle", conn)
	st.engine = &dialect.Engine{Driver: "oracle", AllowedSchemas: allowed, Tables: map[string]string{}}
	return st, probe
}
