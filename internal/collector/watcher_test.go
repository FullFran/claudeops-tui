package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/fullfran/claudeops-tui/internal/store"
)

// mkProjectDir creates a project directory under root without writing files.
func mkProjectDir(t *testing.T, root, project string) string {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// countEvents returns how many rows the events table holds.
func countEvents(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// waitFor polls cond every 5ms until it returns true or the deadline expires.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// startWatch runs Watch in a goroutine and returns a stop func that cancels it
// and waits for the goroutine to return. It blocks until the watcher is
// registered so tests never race file creation against watcher setup.
func startWatch(t *testing.T, c *Collector) func() {
	t.Helper()
	ready := make(chan struct{})
	c.watchReady = func() { close(ready) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Watch(ctx)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("watcher never became ready")
	}
	stop := func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return stop
}

func TestWatchLiveIngestion(t *testing.T) {
	t.Run("ingests a file created after Watch starts", func(t *testing.T) {
		c, s, root := newTestCollector(t)
		mkProjectDir(t, root, "-tmp-live")
		startWatch(t, c)

		writeJSONL(t, root, "-tmp-live", "a.jsonl", sampleAssistant)

		if !waitFor(t, func() bool { return countEvents(t, s) == 1 }) {
			t.Fatalf("event never ingested: got %d rows", countEvents(t, s))
		}
	})

	t.Run("ingests bytes appended to an existing file", func(t *testing.T) {
		c, s, root := newTestCollector(t)
		p := writeJSONL(t, root, "-tmp-live", "a.jsonl", sampleAssistant)
		startWatch(t, c)

		if !waitFor(t, func() bool { return countEvents(t, s) == 1 }) {
			t.Fatalf("cold start event missing: got %d rows", countEvents(t, s))
		}

		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(sampleUser); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()

		if !waitFor(t, func() bool { return countEvents(t, s) == 2 }) {
			t.Fatalf("appended event never ingested: got %d rows", countEvents(t, s))
		}
	})

	t.Run("ignores non-jsonl files", func(t *testing.T) {
		c, s, root := newTestCollector(t)
		mkProjectDir(t, root, "-tmp-live")
		startWatch(t, c)

		writeJSONL(t, root, "-tmp-live", "notes.txt", sampleAssistant)
		writeJSONL(t, root, "-tmp-live", "a.jsonl", sampleAssistant)

		if !waitFor(t, func() bool { return countEvents(t, s) == 1 }) {
			t.Fatalf("want exactly the jsonl event, got %d rows", countEvents(t, s))
		}
	})
}

func TestAddDirsRecursively(t *testing.T) {
	t.Run("registers the root and every nested directory", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "a", "b", "c")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		w, err := fsnotify.NewWatcher()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = w.Close() }()

		if err := addDirsRecursively(w, root); err != nil {
			t.Fatalf("addDirsRecursively: %v", err)
		}
		want := []string{root, filepath.Join(root, "a"), filepath.Join(root, "a", "b"), nested}
		got := map[string]bool{}
		for _, p := range w.WatchList() {
			got[p] = true
		}
		for _, p := range want {
			if !got[p] {
				t.Errorf("directory not watched: %s", p)
			}
		}
	})

	t.Run("creates a missing root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "missing")
		w, err := fsnotify.NewWatcher()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = w.Close() }()

		if err := addDirsRecursively(w, root); err != nil {
			t.Fatalf("addDirsRecursively: %v", err)
		}
		if !isDir(root) {
			t.Errorf("root was not created: %s", root)
		}
	})
}

func TestIsDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.jsonl")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"directory", dir, true},
		{"regular file", file, false},
		{"missing path", filepath.Join(dir, "nope"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDir(tc.path); got != tc.want {
				t.Errorf("isDir(%s): want %v got %v", tc.path, tc.want, got)
			}
		})
	}
}
