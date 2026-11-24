package audit

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestDurationSearchPrecisionAndUnknownTimes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	start := "2026-11-24T10:00:00.999999900Z"
	for _, event := range []Event{
		{ID: "fast", StartedAt: start, FinishedAt: "2026-11-24T10:00:01.000000100Z"},
		{ID: "slow", StartedAt: start, FinishedAt: "2026-11-24T18:00:02.000000100+08:00"},
		{ID: "zero", StartedAt: start, FinishedAt: start},
		{ID: "missing", StartedAt: start},
		{ID: "invalid", StartedAt: start, FinishedAt: "invalid"},
		{ID: "reversed", StartedAt: start, FinishedAt: "2026-11-24T10:00:00Z"},
	} {
		if err := s.Put(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	minimum := 1000.0001
	page, err := s.Search(ctx, Filter{MinDurationMS: &minimum, Sort: "duration_desc"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "slow" {
		t.Fatalf("precise threshold: %+v %v", page, err)
	}
	minimum = 0
	page, err = s.Search(ctx, Filter{MinDurationMS: &minimum, Sort: "duration_asc"})
	if err != nil || page.Total != 3 || len(page.Items) != 3 || page.Items[0].ID != "zero" || page.Items[1].ID != "fast" || page.Items[2].ID != "slow" {
		t.Fatalf("valid durations: %+v %v", page, err)
	}
	for _, sort := range []string{"duration_desc", "duration_asc"} {
		page, err = s.Search(ctx, Filter{Sort: sort, Limit: 2, Offset: 3})
		if err != nil || page.Total != 6 || len(page.Items) != 2 {
			t.Fatalf("pagination: %+v %v", page, err)
		}
		for _, event := range page.Items {
			if _, valid := eventDuration(event.StartedAt, event.FinishedAt); valid {
				t.Fatal("unknown duration sorted before valid duration")
			}
		}
	}
	for _, value := range []float64{-1, math.Inf(1), math.NaN()} {
		if _, err := s.Search(ctx, Filter{MinDurationMS: &value}); err == nil {
			t.Fatal("accepted invalid threshold", value)
		}
	}
	if _, err := s.Search(ctx, Filter{Sort: "duration_desc; DROP TABLE AUDIT_EVENT"}); err == nil {
		t.Fatal("accepted invalid sort")
	}
}

func TestDurationSortAcrossStoresAndPageBoundaries(t *testing.T) {
	ctx := context.Background()
	stores := make([]*Store, 2)
	start := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	for j := range stores {
		s, err := Open(filepath.Join(t.TempDir(), "audit.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		stores[j] = s
		for i := 0; i < 205; i++ {
			n := i*2 + j
			err = s.Put(ctx, Event{ID: fmt.Sprint(n), StartedAt: start.Format(time.RFC3339Nano), FinishedAt: start.Add(time.Duration(n) * time.Millisecond).Format(time.RFC3339Nano)})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	stores[0].Sources = func() []*Store { return stores[1:] }
	for _, sort := range []string{"duration_desc", "duration_asc"} {
		page, err := stores[0].Search(ctx, Filter{Sort: sort, Offset: 199, Limit: 15})
		if err != nil || page.Total != 410 || len(page.Items) != 15 {
			t.Fatalf("merged page: %+v %v", page, err)
		}
		for i, event := range page.Items {
			want := 199 + i
			if sort == "duration_desc" {
				want = 409 - want
			}
			if event.ID != fmt.Sprint(want) {
				t.Fatalf("%s item %d: got %s want %d", sort, i, event.ID, want)
			}
		}
	}
}
