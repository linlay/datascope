// Package requesttiming collects opt-in request timings without persisting payloads.
package requesttiming

import (
	"context"
	"sync"
	"time"
)

type key struct{}

// Trace is shared by request contexts, including nested configuration reads.
type Trace struct {
	mu       sync.Mutex
	stages   map[string]float64
	counts   map[string]int
	method   string
	tool     string
	cacheHit *bool
}

func New(ctx context.Context) (context.Context, *Trace) {
	t := &Trace{stages: map[string]float64{}, counts: map[string]int{}}
	return context.WithValue(ctx, key{}, t), t
}

// Start returns a no-op when tracing is disabled. Repeated stages are summed;
// nested stages are intentionally inclusive and must not be added together.
func Start(ctx context.Context, stage string) func() {
	t, _ := ctx.Value(key{}).(*Trace)
	if t == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.stages[stage] += float64(time.Since(start)) / float64(time.Millisecond)
		t.counts[stage]++
	}
}

func Method(ctx context.Context, method string) {
	if t, _ := ctx.Value(key{}).(*Trace); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.method = method
	}
}

func Tool(ctx context.Context, name string) {
	if t, _ := ctx.Value(key{}).(*Trace); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.tool = name
	}
}

func QueryCache(ctx context.Context, hit bool) {
	if t, _ := ctx.Value(key{}).(*Trace); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.cacheHit = &hit
	}
}

// Details returns copies; nil means query-service initialization was not run.
func (t *Trace) Details() (string, *bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cacheHit == nil {
		return t.tool, nil
	}
	hit := *t.cacheHit
	return t.tool, &hit
}

func (t *Trace) Snapshot() (string, map[string]float64, map[string]int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	stages, counts := map[string]float64{}, map[string]int{}
	for k, v := range t.stages {
		stages[k] = v
	}
	for k, v := range t.counts {
		counts[k] = v
	}
	return t.method, stages, counts
}
