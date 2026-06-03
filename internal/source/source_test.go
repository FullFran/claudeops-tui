package source_test

import (
	"context"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// stubSink is a test-only Sink that records emitted records.
type stubSink struct {
	emitted []source.Record
}

func (ss *stubSink) Emit(_ context.Context, r source.Record) error {
	ss.emitted = append(ss.emitted, r)
	return nil
}

// Compile-time check: *stubSink satisfies source.Sink.
var _ source.Sink = (*stubSink)(nil)

// TestSourcePackageTypes verifies REQ-1.5.1 / REQ-1.5.2:
// source.Record zero-value is valid, Name constants are defined,
// and the interface hierarchy compiles correctly.
func TestSourcePackageTypes(t *testing.T) {
	t.Run("Name constants defined", func(t *testing.T) {
		if source.Claude != "claude" {
			t.Errorf("want Claude='claude', got %q", source.Claude)
		}
		if source.Codex != "codex" {
			t.Errorf("want Codex='codex', got %q", source.Codex)
		}
		if source.Opencode != "opencode" {
			t.Errorf("want Opencode='opencode', got %q", source.Opencode)
		}
	})

	t.Run("Record zero value is valid", func(t *testing.T) {
		var r source.Record
		if r.Source != "" {
			t.Errorf("zero Source should be empty string, got %q", r.Source)
		}
		if !r.TS.IsZero() {
			t.Errorf("zero TS should be zero time")
		}
	})

	t.Run("stubSink satisfies Sink interface", func(t *testing.T) {
		ss := &stubSink{}
		r := source.Record{
			Source:    source.Claude,
			UUID:      "test-uuid",
			SessionID: "sess",
			CWD:       "/tmp",
			Type:      "assistant",
			TS:        time.Now(),
		}
		if err := ss.Emit(context.Background(), r); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		if len(ss.emitted) != 1 {
			t.Errorf("want 1 emitted record, got %d", len(ss.emitted))
		}
		if ss.emitted[0].Source != source.Claude {
			t.Errorf("source mismatch: %v", ss.emitted[0].Source)
		}
	})

	t.Run("LineContext has required fields", func(t *testing.T) {
		lc := source.LineContext{
			Path:        "/path/to/file.jsonl",
			LineOffset:  42,
			SessionUUID: "session-uuid",
			DefaultCWD:  "/home/user",
		}
		if lc.LineOffset != 42 {
			t.Errorf("LineOffset mismatch")
		}
	})
}
