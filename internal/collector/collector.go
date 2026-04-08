// Package collector watches ~/.claude/projects for new JSONL lines and
// pipes them through the parser into the store. It is designed to run as a
// goroutine inside the TUI process; offsets are persisted in the store so a
// warm start does not re-ingest already-seen bytes.
package collector

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fullfran/claudeops-tui/internal/parser"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// TaskResolver resolves which task (if any) an event belongs to.
// Implemented by internal/tasks; collector accepts an interface to avoid the
// import cycle.
type TaskResolver interface {
	Resolve(sessionID string, ts time.Time) *string
}

// nopResolver is the default when the user has no task tracker wired up.
type nopResolver struct{}

func (nopResolver) Resolve(string, time.Time) *string { return nil }

// Collector ingests JSONL files into a store.
type Collector struct {
	root  string
	store *store.Store
	calc  *pricing.Calculator
	tasks TaskResolver

	// counters for diagnostics
	parseErrors atomic.Int64
	unknown     atomic.Int64
	ingested    atomic.Int64

	mu       sync.Mutex
	watching map[string]bool // file path → true
}

// New builds a Collector. `root` is typically ~/.claude/projects.
func New(root string, s *store.Store, calc *pricing.Calculator, tr TaskResolver) *Collector {
	if tr == nil {
		tr = nopResolver{}
	}
	return &Collector{
		root:     root,
		store:    s,
		calc:     calc,
		tasks:    tr,
		watching: map[string]bool{},
	}
}

// IngestedCount returns the number of events written so far. Used by tests.
func (c *Collector) IngestedCount() int64 { return c.ingested.Load() }

// ParseErrorCount returns the number of malformed lines skipped.
func (c *Collector) ParseErrorCount() int64 { return c.parseErrors.Load() }

// UnknownCount returns the number of unknown event types skipped.
func (c *Collector) UnknownCount() int64 { return c.unknown.Load() }

// IngestExisting walks `root` once and ingests every JSONL file found,
// honoring persisted offsets so it can be called repeatedly.
func (c *Collector) IngestExisting(ctx context.Context) error {
	offsets, err := c.store.LoadOffsets()
	if err != nil {
		return err
	}
	return filepath.WalkDir(c.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if err := c.ingestFile(ctx, path, offsets[path]); err != nil {
			return err
		}
		return nil
	})
}

// ingestFile reads from `start` to EOF, parses each line, and persists offsets.
func (c *Collector) ingestFile(ctx context.Context, path string, start int64) error {
	f, err := os.Open(path)
	if err != nil {
		return nil // file may have rotated; ignore
	}
	defer f.Close()
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return err
		}
	}
	br := bufio.NewReaderSize(f, 1<<20)
	off := start
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			off += int64(len(line))
			c.handleLine(ctx, line)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	stat, _ := f.Stat()
	var size int64
	if stat != nil {
		size = stat.Size()
	}
	return c.store.SaveOffset(path, off, size)
}

func (c *Collector) handleLine(ctx context.Context, line []byte) {
	// strip trailing newline before parsing
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	if len(line) == 0 {
		return
	}
	ev, err := parser.ParseLine(line)
	if err != nil {
		c.parseErrors.Add(1)
		return
	}
	switch e := ev.(type) {
	case parser.AssistantEvent:
		c.persistAssistant(ctx, e)
	case parser.UnknownEvent:
		c.unknown.Add(1)
	case parser.UserEvent:
		// no cost; we still record presence so dashboard can count prompts later
		c.persistUser(ctx, e)
	}
}

func (c *Collector) persistAssistant(ctx context.Context, e parser.AssistantEvent) {
	se := store.Event{
		UUID:              e.UUID,
		SessionID:         e.Session,
		CWD:               e.CWD,
		Type:              "assistant",
		Model:             e.Model,
		TS:                e.TS,
		InTokens:          e.InTokens,
		OutTokens:         e.OutTokens,
		CacheReadTokens:   e.CacheReadTokens,
		CacheCreateTokens: e.CacheCreateTokens,
	}
	var cost *float64
	if c.calc != nil {
		cost = c.calc.CostFor(e.Model, e.InTokens, e.OutTokens, e.CacheReadTokens, e.CacheCreateTokens)
	}
	taskID := c.tasks.Resolve(e.Session, e.TS)
	if err := c.store.Insert(ctx, se, cost, taskID); err == nil {
		c.ingested.Add(1)
	}
}

func (c *Collector) persistUser(ctx context.Context, e parser.UserEvent) {
	se := store.Event{
		UUID:      e.UUID,
		SessionID: e.Session,
		CWD:       e.CWD,
		Type:      "user",
		TS:        e.TS,
	}
	if err := c.store.Insert(ctx, se, nil, c.tasks.Resolve(e.Session, e.TS)); err == nil {
		c.ingested.Add(1)
	}
}
