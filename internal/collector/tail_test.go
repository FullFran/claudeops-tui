package collector

import (
	"context"
	"os"
	"testing"
)

// appendTo appends raw bytes to an existing file.
func appendTo(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestPartialTrailingLine covers #36: a line whose newline has not been flushed
// yet must not be consumed, and the persisted offset must stay at its start so
// the next pass re-reads it whole.
func TestPartialTrailingLine(t *testing.T) {
	c, s, root := newTestCollector(t)
	ctx := context.Background()

	half := len(sampleUser) / 2
	p := writeJSONL(t, root, "-tmp-partial", "a.jsonl", sampleAssistant+sampleUser[:half])

	if err := c.IngestExisting(ctx); err != nil {
		t.Fatal(err)
	}
	if c.IngestedCount() != 1 {
		t.Errorf("ingested: want 1 got %d", c.IngestedCount())
	}
	if c.ParseErrorCount() != 0 {
		t.Errorf("parse errors: want 0 (fragment not consumed) got %d", c.ParseErrorCount())
	}
	offsets, _ := s.LoadOffsets()
	if got, want := offsets[p], int64(len(sampleAssistant)); got != want {
		t.Errorf("offset: want %d (start of the incomplete line) got %d", want, got)
	}

	// The writer flushes the rest of the line.
	appendTo(t, p, sampleUser[half:])

	if err := c.IngestExisting(ctx); err != nil {
		t.Fatal(err)
	}
	if c.IngestedCount() != 2 {
		t.Errorf("ingested after flush: want 2 got %d", c.IngestedCount())
	}
	if c.ParseErrorCount() != 0 {
		t.Errorf("parse errors after flush: want 0 got %d", c.ParseErrorCount())
	}
	if n := countEvents(t, s); n != 2 {
		t.Errorf("event rows: want 2 got %d", n)
	}
}
