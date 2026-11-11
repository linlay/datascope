package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"datascope-server/internal/service"
)

// Only query/raw_query may lease these immutable services. Metadata discovery
// and actions retain request-local stores because they can mutate store state.
type queryEntry struct {
	key            [32]byte
	ready          chan struct{}
	svc            *service.QueryService
	refs           int
	retired        bool
	created        time.Time
	timer          *time.Timer
	idleGeneration uint64
}

// AcquireQuery reuses a validated service for the exact configuration snapshot.
// Authentication, tool enablement and business identity are still checked by
// the caller on every request. Release must be called instead of Store.Close.
func (c *Catalog) AcquireQuery(ctx context.Context, d *Database) (*service.QueryService, func(), bool, error) {
	// Ignore discovery telemetry; only executable schema/configuration matters.
	snapshot := *d
	snapshot.ETag = "" // Storage/telemetry changes are not executable configuration.
	if d.Discovery != nil {
		discovery := *d.Discovery
		discovery.CheckedAt, discovery.AttemptedAt = time.Time{}, time.Time{}
		discovery.Reports, discovery.Pending, discovery.Error = nil, nil, ""
		snapshot.Discovery = &discovery
	}
	raw, err := json.Marshal(struct {
		Database *Database
		AsOf     time.Time
		Timeout  time.Duration
	}{&snapshot, c.AsOf, c.Timeout})
	if err != nil {
		return nil, nil, false, err
	}
	key := sha256.Sum256(raw)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		c.queryMu.Lock()
		if c.queryClosed {
			c.queryMu.Unlock()
			return nil, nil, false, errors.New("query cache is closed")
		}
		if c.queryServices == nil {
			c.queryServices = map[string]*queryEntry{}
		}
		e := c.queryServices[d.ID]
		if e != nil && (e.key != key || time.Since(e.created) >= 5*time.Minute) {
			c.retireQueryLocked(d.ID, e)
			e = nil
		}
		if e != nil {
			if e.svc == nil {
				ready := e.ready
				c.queryMu.Unlock()
				select {
				case <-ready:
					continue
				case <-ctx.Done():
					return nil, nil, false, ctx.Err()
				}
			}
			if e.timer != nil {
				e.timer.Stop()
			}
			e.idleGeneration++
			e.refs++
			c.queryMu.Unlock()
			return e.svc, c.queryRelease(d.ID, e), true, nil
		}
		e = &queryEntry{key: key, ready: make(chan struct{}), refs: 1, created: time.Now()}
		c.queryServices[d.ID] = e
		c.queryMu.Unlock()
		// Never hold the catalog/cache lock across network I/O.
		svc, err := c.Open(ctx, &snapshot)
		c.queryMu.Lock()
		e.svc = svc
		if err != nil {
			c.retireQueryLocked(d.ID, e)
			e.refs = 0
		}
		close(e.ready)
		c.queryMu.Unlock()
		if err != nil {
			return nil, nil, false, err
		}
		return svc, c.queryRelease(d.ID, e), false, nil
	}
}

func (c *Catalog) retireQueryLocked(id string, e *queryEntry) {
	if c.queryServices[id] == e {
		delete(c.queryServices, id)
	}
	if e.timer != nil {
		e.timer.Stop()
	}
	e.retired = true
	if e.refs == 0 && e.svc != nil {
		e.svc.Store.Close()
	}
}

func (c *Catalog) queryRelease(id string, e *queryEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			c.queryMu.Lock()
			defer c.queryMu.Unlock()
			e.refs--
			if e.refs != 0 {
				return
			}
			if e.retired {
				e.svc.Store.Close()
				return
			}
			e.idleGeneration++
			generation := e.idleGeneration
			e.timer = time.AfterFunc(time.Minute, func() {
				c.queryMu.Lock()
				defer c.queryMu.Unlock()
				if e.refs == 0 && !e.retired && generation == e.idleGeneration {
					c.retireQueryLocked(id, e)
				}
			})
		})
	}
}

// CloseQueryCache retires idle services now; outstanding leases close on release.
func (c *Catalog) CloseQueryCache() {
	c.queryMu.Lock()
	defer c.queryMu.Unlock()
	c.queryClosed = true
	for id, e := range c.queryServices {
		c.retireQueryLocked(id, e)
	}
}
