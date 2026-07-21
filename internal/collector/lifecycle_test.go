package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestDrainOnShutdown covers #38: the last flush runs after ctx is cancelled,
// so it needs a fresh context or it ingests nothing.
func TestDrainOnShutdown(t *testing.T) {
	c, s, root := newTestCollector(t)
	p := writeJSONL(t, root, "-tmp-drain", "a.jsonl", sampleAssistant)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.drainOnShutdown(ctx, map[string]bool{p: true})

	if n := countEvents(t, s); n != 1 {
		t.Errorf("pending file not drained on shutdown: want 1 row got %d", n)
	}
}

// TestIngestExistingContinuesAfterFileError covers #38: one unreadable file
// must not abort the walk and take the whole watcher down with it.
func TestIngestExistingContinuesAfterFileError(t *testing.T) {
	c, s, root := newTestCollector(t)
	dir := mkProjectDir(t, root, "-tmp-walk")
	// A symlink to a directory is walked as a .jsonl file but fails on read.
	if err := os.Symlink(dir, filepath.Join(dir, "aaa.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeJSONL(t, root, "-tmp-walk", "zzz.jsonl", sampleAssistant)

	if err := c.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: want nil (per-file errors are skipped) got %v", err)
	}
	if n := countEvents(t, s); n != 1 {
		t.Errorf("remaining file not ingested: want 1 row got %d", n)
	}
	if c.FileErrorCount() != 1 {
		t.Errorf("file errors: want 1 got %d", c.FileErrorCount())
	}
}

// TestWatchNewDirectory covers #38: a project directory created after Watch
// started must be watched, including files already written inside it before
// the watch was added.
func TestWatchNewDirectory(t *testing.T) {
	t.Run("file created in a brand new directory", func(t *testing.T) {
		c, s, root := newTestCollector(t)
		startWatch(t, c)

		writeJSONL(t, root, "-tmp-new", "a.jsonl", sampleAssistant)

		if !waitFor(t, func() bool { return countEvents(t, s) == 1 }) {
			t.Fatalf("event in new directory never ingested: got %d rows", countEvents(t, s))
		}
	})

	t.Run("nested directories created at once", func(t *testing.T) {
		c, s, root := newTestCollector(t)
		startWatch(t, c)

		writeJSONL(t, root, filepath.Join("-tmp-outer", "inner"), "a.jsonl", sampleAssistant)

		if !waitFor(t, func() bool { return countEvents(t, s) == 1 }) {
			t.Fatalf("event in nested directory never ingested: got %d rows", countEvents(t, s))
		}
	})
}
