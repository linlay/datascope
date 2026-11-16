package store

import (
	"context"
	"testing"
	"time"

	"datascope-server/internal/requesttiming"
)

func TestFailedConnectionStillRecordsPingTiming(t *testing.T) {
	ctx, trace := requesttiming.New(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	cancel() // No network connection should be established.
	c := validConnection("mysql")
	st, err := OpenConnectionWithPassword(ctx, "mysql", "", &c, "test-password", time.Second)
	if err == nil {
		st.Close()
		t.Fatal("cancelled connection succeeded")
	}
	_, _, counts := trace.Snapshot()
	if counts["connection_open"] != 1 || counts["connection_ping"] != 1 || counts["connection_readonly"] != 0 || counts["connection_table_names"] != 0 {
		t.Fatalf("failed connection timing incorrect: %v", counts)
	}
}
