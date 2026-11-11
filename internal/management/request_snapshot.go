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
	states   map[*Source][]byte
	authMu   sync.Mutex
	auth     map[authSnapshotKey][]byte
}

// WithRequestSnapshot scopes configuration reuse to ONE HTTP request. The next
// request must create a new context so revocations and publications remain live.
// Snapshot bytes are private; every consumer receives a fresh decoded value.
func WithRequestSnapshot(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestSnapshotKey{}, &requestSnapshot{
		states: make(map[*Source][]byte), auth: make(map[authSnapshotKey][]byte),
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
	var state sourceState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	return fn(&state)
}

func (r *requestSnapshot) state(ctx context.Context, s *Source) ([]byte, error) {
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
	var raw []byte
	err := s.withStateUncached(ctx, false, func(st *sourceState) error {
		var err error
		raw, err = json.Marshal(st)
		return err
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
