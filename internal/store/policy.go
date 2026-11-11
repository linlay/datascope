package store

import "time"

// DefaultAsOf is the deterministic reference time used unless configured explicitly.
var DefaultAsOf = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
