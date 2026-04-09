package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/store"
)

// newTestStore creates a fresh in-process SQLite store for testing.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(p)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// insertEvent inserts a single event with the given cost into s.
func insertEvent(t *testing.T, s *store.Store, uuid, session, cwd, model string, ts time.Time, cost float64) {
	t.Helper()
	ev := store.Event{
		UUID:              uuid,
		SessionID:         session,
		CWD:               cwd,
		Type:              "assistant",
		Model:             model,
		TS:                ts,
		InTokens:          100,
		OutTokens:         200,
		CacheReadTokens:   50,
		CacheCreateTokens: 30,
	}
	if err := s.Insert(context.Background(), ev, &cost, nil); err != nil {
		t.Fatalf("Insert %s: %v", uuid, err)
	}
}
