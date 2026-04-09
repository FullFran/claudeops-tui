package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestSessionAggByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	early := now.Add(-2 * time.Hour)

	items := []struct {
		uuid    string
		session string
		ts      time.Time
		cost    float64
	}{
		{"b1", "sess-byid", early, 1.5},
		{"b2", "sess-byid", now, 2.5},
		{"b3", "sess-other", now, 3.0},
	}
	for _, it := range items {
		cost := it.cost
		ev := makeEvent(it.uuid, it.session, "/p/byid", "claude-opus-4-6", it.ts)
		if err := s.Insert(ctx, ev, &cost, nil); err != nil {
			t.Fatalf("Insert %s: %v", it.uuid, err)
		}
	}

	t.Run("found returns correct aggregates", func(t *testing.T) {
		sa, err := s.SessionAggByID(ctx, "sess-byid")
		if err != nil {
			t.Fatalf("SessionAggByID: %v", err)
		}
		if sa.SessionID != "sess-byid" {
			t.Errorf("want SessionID 'sess-byid', got %q", sa.SessionID)
		}
		if sa.Events != 2 {
			t.Errorf("want 2 events, got %d", sa.Events)
		}
		if sa.CostEUR < 3.99 || sa.CostEUR > 4.01 {
			t.Errorf("want cost ~4.0, got %v", sa.CostEUR)
		}
		if sa.FirstSeen.IsZero() {
			t.Error("FirstSeen should not be zero")
		}
		if sa.LastSeen.IsZero() {
			t.Error("LastSeen should not be zero")
		}
		if !sa.LastSeen.After(sa.FirstSeen) {
			t.Errorf("LastSeen should be after FirstSeen")
		}
		if sa.ProjectName != "byid" {
			t.Errorf("want ProjectName 'byid', got %q", sa.ProjectName)
		}
	})

	t.Run("not found returns sql.ErrNoRows", func(t *testing.T) {
		_, err := s.SessionAggByID(ctx, "no-such-session")
		if err != sql.ErrNoRows {
			t.Errorf("want sql.ErrNoRows, got %v", err)
		}
	})
}
