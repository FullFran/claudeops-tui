package store

import (
	"context"
	"testing"
	"time"
)

func TestTopSessionsByCostExtendedFields(t *testing.T) {
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
		{"x1", "sess-Z", early, 1.0},
		{"x2", "sess-Z", now, 2.5},
	}
	for _, it := range items {
		cost := it.cost
		ev := makeEvent(it.uuid, it.session, "/p/zeta", "claude-opus-4-6", it.ts)
		if err := s.Insert(ctx, ev, &cost, nil); err != nil {
			t.Fatalf("Insert %s: %v", it.uuid, err)
		}
	}

	since := now.Add(-3 * time.Hour)
	sessions, err := s.TopSessionsByCost(ctx, 5, since)
	if err != nil {
		t.Fatalf("TopSessionsByCost: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	sa := sessions[0]

	t.Run("events count is populated", func(t *testing.T) {
		if sa.Events != 2 {
			t.Errorf("want 2 events, got %d", sa.Events)
		}
	})

	t.Run("token fields are populated", func(t *testing.T) {
		// Each event has InTokens=5, OutTokens=1101, CacheRead=15718, CacheCreate=20780
		if sa.InTokens != 10 {
			t.Errorf("want InTokens=10, got %d", sa.InTokens)
		}
		if sa.OutTokens != 2202 {
			t.Errorf("want OutTokens=2202, got %d", sa.OutTokens)
		}
	})

	t.Run("FirstSeen and LastSeen populated", func(t *testing.T) {
		if sa.FirstSeen.IsZero() {
			t.Error("FirstSeen should not be zero")
		}
		if sa.LastSeen.IsZero() {
			t.Error("LastSeen should not be zero")
		}
		if !sa.LastSeen.After(sa.FirstSeen) {
			t.Errorf("LastSeen (%v) should be after FirstSeen (%v)", sa.LastSeen, sa.FirstSeen)
		}
	})
}

func TestSessionsForDayExtendedFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, now.Location())

	items := []struct {
		uuid    string
		session string
		ts      time.Time
		cost    float64
	}{
		{"y1", "sess-W", today, 1.0},
		{"y2", "sess-W", today.Add(30 * time.Minute), 3.0},
	}
	for _, it := range items {
		cost := it.cost
		ev := makeEvent(it.uuid, it.session, "/p/w", "claude-opus-4-6", it.ts)
		if err := s.Insert(ctx, ev, &cost, nil); err != nil {
			t.Fatalf("Insert %s: %v", it.uuid, err)
		}
	}

	todayDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sessions, err := s.SessionsForDay(ctx, todayDate)
	if err != nil {
		t.Fatalf("SessionsForDay: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	sa := sessions[0]

	if sa.Events != 2 {
		t.Errorf("want 2 events, got %d", sa.Events)
	}
	if sa.FirstSeen.IsZero() {
		t.Error("FirstSeen should not be zero")
	}
	if sa.LastSeen.IsZero() {
		t.Error("LastSeen should not be zero")
	}
}

func TestModelsForSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()

	cases := []struct {
		uuid    string
		session string
		cwd     string
		model   string
		cost    float64
	}{
		{"m1", "sess-A", "/p/alpha", "claude-opus-4-6", 1.0},
		{"m2", "sess-A", "/p/alpha", "claude-sonnet-4-6", 2.5},
		{"m3", "sess-A", "/p/alpha", "claude-opus-4-6", 0.5},
		{"m4", "sess-B", "/p/beta", "claude-opus-4-6", 3.0},
	}
	for _, c := range cases {
		cost := c.cost
		ev := makeEvent(c.uuid, c.session, c.cwd, c.model, now)
		if err := s.Insert(ctx, ev, &cost, nil); err != nil {
			t.Fatalf("Insert %s: %v", c.uuid, err)
		}
	}

	t.Run("session A has two models", func(t *testing.T) {
		models, err := s.ModelsForSession(ctx, "sess-A")
		if err != nil {
			t.Fatalf("ModelsForSession: %v", err)
		}
		if len(models) != 2 {
			t.Fatalf("want 2 models, got %d", len(models))
		}
		// Ordered by cost desc: opus (1.5) > sonnet (2.5)? Actually sonnet is 2.5, opus is 1.5 total.
		// sonnet: 1 event @ 2.5 = 2.5; opus: 2 events @ 1.0+0.5 = 1.5
		// So sonnet first.
		if models[0].Model != "claude-sonnet-4-6" {
			t.Errorf("first model should be sonnet (highest cost), got %q", models[0].Model)
		}
		if models[0].CostEUR < 2.49 || models[0].CostEUR > 2.51 {
			t.Errorf("sonnet cost want ~2.5 got %v", models[0].CostEUR)
		}
	})

	t.Run("session B has one model", func(t *testing.T) {
		models, err := s.ModelsForSession(ctx, "sess-B")
		if err != nil {
			t.Fatalf("ModelsForSession: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("want 1 model, got %d", len(models))
		}
		if models[0].Model != "claude-opus-4-6" {
			t.Errorf("unexpected model %q", models[0].Model)
		}
	})

	t.Run("unknown session returns empty", func(t *testing.T) {
		models, err := s.ModelsForSession(ctx, "no-such-session")
		if err != nil {
			t.Fatalf("ModelsForSession: %v", err)
		}
		if len(models) != 0 {
			t.Errorf("want 0 models, got %d", len(models))
		}
	})
}

func TestHourlyForSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	hour10 := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, now.Location())
	hour12 := time.Date(now.Year(), now.Month(), now.Day(), 12, 30, 0, 0, now.Location())

	items := []struct {
		uuid    string
		session string
		ts      time.Time
		cost    float64
	}{
		{"h1", "sess-X", hour10, 1.0},
		{"h2", "sess-X", hour10.Add(15 * time.Minute), 0.5},
		{"h3", "sess-X", hour12, 2.0},
		{"h4", "sess-Y", hour10, 5.0}, // different session, should not appear
	}
	for _, it := range items {
		cost := it.cost
		ev := makeEvent(it.uuid, it.session, "/p/x", "claude-opus-4-6", it.ts)
		if err := s.Insert(ctx, ev, &cost, nil); err != nil {
			t.Fatalf("Insert %s: %v", it.uuid, err)
		}
	}

	t.Run("session X has two hours", func(t *testing.T) {
		hourly, err := s.HourlyForSession(ctx, "sess-X")
		if err != nil {
			t.Fatalf("HourlyForSession: %v", err)
		}
		if len(hourly) != 2 {
			t.Fatalf("want 2 hourly entries, got %d: %+v", len(hourly), hourly)
		}

		hours := make(map[int]HourlyAgg)
		for _, h := range hourly {
			hours[h.Hour] = h
		}
		if h10, ok := hours[10]; !ok {
			t.Error("expected hour 10 in results")
		} else {
			if h10.Events != 2 {
				t.Errorf("hour 10: want 2 events, got %d", h10.Events)
			}
			if h10.CostEUR < 1.49 || h10.CostEUR > 1.51 {
				t.Errorf("hour 10: want ~1.5 cost, got %v", h10.CostEUR)
			}
		}
		if _, ok := hours[12]; !ok {
			t.Error("expected hour 12 in results")
		}
	})

	t.Run("session Y events not mixed in", func(t *testing.T) {
		hourly, err := s.HourlyForSession(ctx, "sess-Y")
		if err != nil {
			t.Fatalf("HourlyForSession: %v", err)
		}
		if len(hourly) != 1 {
			t.Fatalf("want 1 hourly entry for sess-Y, got %d", len(hourly))
		}
		if hourly[0].CostEUR < 4.99 || hourly[0].CostEUR > 5.01 {
			t.Errorf("sess-Y cost want ~5.0 got %v", hourly[0].CostEUR)
		}
	})

	t.Run("ordered ascending by hour", func(t *testing.T) {
		hourly, err := s.HourlyForSession(ctx, "sess-X")
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(hourly); i++ {
			if hourly[i].Hour <= hourly[i-1].Hour {
				t.Errorf("hourly not ascending at index %d: %d <= %d", i, hourly[i].Hour, hourly[i-1].Hour)
			}
		}
	})
}
