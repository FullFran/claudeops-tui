package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/store"
)

func newTracker(t *testing.T) (*Tracker, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sidecar := filepath.Join(dir, "current-task.json")
	return New(sidecar, s), s, sidecar
}

func TestStartWritesSidecarAndDB(t *testing.T) {
	tr, s, sidecar := newTracker(t)
	ctx := context.Background()

	task, err := tr.Start(ctx, "refactor parser")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == "" || task.Name != "refactor parser" {
		t.Errorf("bad task: %+v", task)
	}
	if _, err := os.Stat(sidecar); err != nil {
		t.Errorf("sidecar missing: %v", err)
	}
	tasks, _ := s.TaskAggregates(ctx)
	if len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Errorf("DB task missing: %+v", tasks)
	}
}

func TestStopRemovesSidecarAndStampsEnd(t *testing.T) {
	tr, s, sidecar := newTracker(t)
	ctx := context.Background()

	if _, err := tr.Start(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	if err := tr.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Errorf("sidecar still exists")
	}
	tasks, _ := s.TaskAggregates(ctx)
	if tasks[0].EndedAt == nil {
		t.Errorf("EndedAt not stamped")
	}
}

func TestStartImplicitlyStopsPrevious(t *testing.T) {
	tr, s, _ := newTracker(t)
	ctx := context.Background()
	if _, err := tr.Start(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Start(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	tasks, _ := s.TaskAggregates(ctx)
	if len(tasks) != 2 {
		t.Errorf("want 2 tasks, got %d", len(tasks))
	}
	// Newest is first by ORDER BY started_at DESC.
	if tasks[0].Name != "second" || tasks[1].Name != "first" {
		t.Errorf("order/names wrong: %+v", tasks)
	}
	if tasks[1].EndedAt == nil {
		t.Errorf("first task should have EndedAt set")
	}
}

func TestResolveWithinAndOutsideWindow(t *testing.T) {
	tr, _, _ := newTracker(t)
	ctx := context.Background()
	task, _ := tr.Start(ctx, "x")

	if id := tr.Resolve("any-session", task.StartedAt.Add(time.Minute)); id == nil || *id != task.ID {
		t.Errorf("expected resolve to current task")
	}

	// Past max age → auto-stop and nil.
	if id := tr.Resolve("any", task.StartedAt.Add(5*time.Hour)); id != nil {
		t.Errorf("expected nil after expiry")
	}
	if _, ok := tr.Current(); ok {
		t.Errorf("expected no current task after auto-stop")
	}
}

func TestResolveNoTask(t *testing.T) {
	tr, _, _ := newTracker(t)
	if id := tr.Resolve("s", time.Now()); id != nil {
		t.Errorf("want nil")
	}
}
