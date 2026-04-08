package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/store"
)

const sampleAssistant = `{"type":"assistant","uuid":"u1","sessionId":"s1","cwd":"/p","version":"2.1.96","timestamp":"2026-04-08T13:10:57.123Z","message":{"role":"assistant","model":"claude-opus-4-6","usage":{"input_tokens":5,"cache_creation_input_tokens":20780,"cache_read_input_tokens":15718,"output_tokens":1101}}}
`

const sampleUser = `{"type":"user","uuid":"u2","sessionId":"s1","cwd":"/p","timestamp":"2026-04-08T13:11:00Z"}
`

const sampleUnknown = `{"type":"permission-mode","uuid":"u3","sessionId":"s1","cwd":"/p","timestamp":"2026-04-08T13:11:01Z"}
`

const sampleBad = `{not json
`

func newTestCollector(t *testing.T) (*Collector, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	_ = os.MkdirAll(root, 0o755)
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	tbl, err := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	calc := pricing.NewCalculator(tbl)

	c := New(root, s, calc, nil)
	return c, s, root
}

func writeJSONL(t *testing.T, root, project, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestColdStartIngestsExisting(t *testing.T) {
	c, s, root := newTestCollector(t)
	writeJSONL(t, root, "-tmp-proj", "a.jsonl", sampleAssistant+sampleUser+sampleUnknown+sampleBad)

	if err := c.IngestExisting(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.IngestedCount() != 2 {
		t.Errorf("ingested: want 2 got %d", c.IngestedCount())
	}
	if c.UnknownCount() != 1 {
		t.Errorf("unknown: want 1 got %d", c.UnknownCount())
	}
	if c.ParseErrorCount() != 1 {
		t.Errorf("parse errors: want 1 got %d", c.ParseErrorCount())
	}

	agg, err := s.AggregatesSince(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if agg.Events != 2 {
		t.Errorf("agg events want 2 got %d", agg.Events)
	}
	if agg.CostEUR <= 0 {
		t.Errorf("expected cost > 0, got %v", agg.CostEUR)
	}

	offsets, _ := s.LoadOffsets()
	if len(offsets) != 1 {
		t.Errorf("offsets: want 1 got %d", len(offsets))
	}
	for _, off := range offsets {
		if off == 0 {
			t.Errorf("offset should be > 0")
		}
	}
}

func TestWarmStartReadsOnlyNewBytes(t *testing.T) {
	c, s, root := newTestCollector(t)
	p := writeJSONL(t, root, "-tmp-proj", "a.jsonl", sampleAssistant)
	ctx := context.Background()
	if err := c.IngestExisting(ctx); err != nil {
		t.Fatal(err)
	}
	if c.IngestedCount() != 1 {
		t.Fatalf("first pass: want 1 got %d", c.IngestedCount())
	}

	// Append more data (simulating Claude Code adding lines).
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(sampleUser); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// New collector + new store would re-ingest from offset.
	if err := c.IngestExisting(ctx); err != nil {
		t.Fatal(err)
	}
	if c.IngestedCount() != 2 {
		t.Errorf("after append: want 2 got %d", c.IngestedCount())
	}

	agg, _ := s.AggregatesSince(ctx, time.Time{})
	if agg.Events != 2 {
		t.Errorf("agg: want 2 got %d", agg.Events)
	}
}
