package store

import (
	"context"
	"testing"
	"time"
)

func TestPerModelAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(uuid, model string, cost float64) Event {
		ev := makeEvent(uuid, "s1", "/p", model, now)
		return ev
	}
	items := []struct {
		ev   Event
		cost float64
	}{
		{mk("a", "claude-opus-4-6", 5.0), 5.0},
		{mk("b", "claude-opus-4-6", 2.0), 2.0},
		{mk("c", "claude-sonnet-4-6", 1.0), 1.0},
	}
	for _, it := range items {
		c := it.cost
		if err := s.Insert(ctx, it.ev, &c, nil); err != nil {
			t.Fatal(err)
		}
	}
	ms, err := s.PerModelAggregates(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("want 2 models, got %d", len(ms))
	}
	if ms[0].Model != "claude-opus-4-6" || ms[0].Events != 2 || ms[0].CostEUR < 6.99 || ms[0].CostEUR > 7.01 {
		t.Errorf("opus row wrong: %+v", ms[0])
	}
	if ms[1].Model != "claude-sonnet-4-6" || ms[1].Events != 1 {
		t.Errorf("sonnet row wrong: %+v", ms[1])
	}
}
