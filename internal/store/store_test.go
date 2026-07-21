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

func TestInsertIdenticalEventDoesNotWriteWAL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cost := 0.1
	ev := makeEvent("dup-wal", "sess-wal", "/tmp/wal", "claude-opus-4-6", time.Now())
	if err := s.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatal(err)
	}
	checkpointWAL(t, s)

	for range 3 {
		if err := s.Insert(ctx, ev, &cost, nil); err != nil {
			t.Fatal(err)
		}
	}
	if frames := walFrames(t, s); frames != 0 {
		t.Fatalf("identical Insert wrote %d WAL frames, want 0", frames)
	}
}

func TestInsertSessionLastSeenIsMonotonic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cost := 0.1
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	first := makeEvent("first", "sess-monotonic", "/tmp/monotonic", "claude-opus-4-6", base)
	if err := s.Insert(ctx, first, &cost, nil); err != nil {
		t.Fatal(err)
	}

	olderDuplicate := first
	olderDuplicate.TS = base.Add(-time.Minute)
	if err := s.Insert(ctx, olderDuplicate, &cost, nil); err != nil {
		t.Fatal(err)
	}
	assertSessionLastSeen(t, s, first.SessionID, base)

	newerDuplicate := first
	newerDuplicate.TS = base.Add(time.Minute)
	if err := s.Insert(ctx, newerDuplicate, &cost, nil); err != nil {
		t.Fatal(err)
	}
	assertSessionLastSeen(t, s, first.SessionID, newerDuplicate.TS)
}

func TestInsertSessionLastSeenAdvancesAcrossRFC3339NanoFractionBoundary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cost := 0.1
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	first := makeEvent("first-fraction", "sess-fraction", "/tmp/fraction", "claude-opus-4-6", base)
	if err := s.Insert(ctx, first, &cost, nil); err != nil {
		t.Fatal(err)
	}

	later := first
	later.TS = base.Add(100 * time.Millisecond)
	if err := s.Insert(ctx, later, &cost, nil); err != nil {
		t.Fatal(err)
	}
	assertSessionLastSeen(t, s, first.SessionID, later.TS)
}

func TestInsertMaintainsSessionBoundsAcrossOutOfOrderRFC3339NanoEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cost := 0.1
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	earliest := base.Add(time.Nanosecond)
	middle := base.Add(500 * time.Millisecond)
	latest := base.Add(time.Second - time.Nanosecond)

	ev := makeEvent("bounds", "sess-bounds", "/tmp/bounds", "claude-opus-4-6", middle)
	if err := s.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatal(err)
	}

	later := ev
	later.TS = latest
	if err := s.Insert(ctx, later, &cost, nil); err != nil {
		t.Fatal(err)
	}

	earlier := ev
	earlier.TS = earliest
	if err := s.Insert(ctx, earlier, &cost, nil); err != nil {
		t.Fatal(err)
	}
	assertSessionBounds(t, s, ev.SessionID, earliest, latest)

	checkpointWAL(t, s)
	insideBounds := ev
	insideBounds.TS = middle
	if err := s.Insert(ctx, insideBounds, &cost, nil); err != nil {
		t.Fatal(err)
	}
	if frames := walFrames(t, s); frames != 0 {
		t.Fatalf("identical event inside session bounds wrote %d WAL frames, want 0", frames)
	}
	assertSessionBounds(t, s, ev.SessionID, earliest, latest)
}

// Claude Code writes one JSONL line per content block of an assistant message.
// Those lines share input/cache token counts but output_tokens grows
// monotonically (streaming partial → final). After dedup they collapse onto
// one uuid; the surviving row must keep the LARGEST output_tokens (the final
// count), not the first-seen partial. Keep-first would undercount output.
func TestInsertKeepsMaxOutputOnConflict(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ts := time.Now()

	partial := Event{
		UUID: "msg:req", SessionID: "s1", CWD: "/p", Type: "assistant",
		Model: "claude-fable-5", TS: ts,
		InTokens: 10, OutTokens: 1, CacheReadTokens: 200, CacheCreateTokens: 30,
	}
	final := partial
	final.OutTokens = 350 // streaming final count
	costPartial, costFinal := 0.001, 0.35

	// Ingest order is partial-then-final (the real file order).
	if err := s.Insert(ctx, partial, &costPartial, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(ctx, final, &costFinal, nil); err != nil {
		t.Fatal(err)
	}

	var rows, out int64
	var cost float64
	if err := s.db.QueryRow(
		`SELECT COUNT(*), out_tokens, cost_eur FROM events WHERE uuid='msg:req'`,
	).Scan(&rows, &out, &cost); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("want 1 row, got %d", rows)
	}
	if out != 350 {
		t.Errorf("out_tokens: want 350 (final), got %d", out)
	}
	if cost != costFinal {
		t.Errorf("cost_eur: want %v (recomputed for final), got %v", costFinal, cost)
	}

	t.Run("reverse order is order-independent", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.Insert(ctx, final, &costFinal, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Insert(ctx, partial, &costPartial, nil); err != nil {
			t.Fatal(err)
		}
		var out int64
		_ = s.db.QueryRow(`SELECT out_tokens FROM events WHERE uuid='msg:req'`).Scan(&out)
		if out != 350 {
			t.Errorf("out_tokens after reverse order: want 350, got %d", out)
		}
	})

	t.Run("equal output is a no-op (idempotent)", func(t *testing.T) {
		s := newTestStore(t)
		c := 0.35
		if err := s.Insert(ctx, final, &c, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Insert(ctx, final, &c, nil); err != nil {
			t.Fatal(err)
		}
		var rows, out int64
		_ = s.db.QueryRow(`SELECT COUNT(*), out_tokens FROM events WHERE uuid='msg:req'`).Scan(&rows, &out)
		if rows != 1 || out != 350 {
			t.Errorf("idempotent equal insert: rows=%d out=%d, want 1/350", rows, out)
		}
	})
}

func TestInsertRequiresKeys(t *testing.T) {
	s := newTestStore(t)
	if err := s.Insert(context.Background(), Event{}, nil, nil); err == nil {
		t.Fatal("expected error on empty event")
	}
}

// ResetIngestedData clears everything derived from source files (events,
// sessions, projects, file offsets, source watermarks) so a re-ingest rebuilds
// them from scratch — without touching user-owned tasks or config. This is what
// `claudeops reingest` relies on to correct pre-fix inflated rows.
func TestResetIngestedData(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cost := 0.42
	ev := makeEvent("u1", "sess-1", "/tmp/proj-x", "claude-opus-4-6", time.Now())
	if err := s.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOffset("/some/file.jsonl", 100, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSourceWatermark("opencode", "1700000000000"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigSet(ctx, "keep", "me"); err != nil {
		t.Fatal(err)
	}

	if err := s.ResetIngestedData(ctx); err != nil {
		t.Fatalf("ResetIngestedData: %v", err)
	}

	for _, tbl := range []string{"events", "sessions", "projects", "file_offsets", "source_watermarks"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: want 0 rows after reset, got %d", tbl, n)
		}
	}

	// Config is user-owned and must survive a reset.
	if v, ok, err := s.ConfigGet(ctx, "keep"); err != nil || !ok || v != "me" {
		t.Errorf("config wiped by reset: v=%q ok=%v err=%v", v, ok, err)
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

func TestSaveOffsetIdenticalValueDoesNotWriteWAL(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveOffset("/a/wal.jsonl", 100, 200); err != nil {
		t.Fatal(err)
	}
	checkpointWAL(t, s)

	if err := s.SaveOffset("/a/wal.jsonl", 100, 200); err != nil {
		t.Fatal(err)
	}
	if frames := walFrames(t, s); frames != 0 {
		t.Fatalf("identical SaveOffset wrote %d WAL frames, want 0", frames)
	}
}

func assertSessionLastSeen(t *testing.T, s *Store, sessionID string, want time.Time) {
	t.Helper()
	var got string
	if err := s.DB().QueryRow(`SELECT last_seen FROM sessions WHERE id = ?`, sessionID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if wantText := want.UTC().Format(time.RFC3339Nano); got != wantText {
		t.Fatalf("last_seen = %q, want %q", got, wantText)
	}
}

func assertSessionBounds(t *testing.T, s *Store, sessionID string, wantFirst, wantLast time.Time) {
	t.Helper()
	var firstSeen, lastSeen string
	if err := s.DB().QueryRow(`SELECT first_seen, last_seen FROM sessions WHERE id = ?`, sessionID).Scan(&firstSeen, &lastSeen); err != nil {
		t.Fatal(err)
	}
	gotFirst, err := time.Parse(time.RFC3339Nano, firstSeen)
	if err != nil {
		t.Fatalf("parse first_seen %q: %v", firstSeen, err)
	}
	gotLast, err := time.Parse(time.RFC3339Nano, lastSeen)
	if err != nil {
		t.Fatalf("parse last_seen %q: %v", lastSeen, err)
	}
	if !gotFirst.Equal(wantFirst) || !gotLast.Equal(wantLast) {
		t.Fatalf("session bounds = [%s, %s], want [%s, %s]", gotFirst, gotLast, wantFirst, wantLast)
	}
}

func checkpointWAL(t *testing.T, s *Store) {
	t.Helper()
	var busy, log, checkpointed int
	if err := s.DB().QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &log, &checkpointed); err != nil {
		t.Fatal(err)
	}
	if busy != 0 {
		t.Fatalf("WAL checkpoint remained busy")
	}
}

func walFrames(t *testing.T, s *Store) int {
	t.Helper()
	var busy, log, checkpointed int
	if err := s.DB().QueryRow(`PRAGMA wal_checkpoint(PASSIVE)`).Scan(&busy, &log, &checkpointed); err != nil {
		t.Fatal(err)
	}
	if busy != 0 {
		t.Fatalf("WAL checkpoint remained busy")
	}
	return log
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

func TestAggregatesBetweenUsesHalfOpenWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	items := []struct {
		uuid string
		ts   time.Time
		cost float64
	}{
		{uuid: "before", ts: base.Add(-time.Minute), cost: 1.0},
		{uuid: "start", ts: base, cost: 2.0},
		{uuid: "inside", ts: base.Add(time.Hour), cost: 3.0},
		{uuid: "end", ts: base.Add(2 * time.Hour), cost: 4.0},
	}
	for _, it := range items {
		cost := it.cost
		ev := makeEvent(it.uuid, "sess-window", "/p/window", "claude-opus-4-6", it.ts)
		if err := s.Insert(ctx, ev, &cost, nil); err != nil {
			t.Fatalf("Insert %s: %v", it.uuid, err)
		}
	}

	agg, err := s.AggregatesBetween(ctx, base, base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if agg.Events != 2 {
		t.Fatalf("want 2 events in [from,to), got %d", agg.Events)
	}
	if agg.CostEUR < 4.99 || agg.CostEUR > 5.01 {
		t.Fatalf("want ~5.0 cost in [from,to), got %.4f", agg.CostEUR)
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

func TestDayDrillDownQueries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 10, 30, 0, 0, now.Location())

	cost1 := 1.5
	ev1 := makeEvent("d1", "s1", "/p/alpha", "claude-opus-4-6", today)
	if err := s.Insert(ctx, ev1, &cost1, nil); err != nil {
		t.Fatal(err)
	}
	cost2 := 2.0
	ev2 := makeEvent("d2", "s2", "/p/beta", "claude-sonnet-4-6", today.Add(2*time.Hour))
	if err := s.Insert(ctx, ev2, &cost2, nil); err != nil {
		t.Fatal(err)
	}

	todayDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// SessionsForDay
	sess, err := s.SessionsForDay(ctx, todayDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sess))
	}
	// Highest cost first.
	if sess[0].CostEUR < sess[1].CostEUR {
		t.Error("sessions should be ordered by cost desc")
	}

	// ModelsForDay
	models, err := s.ModelsForDay(ctx, todayDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	// HourlyForDay
	hourly, err := s.HourlyForDay(ctx, todayDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(hourly) == 0 {
		t.Fatal("expected at least 1 hourly entry")
	}
	// Should have entries for hour 10 and hour 12.
	hours := make(map[int]bool)
	for _, h := range hourly {
		hours[h.Hour] = true
	}
	if !hours[10] || !hours[12] {
		t.Errorf("expected hours 10 and 12, got %v", hours)
	}
}
