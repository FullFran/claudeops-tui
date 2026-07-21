package collector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/parser"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// flakySink fails Emit for one uuid until healed, delegating everything else.
type flakySink struct {
	inner    *source.StoreSink
	failUUID string
}

func (f *flakySink) Emit(ctx context.Context, r source.Record) error {
	if f.failUUID != "" && r.UUID == f.failUUID {
		return errors.New("database is locked")
	}
	return f.inner.Emit(ctx, r)
}

// newFlakyCollector wires a source-seam Collector whose sink can be made to
// fail, while offsets still go to the real store.
func newFlakyCollector(t *testing.T) (*Collector, *flakySink, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
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
	sink := &flakySink{inner: source.NewStoreSink(s, pricing.NewCalculator(tbl))}
	c := NewWithSource(source.Claude, root, sink, parser.ClaudeLineParser{}, nil)
	c.store = s
	return c, sink, s, root
}

// TestOffsetHeldOnEmitFailure covers #35: a transient store failure must not
// move the persisted offset past the event, so the next pass retries it.
func TestOffsetHeldOnEmitFailure(t *testing.T) {
	ctx := context.Background()
	c, sink, s, root := newFlakyCollector(t)
	sink.failUUID = "u2" // the user event on the second line
	p := writeJSONL(t, root, "-tmp-flaky", "a.jsonl", sampleAssistant+sampleUser+sampleUnknown)

	if err := c.IngestExisting(ctx); err != nil {
		t.Fatal(err)
	}
	if c.IngestedCount() != 1 {
		t.Errorf("ingested: want 1 got %d", c.IngestedCount())
	}
	if c.EmitErrorCount() != 1 {
		t.Errorf("emit errors: want 1 got %d", c.EmitErrorCount())
	}
	offsets, _ := s.LoadOffsets()
	if got, want := offsets[p], int64(len(sampleAssistant)); got != want {
		t.Errorf("offset: want %d (start of the failed event) got %d", want, got)
	}

	// The store recovers; the held offset lets the next pass pick the event up.
	sink.failUUID = ""
	if err := c.IngestExisting(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE uuid='u2'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("retried event: want 1 row got %d", n)
	}
	if got, want := mustOffset(t, s, p), int64(len(sampleAssistant+sampleUser+sampleUnknown)); got != want {
		t.Errorf("offset after recovery: want %d got %d", want, got)
	}
}

// TestUnstorableRecordDoesNotStallFile covers the other half of #35: an event
// that can never be stored must be dropped, not retried forever.
func TestUnstorableRecordDoesNotStallFile(t *testing.T) {
	ctx := context.Background()
	c, _, s, root := newFlakyCollector(t)
	// A claude assistant line with an empty cwd is rejected by the sink forever.
	const noCWD = `{"type":"assistant","uuid":"u-nocwd","sessionId":"s1","cwd":"","timestamp":"2026-04-08T13:10:57.123Z","message":{"id":"msg_nocwd","model":"claude-opus-4-6","usage":{"input_tokens":5,"output_tokens":10}}}` + "\n"
	writeJSONL(t, root, "-tmp-reject", "a.jsonl", noCWD+sampleAssistant)

	if err := c.IngestExisting(ctx); err != nil {
		t.Fatal(err)
	}
	if c.IngestedCount() != 1 {
		t.Errorf("ingested: want 1 (the line after the rejected one) got %d", c.IngestedCount())
	}
	if c.EmitErrorCount() != 1 {
		t.Errorf("emit errors: want 1 got %d", c.EmitErrorCount())
	}
	if n := countEvents(t, s); n != 1 {
		t.Errorf("event rows: want 1 got %d", n)
	}
}

// mustOffset returns the persisted offset for path.
func mustOffset(t *testing.T, s *store.Store, path string) int64 {
	t.Helper()
	offsets, err := s.LoadOffsets()
	if err != nil {
		t.Fatal(err)
	}
	return offsets[path]
}
