package catalog_test

import (
	"context"
	"sync"
	"testing"

	"datascope-server/internal/service"
)

func TestQueryCacheConcurrentReuseAndRevision(t *testing.T) {
	c, _ := setup(t)
	defer c.CloseQueryCache()
	d := mustGet(t, c, "default")
	ctx := context.Background()
	first, release, hit, err := c.AcquireQuery(ctx, d)
	if err != nil || hit {
		t.Fatalf("first lease: hit=%v err=%v", hit, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc, done, hit, err := c.AcquireQuery(ctx, d)
			if err != nil {
				t.Error(err)
				return
			}
			defer done()
			if svc != first || !hit {
				t.Error("warm query did not reuse service")
			}
			if _, err := svc.Query(ctx, "SELECT SUM(id) AS total FROM records", 1, 1000); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	changed := mustGet(t, c, "default")
	for _, p := range changed.Config.Tables {
		if p.Table == "records" {
			p.MaxRows = 1
		}
	}
	if err := c.Save(ctx, *changed); err != nil {
		t.Fatal(err)
	}
	changed = mustGet(t, c, "default")
	next, done, hit, err := c.AcquireQuery(ctx, changed)
	if err != nil || hit || next == first {
		t.Fatalf("revision reused old service: %v %v", hit, err)
	}
	defer done()
	out, err := next.Query(ctx, "SELECT id FROM records ORDER BY id", 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if out.RowCount != 1 || !out.Truncated || out.EffectiveMaxRows != 1 || out.LimitSource != "table_policy" {
		t.Fatalf("new policy ignored: %+v", out.QueryResult)
	}
	// Retiring a revision must not disrupt its in-flight query.
	if _, err := first.Query(ctx, "SELECT id FROM records", 1, 1000); err != nil {
		t.Fatal(err)
	}
	release()
	release() // idempotent, and closes the retired pool
	if _, err := first.Query(ctx, "SELECT id FROM records", 1, 1000); err == nil {
		t.Fatal("retired pool remained open")
	}
	revoked := mustGet(t, c, "default")
	no := false
	for _, policy := range revoked.Config.Tables {
		if policy.Table == "records" {
			policy.Enabled = &no
		}
	}
	if err := c.Save(ctx, *revoked); err != nil {
		t.Fatal(err)
	}
	revoked = mustGet(t, c, "default")
	denied, releaseDenied, _, err := c.AcquireQuery(ctx, revoked)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseDenied()
	if _, err := denied.Query(ctx, "SELECT id FROM records", 1, 1000); err == nil {
		t.Fatal("cached service retained revoked table permission")
	}
	c.CloseQueryCache()
	if _, err := next.Query(ctx, "SELECT id FROM records", 1, 1000); err != nil {
		t.Fatal("shutdown interrupted active lease", err)
	}
	done()
	if _, _, _, err := c.AcquireQuery(ctx, changed); err == nil {
		t.Fatal("closed cache accepted lease")
	}
}

func TestQueryCacheConcurrentColdStart(t *testing.T) {
	c, _ := setup(t)
	defer c.CloseQueryCache()
	d := mustGet(t, c, "default")
	start := make(chan struct{})
	results := make(chan *service.QueryService, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			svc, release, _, err := c.AcquireQuery(context.Background(), d)
			if err != nil {
				t.Error(err)
				return
			}
			defer release()
			results <- svc
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var first *service.QueryService
	for svc := range results {
		if first == nil {
			first = svc
		}
		if first != svc {
			t.Error("duplicate cold initialization")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := c.AcquireQuery(ctx, d); err == nil {
		t.Fatal("cancelled request acquired service")
	}
}
