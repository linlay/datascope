package management

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"datascope-server/internal/requesttiming"
)

func snapshotSource(t testing.TB, tables int) *Source {
	t.Helper()
	s := &Source{Path: filepath.Join(t.TempDir(), "management")}
	st := emptyState()
	policies := []any{}
	for i := 0; i < tables; i++ {
		policies = append(policies, map[string]any{"table": fmt.Sprintf("table_%03d", i), "scheme": "NONE", "passthrough": true})
	}
	st.Database = map[string]any{"id": "source", "name": "original", "config": map[string]any{"tables": policies}}
	if err := publishState(s.Path, st); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRequestSnapshotIsolationFreshnessAndWrites(t *testing.T) {
	s := snapshotSource(t, 2)
	ctx, trace := requesttiming.New(WithRequestSnapshot(context.Background()))
	read := func(ctx context.Context, want string) {
		t.Helper()
		if err := s.withState(ctx, false, func(st *sourceState) error {
			if st.Database["name"] != want {
				t.Errorf("got %v, want %s", st.Database["name"], want)
			}
			st.Database["name"] = "caller mutation"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 9; i++ {
		read(ctx, "original")
	}
	_, _, counts := trace.Snapshot()
	if counts["read_state"] != 1 {
		t.Fatalf("repeated disk reads: %v", counts)
	}
	// Another process/request publishes: an in-flight request keeps its view;
	// the very next request must observe the new state, with no TTL delay.
	if err := s.withState(context.Background(), true, func(st *sourceState) error { st.Database["name"] = "published"; return nil }); err != nil {
		t.Fatal(err)
	}
	read(ctx, "original")
	read(WithRequestSnapshot(context.Background()), "published")
	// A write in the same request must read fresh state and invalidate reuse.
	if err := s.withState(ctx, true, func(st *sourceState) error {
		if st.Database["name"] != "published" {
			t.Error("write used stale state")
		}
		st.Database["name"] = "written"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	read(ctx, "written")
}

func TestRequestSnapshotConcurrentReadersAndCancellation(t *testing.T) {
	s := snapshotSource(t, 1)
	ctx, trace := requesttiming.New(WithRequestSnapshot(context.Background()))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			if err := s.withState(ctx, false, func(st *sourceState) error { st.Database["name"] = "private"; return nil }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	_, _, counts := trace.Snapshot()
	if counts["read_state"] != 1 {
		t.Fatalf("duplicate snapshot loads: %v", counts)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.withState(cancelled, false, func(*sourceState) error { t.Error("cancelled callback ran"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRequestAuthSnapshotClonesAndSeesNextRequestChanges(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "management.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, trace := requesttiming.New(WithRequestSnapshot(context.Background()))
	a, err := m.ResolveAuth(ctx, "API", "platform")
	if err != nil {
		t.Fatal(err)
	}
	original := a.Policy.Name
	a.Policy.Name = "caller mutation"
	a, err = m.ResolveAuth(ctx, "API", "platform")
	if err != nil || a.Policy.Name != original || a.store != m {
		t.Fatalf("invalid cached auth: %+v %v", a, err)
	}
	_, _, counts := trace.Snapshot()
	if counts["resolve_auth"] != 1 {
		t.Fatal(counts)
	}
	cfg, err := m.AuthConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Policies {
		if cfg.Policies[i].ID == a.Policy.ID {
			cfg.Policies[i].Name = "updated"
		}
	}
	if err := m.SaveAuthConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	a, err = m.ResolveAuth(WithRequestSnapshot(context.Background()), "API", "platform")
	if err != nil || a.Policy.Name != "updated" {
		t.Fatalf("new request retained old auth: %+v %v", a, err)
	}
}

func BenchmarkRequestConfigurationReads(b *testing.B) {
	s := snapshotSource(b, 160)
	for _, snapshot := range []bool{false, true} {
		b.Run(fmt.Sprintf("snapshot=%t", snapshot), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				ctx := context.Background()
				if snapshot {
					ctx = WithRequestSnapshot(ctx)
				}
				for i := 0; i < 9; i++ {
					if err := s.withState(ctx, false, func(*sourceState) error { return nil }); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
