// Package tasks tracks the user's "current task" via a sidecar JSON file
// and resolves which task an event belongs to.
//
// Sidecar shape (~/.claudeops/current-task.json):
//
//	{
//	  "id":         "<uuid>",
//	  "name":       "refactor parser",
//	  "started_at": "2026-04-08T13:11:00Z",
//	  "max_age_seconds": 14400
//	}
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fullfran/claudeops-tui/internal/store"
)

// Task is the in-memory representation.
type Task struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	StartedAt     time.Time     `json:"started_at"`
	MaxAge        time.Duration `json:"-"`
	MaxAgeSeconds int64         `json:"max_age_seconds"`
}

// DefaultMaxAge is the cap on how long a task auto-stays-active.
const DefaultMaxAge = 4 * time.Hour

// Tracker is the public API used by the collector and CLI.
type Tracker struct {
	path  string
	store *store.Store

	mu  sync.Mutex
	cur *Task
}

// New builds a Tracker. `sidecar` is the path to current-task.json.
func New(sidecar string, s *store.Store) *Tracker {
	return &Tracker{path: sidecar, store: s}
}

// Load reads the sidecar from disk into memory. Returns nil if absent.
func (t *Tracker) Load() error {
	b, err := os.ReadFile(t.path)
	if errors.Is(err, os.ErrNotExist) {
		t.mu.Lock()
		t.cur = nil
		t.mu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	var task Task
	if err := json.Unmarshal(b, &task); err != nil {
		return err
	}
	if task.MaxAgeSeconds > 0 {
		task.MaxAge = time.Duration(task.MaxAgeSeconds) * time.Second
	} else {
		task.MaxAge = DefaultMaxAge
		task.MaxAgeSeconds = int64(DefaultMaxAge.Seconds())
	}
	t.mu.Lock()
	t.cur = &task
	t.mu.Unlock()
	return nil
}

// Current returns the active task (if any), reloading from disk so external
// edits via the CLI are picked up by long-running processes.
func (t *Tracker) Current() (*Task, bool) {
	_ = t.Load()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cur == nil {
		return nil, false
	}
	cp := *t.cur
	return &cp, true
}

// Start begins a new task. If one already exists it is implicitly stopped.
func (t *Tracker) Start(ctx context.Context, name string) (*Task, error) {
	if name == "" {
		return nil, errors.New("task name required")
	}
	if _, ok := t.Current(); ok {
		if err := t.Stop(ctx); err != nil {
			return nil, err
		}
	}
	task := &Task{
		ID:            uuid.NewString(),
		Name:          name,
		StartedAt:     time.Now().UTC(),
		MaxAge:        DefaultMaxAge,
		MaxAgeSeconds: int64(DefaultMaxAge.Seconds()),
	}
	if err := t.write(task); err != nil {
		return nil, err
	}
	if t.store != nil {
		if err := t.store.UpsertTask(ctx, task.ID, task.Name, task.StartedAt, task.MaxAge); err != nil {
			return nil, err
		}
	}
	t.mu.Lock()
	t.cur = task
	t.mu.Unlock()
	return task, nil
}

// Stop ends the current task: removes the sidecar and stamps ended_at.
func (t *Tracker) Stop(ctx context.Context) error {
	t.mu.Lock()
	cur := t.cur
	t.cur = nil
	t.mu.Unlock()
	if cur == nil {
		// fall back to disk
		if err := t.Load(); err != nil {
			return err
		}
		t.mu.Lock()
		cur = t.cur
		t.cur = nil
		t.mu.Unlock()
	}
	if err := os.Remove(t.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if cur != nil && t.store != nil {
		if err := t.store.EndTask(ctx, cur.ID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

// Resolve returns the task id an event with (sessionID, ts) belongs to,
// or nil if there's no active task or the active task has expired.
// Side effect: an expired task is auto-stopped.
func (t *Tracker) Resolve(_ string, ts time.Time) *string {
	cur, ok := t.Current()
	if !ok {
		return nil
	}
	if ts.After(cur.StartedAt.Add(cur.MaxAge)) {
		_ = t.Stop(context.Background())
		return nil
	}
	id := cur.ID
	return &id
}

func (t *Tracker) write(task *Task) error {
	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.path, b, 0o600)
}
