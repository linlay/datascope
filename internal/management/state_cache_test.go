package management

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"datascope-server/internal/requesttiming"
)

func TestParsedStateCacheFreshnessAndFailures(t *testing.T) {
	s := snapshotSource(t, 2)
	read := func(want string, hit bool) {
		t.Helper()
		ctx, trace := requesttiming.New(WithRequestSnapshot(context.Background()))
		if err := s.withState(ctx, false, func(st *sourceState) error {
			if st.Database["name"] != want {
				t.Fatalf("got %v want %s", st.Database["name"], want)
			}
			st.Database["name"] = "private"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_, _, counts := trace.Snapshot()
		if (counts["config_cache_hit"] == 1) != hit {
			t.Fatal(counts)
		}
		if hit && counts["read_state"] != 0 {
			t.Fatal("cache hit parsed YAML", counts)
		}
	}
	read("original", false)
	read("original", true)
	// A separate Source simulates the API process publishing a new generation.
	other := &Source{Path: s.Path}
	if err := other.withState(context.Background(), true, func(st *sourceState) error { st.Database["name"] = "external"; return nil }); err != nil {
		t.Fatal(err)
	}
	read("external", false)
	read("external", true)
	// Same-size edits with the original timestamp must also be observed.
	path := filepath.Join(s.Path, "database.yml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.ReplaceAll(b, []byte("external"), []byte("modified"))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	read("modified", false)
	// A malformed generation must fail instead of serving the cached policy.
	if err := os.WriteFile(path, []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.withState(WithRequestSnapshot(context.Background()), false, func(*sourceState) error { t.Error("served invalid configuration"); return nil }); err == nil {
		t.Fatal("accepted invalid YAML")
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	read("modified", false)
	if err := os.Remove(filepath.Join(s.Path, "authentication.yml")); err != nil {
		t.Fatal(err)
	}
	if err := s.withState(WithRequestSnapshot(context.Background()), false, func(*sourceState) error { t.Error("served stale authentication"); return nil }); err == nil {
		t.Fatal("accepted missing authentication")
	}
}

func TestParsedStateCacheRecoversPendingPublication(t *testing.T) {
	s := snapshotSource(t, 1)
	ctx := WithRequestSnapshot(context.Background())
	if err := s.withState(ctx, false, func(*sourceState) error { return nil }); err != nil {
		t.Fatal(err)
	}
	st, err := readState(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	st.Database["name"] = "recovered"
	stage, err := stageState(s.Path, st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, s.Path+".next"); err != nil {
		t.Fatal(err)
	}
	if err := s.withState(WithRequestSnapshot(context.Background()), false, func(st *sourceState) error {
		if st.Database["name"] != "recovered" {
			t.Fatal("cached state bypassed recovery")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestParsedStateCacheConcurrentRequests(t *testing.T) {
	s := snapshotSource(t, 3)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			if err := s.withState(WithRequestSnapshot(context.Background()), false, func(st *sourceState) error {
				if st.Database["name"] != "original" {
					t.Error("shared mutable state")
				}
				st.Database["name"] = "private"
				return nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestSourceStateCloneIsDeepAndPreservesIntegers(t *testing.T) {
	st := emptyState()
	st.Database = map[string]any{"nested": []any{map[string]any{"id": int64(9007199254740993)}}}
	st.Credentials["READ"] = "original"
	st.Accounts = []accountState{{BusinessAccount: BusinessAccount{Permissions: []string{"query"}}}}
	st.Grants = []Grant{{Permissions: []string{"query"}}}
	st.Records["knowledge"] = []Record{{Key: "k", Data: "original"}}
	copy := cloneSourceState(st)
	if !reflect.DeepEqual(st, copy) {
		t.Fatal("clone changed values/types")
	}
	copy.Database["nested"].([]any)[0].(map[string]any)["id"] = int64(0)
	copy.Credentials["READ"] = "private"
	copy.Accounts[0].Permissions[0] = "private"
	copy.Grants[0].Permissions[0] = "private"
	copy.Records["knowledge"][0].Data = "private"
	if st.Database["nested"].([]any)[0].(map[string]any)["id"] != int64(9007199254740993) || st.Credentials["READ"] != "original" || st.Accounts[0].Permissions[0] != "query" || st.Grants[0].Permissions[0] != "query" || st.Records["knowledge"][0].Data != "original" {
		t.Fatal("clone aliases original")
	}
}

// Compare the previous one-YAML-load/JSON-copy-per-request implementation with
// validated parsed-state reuse, using the same nine consumers and 160 tables.
func BenchmarkParsedConfigurationCache(b *testing.B) {
	s := snapshotSource(b, 160)
	b.Run("previous_request_snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var raw []byte
			if err := s.withStateUncached(context.Background(), false, func(st *sourceState) error {
				var err error
				raw, err = json.Marshal(st)
				return err
			}); err != nil {
				b.Fatal(err)
			}
			for i := 0; i < 9; i++ {
				var state sourceState
				if err := json.Unmarshal(raw, &state); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("validated_parsed_cache", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ctx := WithRequestSnapshot(context.Background())
			for i := 0; i < 9; i++ {
				if err := s.withState(ctx, false, func(*sourceState) error { return nil }); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
