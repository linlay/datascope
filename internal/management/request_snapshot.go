package management

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
)

type requestSnapshotKey struct{}
type authSnapshotKey struct {
	store    *Store
	kind, id string
}

type requestSnapshot struct {
	disabled atomic.Bool
	stateMu  sync.Mutex
	states   map[*Source]*sourceState
	authMu   sync.Mutex
	auth     map[authSnapshotKey][]byte
}

// WithRequestSnapshot scopes configuration reuse to ONE HTTP request. The next
// request must create a new context so revocations and publications remain live.
// Immutable parsed state is private; every consumer receives a deep copy.
func WithRequestSnapshot(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestSnapshotKey{}, &requestSnapshot{
		states: make(map[*Source]*sourceState), auth: make(map[authSnapshotKey][]byte),
	})
}

func snapshotFrom(ctx context.Context) *requestSnapshot {
	r, _ := ctx.Value(requestSnapshotKey{}).(*requestSnapshot)
	return r
}

func invalidateRequestSnapshot(ctx context.Context) {
	if snapshot := snapshotFrom(ctx); snapshot != nil {
		snapshot.disabled.Store(true)
	}
}

func (r *requestSnapshot) read(ctx context.Context, s *Source, fn func(*sourceState) error) error {
	// Release the cache lock before invoking callers: callbacks may themselves
	// load another source or publish configuration.
	raw, err := r.state(ctx, s)
	if err != nil {
		return err
	}
	if raw == nil {
		return s.withStateUncached(ctx, false, fn)
	}
	return fn(cloneSourceState(raw))
}

func (r *requestSnapshot) state(ctx context.Context, s *Source) (*sourceState, error) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.disabled.Load() {
		return nil, nil
	}
	if raw, ok := r.states[s]; ok {
		return raw, nil
	}
	var raw *sourceState
	err := s.withStateMode(ctx, false, true, func(st *sourceState) error {
		raw = st
		return nil
	})
	if err == nil {
		r.states[s] = raw
	}
	return raw, err
}

func (r *requestSnapshot) resolveAuth(ctx context.Context, s *Store, kind, id string) (ResolvedAuth, error) {
	r.authMu.Lock()
	defer r.authMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ResolvedAuth{}, err
	}
	if r.disabled.Load() {
		return s.resolveAuth(ctx, kind, id)
	}
	key := authSnapshotKey{s, kind, id}
	raw, ok := r.auth[key]
	if !ok {
		value, err := s.resolveAuth(ctx, kind, id)
		if err != nil {
			return ResolvedAuth{}, err
		}
		raw, err = json.Marshal(value)
		if err != nil {
			return ResolvedAuth{}, err
		}
		r.auth[key] = raw
	}
	var out ResolvedAuth
	err := json.Unmarshal(raw, &out)
	out.store = s
	return out, err
}
