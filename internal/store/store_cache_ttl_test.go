package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestMigrateAddCacheCreate1h covers the additive migration that gives events a
// cache_create_1h_tokens column: an install created before it existed must
// upgrade in place and keep every row it already had.
func TestMigrateAddCacheCreate1h(t *testing.T) {
	t.Run("an existing database gains the column and keeps its rows", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "legacy.db")

		s0, err := Open(p)
		if err != nil {
			t.Fatalf("initial Open: %v", err)
		}
		if err := s0.Insert(context.Background(), Event{
			UUID: "u1", SessionID: "s1", CWD: "/tmp/proj", Type: "assistant",
			Model: "claude-fable-5", TS: time.Now().UTC(), OutTokens: 7,
			CacheCreateTokens: 30000,
		}, nil, nil); err != nil {
			t.Fatalf("insert: %v", err)
		}
		// Drop the column to reproduce a pre-migration schema.
		if _, err := s0.db.Exec(`ALTER TABLE events DROP COLUMN cache_create_1h_tokens`); err != nil {
			t.Fatalf("drop column: %v", err)
		}
		_ = s0.Close()

		s1, err := Open(p)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer s1.Close()

		has, err := s1.columnExists("events", "cache_create_1h_tokens")
		if err != nil {
			t.Fatalf("columnExists: %v", err)
		}
		if !has {
			t.Fatal("migration did not add cache_create_1h_tokens")
		}
		var create, create1h int64
		if err := s1.db.QueryRow(
			`SELECT cache_create_tokens, cache_create_1h_tokens FROM events WHERE uuid = 'u1'`,
		).Scan(&create, &create1h); err != nil {
			t.Fatalf("the pre-existing row did not survive the migration: %v", err)
		}
		if create != 30000 {
			t.Errorf("cache_create_tokens = %d, want 30000", create)
		}
		if create1h != 0 {
			t.Errorf("cache_create_1h_tokens = %d, want 0 for a pre-migration row", create1h)
		}
	})

	t.Run("migration is idempotent", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "idem.db")
		s1, err := Open(p)
		if err != nil {
			t.Fatalf("first open: %v", err)
		}
		_ = s1.Close()
		s2, err := Open(p)
		if err != nil {
			t.Fatalf("second open: %v", err)
		}
		defer s2.Close()
		if has, _ := s2.columnExists("events", "cache_create_1h_tokens"); !has {
			t.Fatal("cache_create_1h_tokens missing after reopen")
		}
	})
}

func TestInsertPersistsTheOneHourCacheWriteSplit(t *testing.T) {
	s := newTestStore(t)
	ev := Event{
		UUID: "u1", SessionID: "s1", CWD: "/tmp/proj", Type: "assistant",
		Model: "claude-fable-5", TS: time.Now().UTC(), OutTokens: 7,
		CacheCreateTokens: 30000, CacheCreate1hTokens: 28583,
	}
	if err := s.Insert(context.Background(), ev, nil, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var got int64
	if err := s.db.QueryRow(`SELECT cache_create_1h_tokens FROM events WHERE uuid = 'u1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 28583 {
		t.Errorf("cache_create_1h_tokens = %d, want 28583", got)
	}

	// A later line for the same message carries more output and wins the
	// conflict; its 1h split must be carried over with the other counts.
	ev.OutTokens, ev.CacheCreate1hTokens = 9, 30000
	if err := s.Insert(context.Background(), ev, nil, nil); err != nil {
		t.Fatalf("re-insert: %v", err)
	}
	if err := s.db.QueryRow(`SELECT cache_create_1h_tokens FROM events WHERE uuid = 'u1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 30000 {
		t.Errorf("cache_create_1h_tokens after upsert = %d, want 30000", got)
	}
}
