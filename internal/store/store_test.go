package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func makeEvent(uuid, session, cwd, model string, ts time.Time) Event {
	return Event{
		UUID:              uuid,
		SessionID:         session,
		CWD:               cwd,
		Type:              "assistant",
		Model:             model,
		TS:                ts,
		InTokens:          5,
		OutTokens:         1101,
		CacheReadTokens:   15718,
		CacheCreateTokens: 20780,
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.db")
	s1, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = s2.Close()
}

func TestInsertUpsertsProjectAndSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cost := 0.42
	ev := makeEvent("u1", "sess-1", "/tmp/proj-x", "claude-opus-4-6", time.Now())
	if err := s.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE cwd='/tmp/proj-x'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("project upsert: n=%d err=%v", n, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id='sess-1'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("session upsert: n=%d err=%v", n, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE uuid='u1'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("event insert: n=%d err=%v", n, err)
	}
}

func TestInsertIdempotentOnUUID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cost := 0.1
	ev := makeEvent("dup", "sess-1", "/tmp/p", "claude-opus-4-6", time.Now())
	if err := s.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE uuid='dup'`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
}

func TestInsertRequiresKeys(t *testing.T) {
	s := newTestStore(t)
	if err := s.Insert(context.Background(), Event{}, nil, nil); err == nil {
		t.Fatal("expected error on empty event")
	}
}

func TestOffsetsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveOffset("/a/b.jsonl", 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOffset("/a/b.jsonl", 150, 250); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOffset("/c.jsonl", 7, 7); err != nil {
		t.Fatal(err)
	}
	m, err := s.LoadOffsets()
	if err != nil {
		t.Fatal(err)
	}
	if m["/a/b.jsonl"] != 150 {
		t.Errorf("offset want 150 got %d", m["/a/b.jsonl"])
	}
	if m["/c.jsonl"] != 7 {
		t.Errorf("offset want 7 got %d", m["/c.jsonl"])
	}
}

func TestAggregatesAndTopQueries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(uuid, session, cwd string, cost float64) Event {
		ev := makeEvent(uuid, session, cwd, "claude-opus-4-6", now)
		return ev
	}
	type item struct {
		ev   Event
		cost float64
	}
	items := []item{
		{mk("a", "s1", "/p/alpha", 1.0), 1.0},
		{mk("b", "s1", "/p/alpha", 2.5), 2.5},
		{mk("c", "s2", "/p/alpha", 0.5), 0.5},
		{mk("d", "s3", "/p/beta", 3.0), 3.0},
	}
	for _, it := range items {
		c := it.cost
		if err := s.Insert(ctx, it.ev, &c, nil); err != nil {
			t.Fatal(err)
		}
	}

	agg, err := s.AggregatesForToday(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if agg.Events != 4 {
		t.Errorf("events: want 4 got %d", agg.Events)
	}
	if agg.CostEUR < 6.99 || agg.CostEUR > 7.01 {
		t.Errorf("cost: want ~7.0 got %v", agg.CostEUR)
	}

	since := now.Add(-time.Hour)
	sessions, err := s.TopSessionsByCost(ctx, 5, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Errorf("sessions: want 3 got %d", len(sessions))
	}
	if sessions[0].SessionID != "s1" || sessions[0].CostEUR < 3.49 || sessions[0].CostEUR > 3.51 {
		t.Errorf("top session unexpected: %+v", sessions[0])
	}

	projects, err := s.TopProjectsByCost(ctx, 5, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Errorf("projects: want 2 got %d", len(projects))
	}
	if projects[0].ProjectName != "alpha" || projects[0].CostEUR < 3.99 || projects[0].CostEUR > 4.01 {
		t.Errorf("top project unexpected: %+v", projects[0])
	}
}

func TestTaskUpsertAndAggregate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.UpsertTask(ctx, "t1", "refactor", now, 4*time.Hour); err != nil {
		t.Fatal(err)
	}
	tid := "t1"
	cost := 1.5
	ev := makeEvent("u1", "s1", "/p/alpha", "claude-opus-4-6", now)
	if err := s.Insert(ctx, ev, &cost, &tid); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.TaskAggregates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "t1" || tasks[0].Events != 1 || tasks[0].CostEUR < 1.49 {
		t.Errorf("task agg unexpected: %+v", tasks)
	}
	if err := s.EndTask(ctx, "t1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	tasks, _ = s.TaskAggregates(ctx)
	if tasks[0].EndedAt == nil {
		t.Errorf("expected ended_at set")
	}
}

func TestDailyAggregatesLocalReturnsContiguousSeries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	twoDaysAgo := today.AddDate(0, 0, -2)

	c1 := 1.5
	c2 := 4.0
	c3 := 2.25
	_ = s.Insert(ctx, makeEvent("u1", "sA", "/p/x", "claude-opus-4-6", today), &c1, nil)
	_ = s.Insert(ctx, makeEvent("u2", "sA", "/p/x", "claude-opus-4-6", today), &c2, nil)
	_ = s.Insert(ctx, makeEvent("u3", "sB", "/p/x", "claude-opus-4-6", twoDaysAgo), &c3, nil)

	rows, err := s.DailyAggregatesLocal(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 7 {
		t.Fatalf("want 7 contiguous days, got %d", len(rows))
	}
	last := rows[len(rows)-1]
	if last.CostEUR != 5.5 || last.Events != 2 || last.Sessions != 1 {
		t.Errorf("today row wrong: %+v", last)
	}
	twoAgo := rows[len(rows)-3]
	if twoAgo.CostEUR != 2.25 || twoAgo.Events != 1 {
		t.Errorf("two-days-ago row wrong: %+v", twoAgo)
	}
	// Days with no activity must be present with zeros (sparkline needs them).
	yesterday := rows[len(rows)-2]
	if yesterday.CostEUR != 0 || yesterday.Events != 0 {
		t.Errorf("empty day should be zero, got %+v", yesterday)
	}
	// Series must be sorted oldest → newest.
	for i := 1; i < len(rows); i++ {
		if !rows[i].Date.After(rows[i-1].Date) {
			t.Errorf("series not strictly ascending at index %d", i)
		}
	}
}
