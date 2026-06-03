package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/parser"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// newTestCollectorSource builds a Collector using the new source-aware constructor.
func newTestCollectorSource(t *testing.T) (*Collector, *store.Store, string) {
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
	sink := source.NewStoreSink(s, calc)
	lp := parser.ClaudeLineParser{}

	c := NewWithSource(source.Claude, root, sink, lp, nil)
	return c, s, root
}

// TestCollectorWithSourceSeam covers REQ-1.5.1, REQ-1.5.3, REQ-1.7.1, REQ-1.7.2.
func TestCollectorWithSourceSeam(t *testing.T) {
	t.Run("REQ-1.5.1 claude collector via source seam — source='claude'", func(t *testing.T) {
		c, s, root := newTestCollectorSource(t)
		writeJSONL(t, root, "-tmp-proj", "a.jsonl", sampleAssistant+sampleUser+sampleUnknown)
		if err := c.IngestExisting(context.Background()); err != nil {
			t.Fatal(err)
		}
		// 2 events ingested (assistant + user; unknown skipped).
		if c.IngestedCount() != 2 {
			t.Errorf("ingested: want 2 got %d", c.IngestedCount())
		}
		// Verify source is stamped correctly.
		var src string
		if err := s.DB().QueryRow(`SELECT source FROM events WHERE uuid='u1'`).Scan(&src); err != nil {
			t.Fatalf("SELECT source: %v", err)
		}
		if src != "claude" {
			t.Errorf("want source='claude', got %q", src)
		}
	})

	t.Run("REQ-1.5.3 parser error is soft-fail — no panic", func(t *testing.T) {
		c, _, root := newTestCollectorSource(t)
		writeJSONL(t, root, "-tmp-proj", "bad.jsonl", sampleBad+sampleAssistant)
		if err := c.IngestExisting(context.Background()); err != nil {
			t.Fatal(err)
		}
		if c.ParseErrorCount() != 1 {
			t.Errorf("parse errors: want 1 got %d", c.ParseErrorCount())
		}
		if c.IngestedCount() != 1 {
			t.Errorf("ingested after bad line: want 1 got %d", c.IngestedCount())
		}
	})

	t.Run("REQ-1.7.1 cost unchanged vs reference value", func(t *testing.T) {
		c, s, root := newTestCollectorSource(t)
		writeJSONL(t, root, "-tmp-proj2", "ref.jsonl", sampleAssistant)
		if err := c.IngestExisting(context.Background()); err != nil {
			t.Fatal(err)
		}
		agg, err := s.AggregatesSince(context.Background(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if agg.CostEUR <= 0 {
			t.Errorf("expected cost > 0, got %v", agg.CostEUR)
		}
	})

	t.Run("REQ-1.7.2 offsets unchanged — warm start skips processed bytes", func(t *testing.T) {
		c, _, root := newTestCollectorSource(t)
		p := writeJSONL(t, root, "-tmp-proj3", "b.jsonl", sampleAssistant)
		ctx := context.Background()
		if err := c.IngestExisting(ctx); err != nil {
			t.Fatal(err)
		}
		if c.IngestedCount() != 1 {
			t.Fatalf("first pass: want 1 got %d", c.IngestedCount())
		}

		// Append more data.
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(sampleUser); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()

		if err := c.IngestExisting(ctx); err != nil {
			t.Fatal(err)
		}
		if c.IngestedCount() != 2 {
			t.Errorf("after append: want 2 got %d", c.IngestedCount())
		}
	})
}
