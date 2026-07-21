package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/codex"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
)

func TestSessionUUIDFromPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"codex rollout file", "/c/sessions/2026/01/15/rollout-20260115T120000Z-abc-123.jsonl", "abc-123"},
		{"plain session file", "/p/-tmp-proj/9f0e.jsonl", "9f0e"},
		{"rollout without uuid", "/c/rollout-20260115T120000Z.jsonl", "rollout-20260115T120000Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionUUIDFromPath(tc.path); got != tc.want {
				t.Errorf("want %q got %q", tc.want, got)
			}
		})
	}
}

// TestCodexFilesGetDistinctIdentity covers #32: without a per-file SessionUUID
// two rollout files synthesize the same uuid for an event at the same byte
// offset, and one silently overwrites the other.
func TestCodexFilesGetDistinctIdentity(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "sessions", "2026", "01", "15")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	tbl, err := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
	if err != nil {
		t.Fatal(err)
	}

	// Byte-identical files: every line sits at the same offset in both.
	const content = `{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"gpt-5","cwd":"/home/u/proj"}}` + "\n" +
		`{"timestamp":"2026-01-15T12:00:01Z","type":"token_count","payload":{"info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":50,"reasoning_output_tokens":10}}}}` + "\n"
	for _, name := range []string{
		"rollout-20260115T120000Z-1111-aaaa.jsonl",
		"rollout-20260115T130000Z-2222-bbbb.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sink := source.NewStoreSink(s, pricing.NewCalculator(tbl))
	c := NewWithSource(source.Codex, filepath.Join(dir, "sessions"), sink, codex.NewParser(), nil)
	c.store = s

	if err := c.IngestExisting(context.Background()); err != nil {
		t.Fatal(err)
	}

	var events, sessions int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`SELECT COUNT(DISTINCT session_id) FROM events`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Errorf("events: want 2 (one per file) got %d", events)
	}
	if sessions != 2 {
		t.Errorf("distinct sessions: want 2 got %d", sessions)
	}
}
