package management

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"datascope-server/internal/requesttiming"
)

// configurationDigest is checked under the publication lock on every new
// request. Hash actual bytes, not timestamps: in-place edits, restored mtimes,
// atomic replacements and deletion must all invalidate the parsed state.
func configurationDigest(ctx context.Context, path string) ([32]byte, error) {
	h := sha256.New()
	err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(path, name)
		if err != nil {
			return err
		}
		field := func(b []byte) {
			var size [8]byte
			binary.LittleEndian.PutUint64(size[:], uint64(len(b)))
			h.Write(size[:])
			h.Write(b)
		}
		field([]byte(rel))
		field([]byte(entry.Type().String()))
		if !entry.IsDir() {
			b, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			field(b)
		}
		return nil
	})
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, err
}

// Caller holds the cross-process publication lock and has completed recovery.
// The returned state is immutable and must never be passed to user callbacks.
func (s *Source) cachedState(ctx context.Context) (*sourceState, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	done := requesttiming.Start(ctx, "config_cache_check")
	digest, err := configurationDigest(ctx, s.Path)
	done()
	if err != nil {
		s.stateCache = nil
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Preserve readState's behavior for an absent source directory.
		if _, statErr := os.Stat(s.Path); os.IsNotExist(statErr) {
			return emptyState(), nil
		}
		return nil, err
	}
	if s.stateCache != nil && digest == s.stateDigest {
		requesttiming.Start(ctx, "config_cache_hit")()
		return s.stateCache, nil
	}
	s.stateCache = nil // Failed validation must never fall back to stale access.
	readDone := requesttiming.Start(ctx, "read_state")
	state, err := readState(s.Path)
	readDone()
	if err != nil {
		return nil, err
	}
	// Detect unsupported lock-free edits that raced parsing; do not cache or
	// expose a mixed generation. A subsequent request can retry after the edit.
	done = requesttiming.Start(ctx, "config_cache_check")
	after, err := configurationDigest(ctx, s.Path)
	done()
	if err != nil {
		return nil, err
	}
	if after != digest {
		return nil, errors.New("配置在读取期间发生变化，请重试")
	}
	s.stateCache, s.stateDigest = state, digest
	return state, nil
}

func cloneSourceState(st *sourceState) *sourceState {
	out := *st
	if st.Database != nil {
		out.Database = cloneConfigurationValue(st.Database).(map[string]any)
	}
	out.Credentials = maps.Clone(st.Credentials)
	out.Accounts = slices.Clone(st.Accounts)
	for i := range out.Accounts {
		out.Accounts[i].Permissions = slices.Clone(st.Accounts[i].Permissions)
	}
	out.Grants = slices.Clone(st.Grants)
	for i := range out.Grants {
		out.Grants[i].Permissions = slices.Clone(st.Grants[i].Permissions)
	}
	out.Records = maps.Clone(st.Records)
	for key, records := range st.Records {
		out.Records[key] = slices.Clone(records)
	}
	return &out
}

// readState constructs JSON-compatible dictionaries and arrays. Scalars are
// immutable; preserve their numeric types rather than round-tripping float64.
func cloneConfigurationValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		if x == nil {
			return x
		}
		out := make(map[string]any, len(x))
		for k, value := range x {
			out[k] = cloneConfigurationValue(value)
		}
		return out
	case []any:
		if x == nil {
			return x
		}
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = cloneConfigurationValue(value)
		}
		return out
	default:
		return v
	}
}
