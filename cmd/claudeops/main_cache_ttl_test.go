package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/collector"
	"github.com/fullfran/claudeops-tui/internal/parser"
	"github.com/fullfran/claudeops-tui/internal/source"
)

// The two lines carry identical usage; only the 1h/5m split differs. Anthropic
// bills a 1h cache write at 2x the base rate against 1.25x for a 5m one, so the
// second line must cost strictly more once the split survives the trip from the
// JSONL file to the events table.
const (
	cacheWrite5mLine = `{"type":"assistant","uuid":"u1","sessionId":"s1","cwd":"/tmp/proj","timestamp":"2026-07-21T10:00:00Z","message":{"id":"msg_5m","model":"claude-fable-5","usage":{"input_tokens":5,"output_tokens":7,"cache_creation_input_tokens":30000,"cache_read_input_tokens":11}}}`
	cacheWrite1hLine = `{"type":"assistant","uuid":"u2","sessionId":"s1","cwd":"/tmp/proj","timestamp":"2026-07-21T10:00:01Z","message":{"id":"msg_1h","model":"claude-fable-5","usage":{"input_tokens":5,"output_tokens":7,"cache_creation_input_tokens":30000,"cache_read_input_tokens":11,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":30000}}}}`
)

func TestOneHourCacheWritesCostMoreThanFiveMinuteOnes(t *testing.T) {
	p := newTestPaths(t)
	c, err := openCoreAt(p)
	if err != nil {
		t.Fatalf("openCoreAt: %v", err)
	}
	defer c.close()

	dir := filepath.Join(p.ClaudeProjects, "proj")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := cacheWrite5mLine + "\n" + cacheWrite1hLine + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	sink := source.NewStoreSink(c.store, c.calc)
	col := collector.NewWithSource(source.Claude, p.ClaudeProjects, sink, parser.ClaudeLineParser{}, nil)
	if err := col.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}

	cost5m := eventCost(t, c, "msg_5m")
	cost1h := eventCost(t, c, "msg_1h")
	if !(cost1h > cost5m) {
		t.Errorf("1h cache write cost %v, 5m cost %v; want the 1h write to cost more", cost1h, cost5m)
	}
	if got := eventCacheCreate1h(t, c, "msg_1h"); got != 30000 {
		t.Errorf("stored cache_create_1h_tokens = %d, want 30000", got)
	}
	if got := eventCacheCreate1h(t, c, "msg_5m"); got != 0 {
		t.Errorf("stored cache_create_1h_tokens = %d, want 0 for a line without a breakdown", got)
	}
}

func eventCost(t *testing.T, c *core, uuid string) float64 {
	t.Helper()
	var cost float64
	if err := c.store.DB().QueryRow(`SELECT cost_eur FROM events WHERE uuid = ?`, uuid).Scan(&cost); err != nil {
		t.Fatalf("cost for %s: %v", uuid, err)
	}
	return cost
}

func eventCacheCreate1h(t *testing.T, c *core, uuid string) int64 {
	t.Helper()
	var n int64
	if err := c.store.DB().QueryRow(`SELECT cache_create_1h_tokens FROM events WHERE uuid = ?`, uuid).Scan(&n); err != nil {
		t.Fatalf("cache_create_1h_tokens for %s: %v", uuid, err)
	}
	return n
}
