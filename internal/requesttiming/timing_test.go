package requesttiming

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrentStagesAndIndependentSnapshots(t *testing.T) {
	Start(context.Background(), "disabled")()
	ctx, trace := New(context.Background())
	Method(ctx, "ping")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() { defer Start(ctx, "read_state")() })
	}
	wg.Wait()
	method, stages, counts := trace.Snapshot()
	if method != "ping" || counts["read_state"] != 20 || stages["read_state"] < 0 {
		t.Fatalf("unexpected trace: %s %v %v", method, stages, counts)
	}
	counts["read_state"] = 0
	_, _, again := trace.Snapshot()
	if again["read_state"] != 20 {
		t.Fatal("snapshot changed trace")
	}
}

func TestToolAndCacheDetails(t *testing.T) {
	ctx, trace := New(context.Background())
	if _, hit := trace.Details(); hit != nil {
		t.Fatal("uninitialized cache reported a hit/miss")
	}
	Tool(ctx, "query")
	QueryCache(ctx, false)
	name, hit := trace.Details()
	if name != "query" || hit == nil || *hit {
		t.Fatal("cold cache details lost")
	}
	*hit = true
	_, hit = trace.Details()
	if *hit {
		t.Fatal("details exposed mutable state")
	}
	QueryCache(ctx, true)
	_, hit = trace.Details()
	if !*hit {
		t.Fatal("warm cache details lost")
	}
}
