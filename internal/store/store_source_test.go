package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newCtx() context.Context { return context.Background() }
func nowTime() time.Time      { return time.Now().UTC() }

// TestEventSourceField covers REQ-1.2: Source field on Event is stored and read back.
func TestEventSourceField(t *testing.T) {
	s := newTestStore(t)
	ctx := newCtx()

	ev := makeEvent("src-u1", "src-s1", "/p/x", "claude-opus-4-6", nowTime())
	ev.Source = "claude"
	cost := 0.1
	if err := s.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var src string
	if err := s.db.QueryRow(`SELECT source FROM events WHERE uuid='src-u1'`).Scan(&src); err != nil {
		t.Fatalf("SELECT source: %v", err)
	}
	if src != "claude" {
		t.Errorf("want source='claude' got %q", src)
	}
}

// TestDedupIgnoresSource covers REQ-1.3: same uuid, different source → silently ignored.
func TestDedupIgnoresSource(t *testing.T) {
	s := newTestStore(t)
	ctx := newCtx()
	ts := nowTime()

	ev := makeEvent("dedup-uuid", "dedup-s", "/p/dedup", "model", ts)
	ev.Source = "claude"
	cost := 0.5
	if err := s.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatalf("first Insert: %v", err)
	}

	// Same uuid, different source — must be silently ignored.
	ev2 := makeEvent("dedup-uuid", "dedup-s", "/p/dedup", "model", ts)
	ev2.Source = "codex"
	if err := s.Insert(ctx, ev2, &cost, nil); err != nil {
		t.Fatalf("second Insert (different source): %v", err)
	}

	var cnt int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE uuid='dedup-uuid'`).Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Errorf("want 1 row after dedup, got %d", cnt)
	}

	var src string
	if err := s.db.QueryRow(`SELECT source FROM events WHERE uuid='dedup-uuid'`).Scan(&src); err != nil {
		t.Fatalf("SELECT source: %v", err)
	}
	if src != "claude" {
		t.Errorf("source should stay 'claude' (first writer wins), got %q", src)
	}
}

// TestSourceColumnMigration covers REQ-1.1 scenarios.
func TestSourceColumnMigration(t *testing.T) {
	t.Run("fresh DB has source column with DEFAULT claude", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "fresh.db")
		s, err := Open(p)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = s.Close() }()

		has, err := s.columnExists("events", "source")
		if err != nil {
			t.Fatalf("columnExists: %v", err)
		}
		if !has {
			t.Fatal("fresh DB: events table missing source column")
		}

		has, err = s.columnExists("sessions", "source")
		if err != nil {
			t.Fatalf("columnExists sessions: %v", err)
		}
		if !has {
			t.Fatal("fresh DB: sessions table missing source column")
		}
	})

	t.Run("migration adds source column to existing DB", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "legacy.db")

		// Create a DB and manually remove the source column to simulate a v0 schema.
		s0, err := Open(p)
		if err != nil {
			t.Fatalf("initial Open: %v", err)
		}
		if _, err := s0.db.Exec(`ALTER TABLE events RENAME TO events_old`); err != nil {
			t.Fatalf("rename: %v", err)
		}
		if _, err := s0.db.Exec(`CREATE TABLE events (
			uuid TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			ts TEXT NOT NULL,
			type TEXT NOT NULL,
			model TEXT,
			in_tokens INTEGER NOT NULL DEFAULT 0,
			out_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_create_tokens INTEGER NOT NULL DEFAULT 0,
			cost_eur REAL,
			task_id TEXT REFERENCES tasks(id)
		)`); err != nil {
			t.Fatalf("create legacy events: %v", err)
		}
		// Copy rows (may be 0 in this test).
		_, _ = s0.db.Exec(`INSERT INTO events SELECT uuid, session_id, ts, type, model, in_tokens, out_tokens, cache_read_tokens, cache_create_tokens, cost_eur, task_id FROM events_old`)
		if _, err := s0.db.Exec(`DROP TABLE events_old`); err != nil {
			t.Fatalf("drop old: %v", err)
		}
		_ = s0.Close()

		// Reopen — migration must add the source column.
		s1, err := Open(p)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer func() { _ = s1.Close() }()

		has, err := s1.columnExists("events", "source")
		if err != nil {
			t.Fatalf("columnExists: %v", err)
		}
		if !has {
			t.Fatal("migration did not add source column")
		}
	})

	t.Run("migration is idempotent", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "idem.db")

		s1, err := Open(p)
		if err != nil {
			t.Fatalf("first open: %v", err)
		}
		_ = s1.Close()

		// Second open must not error.
		s2, err := Open(p)
		if err != nil {
			t.Fatalf("second open: %v", err)
		}
		defer func() { _ = s2.Close() }()

		has, _ := s2.columnExists("events", "source")
		if !has {
			t.Fatal("source column missing after idempotent reopen")
		}
	})

	t.Run("existing rows read as claude via DEFAULT", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "default.db")

		// Create v0 DB and insert a row without source.
		s0, err := Open(p)
		if err != nil {
			t.Fatalf("initial Open: %v", err)
		}
		if _, err := s0.db.Exec(`ALTER TABLE events RENAME TO events_old`); err != nil {
			t.Fatalf("rename: %v", err)
		}
		if _, err := s0.db.Exec(`CREATE TABLE events (
			uuid TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			ts TEXT NOT NULL,
			type TEXT NOT NULL,
			model TEXT,
			in_tokens INTEGER NOT NULL DEFAULT 0,
			out_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_create_tokens INTEGER NOT NULL DEFAULT 0,
			cost_eur REAL,
			task_id TEXT REFERENCES tasks(id)
		)`); err != nil {
			t.Fatalf("create legacy events: %v", err)
		}
		if _, err := s0.db.Exec(`INSERT INTO projects (cwd, name) VALUES ('/p', 'p')`); err != nil {
			t.Fatalf("insert project: %v", err)
		}
		if _, err := s0.db.Exec(`INSERT INTO sessions (id, project_id, first_seen, last_seen) VALUES ('s1', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
			t.Fatalf("insert session: %v", err)
		}
		if _, err := s0.db.Exec(`INSERT INTO events (uuid, session_id, ts, type) VALUES ('u1', 's1', '2026-01-01T00:00:00Z', 'assistant')`); err != nil {
			t.Fatalf("insert event: %v", err)
		}
		if _, err := s0.db.Exec(`DROP TABLE events_old`); err != nil {
			t.Fatalf("drop old: %v", err)
		}
		_ = s0.Close()

		// Reopen — migration adds source with DEFAULT 'claude'.
		s1, err := Open(p)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer func() { _ = s1.Close() }()

		var src string
		if err := s1.db.QueryRow(`SELECT source FROM events WHERE uuid='u1'`).Scan(&src); err != nil {
			t.Fatalf("SELECT source: %v", err)
		}
		if src != "claude" {
			t.Errorf("want source='claude' got %q", src)
		}
	})
}
