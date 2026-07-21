package store

import (
	"context"
	"testing"
	"time"
)

// Model ids reach the events table decorated ("claude-opus-4-8[1m]"), so a
// plain GROUP BY splits one model across several rows. Aggregates fold the
// decorations away at read time — but never the free-tier marker, which names
// a variant that bills nothing.
func TestPerModelAggregatesFoldDecoratedIDs(t *testing.T) {
	type row struct {
		uuid  string
		model string
		cost  float64
	}
	tests := []struct {
		name  string
		rows  []row
		want  map[string]int64 // model → events
		costs map[string]float64
	}{
		{
			name: "bracket suffix merges into the bare id",
			rows: []row{
				{"a", "claude-opus-4-8[1m]", 5},
				{"b", "claude-opus-4-8", 2},
			},
			want:  map[string]int64{"claude-opus-4-8": 2},
			costs: map[string]float64{"claude-opus-4-8": 7},
		},
		{
			name: "free and paid tiers stay apart",
			rows: []row{
				{"a", "minimax-m2.5", 5},
				{"b", "minimax-m2.5-free", 0},
			},
			want:  map[string]int64{"minimax-m2.5": 1, "minimax-m2.5-free": 1},
			costs: map[string]float64{"minimax-m2.5": 5, "minimax-m2.5-free": 0},
		},
		{
			name: "a decorated free id merges with its plain free form",
			rows: []row{
				{"a", "openrouter/minimax-m2.5:free", 0},
				{"b", "minimax-m2.5-free", 0},
				{"c", "minimax-m2.5", 3},
			},
			want:  map[string]int64{"minimax-m2.5-free": 2, "minimax-m2.5": 1},
			costs: map[string]float64{"minimax-m2.5-free": 0, "minimax-m2.5": 3},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			now := time.Now().UTC()
			for _, r := range tc.rows {
				cost := r.cost
				if err := s.Insert(ctx, makeEvent(r.uuid, "s1", "/p", r.model, now), &cost, nil); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.PerModelAggregates(ctx, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %d, want %d (%+v)", len(got), len(tc.want), got)
			}
			for _, ma := range got {
				wantEvents, ok := tc.want[ma.Model]
				if !ok {
					t.Fatalf("unexpected model %q in %+v", ma.Model, got)
				}
				if ma.Events != wantEvents {
					t.Errorf("%s events = %d, want %d", ma.Model, ma.Events, wantEvents)
				}
				if want := tc.costs[ma.Model]; ma.CostEUR != want {
					t.Errorf("%s cost = %v, want %v", ma.Model, ma.CostEUR, want)
				}
			}
			// Rows stay ordered by cost, most expensive first.
			for i := 1; i < len(got); i++ {
				if got[i-1].CostEUR < got[i].CostEUR {
					t.Errorf("rows out of order: %+v", got)
				}
			}
		})
	}
}

func TestModelsForDayAndSessionFoldDecoratedIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, model := range []string{"claude-opus-4-8[1m]", "claude-opus-4-8"} {
		cost := float64(i + 1)
		ev := makeEvent(string(rune('a'+i)), "s1", "/p", model, now)
		if err := s.Insert(ctx, ev, &cost, nil); err != nil {
			t.Fatal(err)
		}
	}

	day, err := s.ModelsForDay(ctx, now.Local())
	if err != nil {
		t.Fatal(err)
	}
	if len(day) != 1 || day[0].Model != "claude-opus-4-8" || day[0].Events != 2 {
		t.Errorf("ModelsForDay = %+v, want one merged claude-opus-4-8 row", day)
	}

	sess, err := s.ModelsForSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess) != 1 || sess[0].Model != "claude-opus-4-8" || sess[0].Events != 2 {
		t.Errorf("ModelsForSession = %+v, want one merged claude-opus-4-8 row", sess)
	}
}
