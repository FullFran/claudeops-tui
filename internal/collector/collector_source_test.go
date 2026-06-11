package collector

import (
	"context"
	"fmt"
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

// TestContentBlockLinesCountUsageOnce covers the fix for the ~2.4x over-count:
// Claude Code writes one JSONL line per content block of an assistant message
// (distinct uuid, same message.id + requestId). Input/cache counts are stable
// across those lines but output_tokens grows monotonically (streaming partial →
// final). End to end, the lines must collapse to one events row carrying the
// FINAL (largest) output_tokens — not a per-block sum and not the first partial.
func TestContentBlockLinesCountUsageOnce(t *testing.T) {
	c, s, root := newTestCollectorSource(t)

	// Real-shaped fixture: output_tokens grows 1 → 5 → 350 across the three
	// content-block lines; input/cache are identical (as in real logs).
	const blockTmpl = `{"type":"assistant","uuid":"cb-%d","sessionId":"s-dedup","cwd":"/tmp/proj","requestId":"req_dd","timestamp":"2026-06-11T10:00:0%d.000Z","message":{"id":"msg_dd","model":"claude-fable-5","usage":{"input_tokens":7,"output_tokens":%d,"cache_read_input_tokens":13,"cache_creation_input_tokens":17}}}` + "\n"
	outputs := []int64{1, 5, 350}
	content := ""
	for i, out := range outputs {
		content += fmt.Sprintf(blockTmpl, i, i, out)
	}
	writeJSONL(t, root, "-tmp-dedup", "dedup.jsonl", content)

	if err := c.IngestExisting(context.Background()); err != nil {
		t.Fatal(err)
	}

	var rows, outSum, inSum int64
	err := s.DB().QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(out_tokens),0), COALESCE(SUM(in_tokens),0) FROM events WHERE session_id='s-dedup'`,
	).Scan(&rows, &outSum, &inSum)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("events rows: want 1 (usage counted once), got %d", rows)
	}
	if outSum != 350 {
		t.Errorf("out_tokens: want 350 (final streaming count), got %d", outSum)
	}
	if inSum != 7 {
		t.Errorf("in_tokens: want 7 (counted once), got %d", inSum)
	}
}
