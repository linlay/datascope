package catalog_test

import (
	"context"
	"datascope-server/internal/catalog"
	"testing"
	"time"
)

func TestDatabaseTimeoutOverridesAndCache(t *testing.T) {
	c, _ := setup(t)
	d := mustGet(t, c, "default")
	ctx := context.Background()
	first, release, _, err := c.AcquireQuery(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	query, connect := 90, 5
	d.QueryTimeoutSeconds = &query
	d.ConnectTimeoutSeconds = &connect
	if err = c.Save(ctx, *d); err != nil {
		t.Fatal(err)
	}
	saved := mustGet(t, c, d.ID)
	if c.QueryTimeout(saved) != 90*time.Second || c.ConnectTimeout(saved) != 5*time.Second {
		t.Fatal("timeouts lost")
	}
	next, done, hit, err := c.AcquireQuery(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if hit || next == first {
		t.Fatal("stale timeout cache reused")
	}
	zero := 0
	saved.QueryTimeoutSeconds = &zero
	saved.ConnectTimeoutSeconds = &zero
	if err = c.Save(ctx, *saved); err != nil {
		t.Fatal(err)
	}
	saved = mustGet(t, c, d.ID)
	c.Timeout = 45 * time.Second
	c.ConnectionTimeout = 7 * time.Second
	if c.QueryTimeout(saved) != 45*time.Second || c.ConnectTimeout(saved) != 7*time.Second {
		t.Fatal("inheritance lost")
	}
	for _, v := range []int{-1, 121} {
		invalid := *saved
		invalid.QueryTimeoutSeconds = &v
		if err = c.Save(ctx, invalid); err == nil {
			t.Fatalf("accepted query timeout %d", v)
		}
	}
	invalid := *saved
	v := 61
	invalid.ConnectTimeoutSeconds = &v
	if err = c.Save(ctx, invalid); err == nil {
		t.Fatal("accepted connection timeout")
	}
}

func TestTimeoutValidation(t *testing.T) {
	for _, v := range []int{0, 1, 120} {
		d := catalog.Database{QueryTimeoutSeconds: &v}
		if err := d.ValidateTimeouts(); err != nil {
			t.Fatal(err)
		}
	}
}
