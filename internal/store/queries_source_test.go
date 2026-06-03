package store

import (
	"context"
	"testing"
	"time"
)

// TestAggregatesBySource covers REQ-4.1 scenarios.
func TestAggregatesBySource(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("empty DB returns empty slice", func(t *testing.T) {
		s := newTestStore(t)
		got, err := s.AggregatesBySource(ctx, time.Time{})
		if err != nil {
			t.Fatalf("AggregatesBySource: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("want 0 rows, got %d", len(got))
		}
	})

	t.Run("returns one row per source", func(t *testing.T) {
		s := newTestStore(t)

		// Insert 3 claude events and 2 codex events.
		for i, src := range []string{"claude", "claude", "claude", "codex", "codex"} {
			ev := makeEvent(
				"msrc-"+src+"-"+string(rune('a'+i)),
				"msrc-sess-"+src,
				"/p/msrc",
				"model",
				now,
			)
			ev.Source = src
			cost := 1.0
			if err := s.Insert(ctx, ev, &cost, nil); err != nil {
				t.Fatalf("Insert %s[%d]: %v", src, i, err)
			}
		}

		got, err := s.AggregatesBySource(ctx, time.Time{})
		if err != nil {
			t.Fatalf("AggregatesBySource: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 source rows, got %d: %+v", len(got), got)
		}

		bySource := make(map[string]SourceAgg, len(got))
		for _, ag := range got {
			bySource[ag.Source] = ag
		}
		if bySource["claude"].Events != 3 {
			t.Errorf("claude: want 3 events, got %d", bySource["claude"].Events)
		}
		if bySource["codex"].Events != 2 {
			t.Errorf("codex: want 2 events, got %d", bySource["codex"].Events)
		}
	})

	t.Run("sums are correct", func(t *testing.T) {
		s := newTestStore(t)

		// 3 claude events with cost 1.0 each → total 3.0
		for i := 0; i < 3; i++ {
			ev := makeEvent(
				"sum-claude-"+string(rune('a'+i)),
				"sum-sess",
				"/p/sum",
				"model",
				now,
			)
			ev.Source = "claude"
			ev.InTokens = 100
			cost := 1.0
			if err := s.Insert(ctx, ev, &cost, nil); err != nil {
				t.Fatalf("Insert: %v", err)
			}
		}

		got, err := s.AggregatesBySource(ctx, time.Time{})
		if err != nil {
			t.Fatalf("AggregatesBySource: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 row, got %d", len(got))
		}
		ag := got[0]
		if ag.Source != "claude" {
			t.Errorf("want source='claude', got %q", ag.Source)
		}
		if ag.Events != 3 {
			t.Errorf("want 3 events, got %d", ag.Events)
		}
		if ag.InTokens != 300 {
			t.Errorf("want InTokens=300, got %d", ag.InTokens)
		}
		if ag.CostEUR < 2.99 || ag.CostEUR > 3.01 {
			t.Errorf("want ~3.0 cost, got %v", ag.CostEUR)
		}
	})
}
