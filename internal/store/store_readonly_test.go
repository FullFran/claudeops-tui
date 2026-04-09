package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenReadOnly(t *testing.T) {
	t.Run("opens existing DB in read-only mode", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "test.db")

		// Create a DB first with the regular Open.
		s, err := Open(p)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		_ = s.Close()

		// Now open read-only.
		ro, err := OpenReadOnly(p)
		if err != nil {
			t.Fatalf("OpenReadOnly: %v", err)
		}
		defer ro.Close()

		// Reads must work.
		var n int
		if err := ro.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
			t.Fatalf("read failed: %v", err)
		}
	})

	t.Run("fails on missing file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "nonexistent.db")

		_, err := OpenReadOnly(p)
		if err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
	})

	t.Run("rejects writes", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "test.db")

		// Create DB.
		s, err := Open(p)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		_ = s.Close()

		ro, err := OpenReadOnly(p)
		if err != nil {
			t.Fatalf("OpenReadOnly: %v", err)
		}
		defer ro.Close()

		ctx := context.Background()
		cost := 0.1
		ev := makeEvent("rw1", "sess-ro", "/p/x", "model", time.Now())
		err = ro.Insert(ctx, ev, &cost, nil)
		if err == nil {
			t.Fatal("expected write to fail on read-only store, got nil")
		}
	})
}
