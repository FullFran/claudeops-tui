package store

import (
	"context"
	"testing"
	"time"
)

func TestAggregatesByProjectBetween(t *testing.T) {
	base := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	from := base
	to := base.Add(4 * time.Hour)

	tests := []struct {
		name      string
		seed      func(t *testing.T, s *Store)
		wantLen   int
		checkFunc func(t *testing.T, aggs []ProjectPeriodAgg)
	}{
		{
			name:    "empty db returns empty slice",
			seed:    func(t *testing.T, s *Store) {},
			wantLen: 0,
			checkFunc: func(t *testing.T, aggs []ProjectPeriodAgg) {
				t.Helper()
				if aggs != nil {
					t.Errorf("want nil slice, got %v", aggs)
				}
			},
		},
		{
			name: "one project one event in range",
			seed: func(t *testing.T, s *Store) {
				t.Helper()
				ctx := context.Background()
				cost := 1.5
				ev := makeEvent("p1e1", "p1s1", "/p/alpha", "claude-opus-4-6", base.Add(time.Hour))
				ev.InTokens = 100
				ev.OutTokens = 50
				ev.CacheReadTokens = 200
				ev.CacheCreateTokens = 300
				if err := s.Insert(ctx, ev, &cost, nil); err != nil {
					t.Fatalf("Insert: %v", err)
				}
			},
			wantLen: 1,
			checkFunc: func(t *testing.T, aggs []ProjectPeriodAgg) {
				t.Helper()
				a := aggs[0]
				if a.ProjectName != "alpha" {
					t.Errorf("ProjectName: want alpha got %q", a.ProjectName)
				}
				if a.CostEUR < 1.49 || a.CostEUR > 1.51 {
					t.Errorf("CostEUR: want ~1.5 got %v", a.CostEUR)
				}
				if a.InTokens != 100 {
					t.Errorf("InTokens: want 100 got %d", a.InTokens)
				}
				if a.OutTokens != 50 {
					t.Errorf("OutTokens: want 50 got %d", a.OutTokens)
				}
				if a.CacheReadTokens != 200 {
					t.Errorf("CacheReadTokens: want 200 got %d", a.CacheReadTokens)
				}
				if a.CacheCreateTokens != 300 {
					t.Errorf("CacheCreateTokens: want 300 got %d", a.CacheCreateTokens)
				}
				if a.Sessions != 1 {
					t.Errorf("Sessions: want 1 got %d", a.Sessions)
				}
			},
		},
		{
			name: "two projects both returned sorted by cost desc",
			seed: func(t *testing.T, s *Store) {
				t.Helper()
				ctx := context.Background()
				c1 := 1.0
				c2 := 3.5
				ev1 := makeEvent("tp1e1", "tp1s1", "/p/cheap", "claude-opus-4-6", base.Add(time.Hour))
				ev2 := makeEvent("tp2e1", "tp2s1", "/p/expensive", "claude-opus-4-6", base.Add(2*time.Hour))
				if err := s.Insert(ctx, ev1, &c1, nil); err != nil {
					t.Fatalf("Insert ev1: %v", err)
				}
				if err := s.Insert(ctx, ev2, &c2, nil); err != nil {
					t.Fatalf("Insert ev2: %v", err)
				}
			},
			wantLen: 2,
			checkFunc: func(t *testing.T, aggs []ProjectPeriodAgg) {
				t.Helper()
				// First should be the more expensive one
				if aggs[0].ProjectName != "expensive" {
					t.Errorf("want expensive first, got %q", aggs[0].ProjectName)
				}
				if aggs[1].ProjectName != "cheap" {
					t.Errorf("want cheap second, got %q", aggs[1].ProjectName)
				}
			},
		},
		{
			name: "event at from included, event at to excluded",
			seed: func(t *testing.T, s *Store) {
				t.Helper()
				ctx := context.Background()
				cAt := 1.0
				cBefore := 2.0
				cEnd := 3.0
				evAt := makeEvent("bnd_at", "bnd_s1", "/p/boundary", "claude-opus-4-6", from)
				evBefore := makeEvent("bnd_before", "bnd_s2", "/p/boundary", "claude-opus-4-6", from.Add(-time.Minute))
				evEnd := makeEvent("bnd_end", "bnd_s3", "/p/boundary", "claude-opus-4-6", to)
				if err := s.Insert(ctx, evAt, &cAt, nil); err != nil {
					t.Fatalf("Insert evAt: %v", err)
				}
				if err := s.Insert(ctx, evBefore, &cBefore, nil); err != nil {
					t.Fatalf("Insert evBefore: %v", err)
				}
				if err := s.Insert(ctx, evEnd, &cEnd, nil); err != nil {
					t.Fatalf("Insert evEnd: %v", err)
				}
			},
			wantLen: 1,
			checkFunc: func(t *testing.T, aggs []ProjectPeriodAgg) {
				t.Helper()
				a := aggs[0]
				if a.ProjectName != "boundary" {
					t.Errorf("ProjectName: want boundary got %q", a.ProjectName)
				}
				// Only the event at `from` should be included (not before, not at `to`)
				if a.CostEUR < 0.99 || a.CostEUR > 1.01 {
					t.Errorf("CostEUR: want ~1.0 (only event at from), got %v", a.CostEUR)
				}
			},
		},
		{
			name: "multiple events same project are summed",
			seed: func(t *testing.T, s *Store) {
				t.Helper()
				ctx := context.Background()
				costs := []float64{1.0, 2.0, 3.0}
				for i, c := range costs {
					c := c
					ev := makeEvent(
						"sum_ev_"+string(rune('a'+i)),
						"sum_sess",
						"/p/summed",
						"claude-opus-4-6",
						base.Add(time.Duration(i+1)*time.Hour),
					)
					ev.InTokens = int64(100 * (i + 1))
					if err := s.Insert(ctx, ev, &c, nil); err != nil {
						t.Fatalf("Insert ev%d: %v", i, err)
					}
				}
			},
			wantLen: 1,
			checkFunc: func(t *testing.T, aggs []ProjectPeriodAgg) {
				t.Helper()
				a := aggs[0]
				if a.CostEUR < 5.99 || a.CostEUR > 6.01 {
					t.Errorf("CostEUR: want ~6.0 got %v", a.CostEUR)
				}
				if a.InTokens != 600 { // 100+200+300
					t.Errorf("InTokens: want 600 got %d", a.InTokens)
				}
				if a.Sessions != 1 {
					t.Errorf("Sessions: want 1 got %d", a.Sessions)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			tc.seed(t, s)
			aggs, err := s.AggregatesByProjectBetween(context.Background(), from, to)
			if err != nil {
				t.Fatalf("AggregatesByProjectBetween: %v", err)
			}
			if len(aggs) != tc.wantLen {
				t.Fatalf("len(aggs): want %d got %d: %+v", tc.wantLen, len(aggs), aggs)
			}
			if tc.checkFunc != nil {
				tc.checkFunc(t, aggs)
			}
		})
	}
}
