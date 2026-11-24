package audit

import (
	"database/sql/driver"
	"time"

	"modernc.org/sqlite"
)

func eventDuration(start, finish string) (time.Duration, bool) {
	a, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return 0, false
	}
	b, err := time.Parse(time.RFC3339Nano, finish)
	if err != nil || b.Before(a) {
		return 0, false
	}
	return b.Sub(a), true
}

func init() {
	// Parse before subtracting: SQLite julianday loses sub-millisecond precision.
	sqlite.MustRegisterDeterministicScalarFunction("datascope_audit_duration_ms", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		start, _ := args[0].(string)
		finish, _ := args[1].(string)
		elapsed, valid := eventDuration(start, finish)
		if !valid {
			return nil, nil
		}
		return float64(elapsed) / float64(time.Millisecond), nil
	})
}
