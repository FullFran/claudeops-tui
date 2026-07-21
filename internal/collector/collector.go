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
	"strings"
	"sync/atomic"
	"time"

	"github.com/fullfran/claudeops-tui/internal/parser"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
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

	// source seam fields (set by NewWithSource; nil when using legacy New)
	sourceName source.Name
	sink       source.Sink
	lineParser source.LineParser

	// counters for diagnostics
	parseErrors atomic.Int64
	unknown     atomic.Int64
	ingested    atomic.Int64
	emitErrors  atomic.Int64
	fileErrors  atomic.Int64

	// watchReady, when non-nil, is called once Watch has registered every
	// directory. Tests use it to avoid racing file creation against setup.
	watchReady func()
}

// New builds a Collector using the classic direct-store path.
// `root` is typically ~/.claude/projects.
// This constructor is preserved for backward compatibility.
func New(root string, s *store.Store, calc *pricing.Calculator, tr TaskResolver) *Collector {
	if tr == nil {
		tr = nopResolver{}
	}
	return &Collector{
		root:  root,
		store: s,
		calc:  calc,
		tasks: tr,
	}
}

// NewWithSource builds a source-aware Collector.
// All event processing goes through lp (LineParser) and sk (Sink).
// The store is still used for offset persistence; pricing and insert logic
// live behind the Sink. tr may be nil.
func NewWithSource(name source.Name, root string, sk source.Sink, lp source.LineParser, tr TaskResolver) *Collector {
	if tr == nil {
		tr = nopResolver{}
	}
	return &Collector{
		root:       root,
		sourceName: name,
		sink:       sk,
		lineParser: lp,
		tasks:      tr,
	}
}

// IngestedCount returns the number of events written so far. Used by tests.
func (c *Collector) IngestedCount() int64 { return c.ingested.Load() }

// ParseErrorCount returns the number of malformed lines skipped.
func (c *Collector) ParseErrorCount() int64 { return c.parseErrors.Load() }

// UnknownCount returns the number of unknown event types skipped.
func (c *Collector) UnknownCount() int64 { return c.unknown.Load() }

// EmitErrorCount returns the number of events whose write to the sink failed.
func (c *Collector) EmitErrorCount() int64 { return c.emitErrors.Load() }

// FileErrorCount returns the number of files skipped because they could not be
// read. They are retried on the next pass.
func (c *Collector) FileErrorCount() int64 { return c.fileErrors.Load() }

// IngestExisting walks `root` once and ingests every JSONL file found,
// honoring persisted offsets so it can be called repeatedly.
func (c *Collector) IngestExisting(ctx context.Context) error {
	offsets, err := c.loadOffsets()
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
			if ctx.Err() != nil {
				return err
			}
			// A single unreadable file must not abort the walk — it keeps its
			// offset and is retried on the next pass.
			c.fileErrors.Add(1)
		}
		return nil
	})
}

// loadOffsets returns persisted byte offsets per file.
// When using the source seam (sink+lineParser), the Collector may not have a
// store directly; in that case we need a way to persist offsets. We extract the
// store from the StoreSink if possible, or fall back to an empty map (stateless).
func (c *Collector) loadOffsets() (map[string]int64, error) {
	if c.store != nil {
		return c.store.LoadOffsets()
	}
	// Source-seam path: extract store from StoreSink if available.
	if ss, ok := c.sink.(*source.StoreSink); ok {
		return ss.Store().LoadOffsets()
	}
	return map[string]int64{}, nil
}

// saveOffset persists how many bytes have been processed from a file.
func (c *Collector) saveOffset(path string, offset, size int64) error {
	if c.store != nil {
		return c.store.SaveOffset(path, offset, size)
	}
	if ss, ok := c.sink.(*source.StoreSink); ok {
		return ss.Store().SaveOffset(path, offset, size)
	}
	return nil // stateless sink; silently ignore
}

// ingestFile reads from `start` to EOF, parses each line, and persists offsets.
func (c *Collector) ingestFile(ctx context.Context, path string, start int64) error {
	f, err := os.Open(path)
	if err != nil {
		return nil // file may have rotated; ignore
	}
	defer f.Close()
	if stat, err := f.Stat(); err == nil && start > stat.Size() {
		// Truncation, rotation or an editor atomic save left the stored offset
		// past EOF; re-read from the beginning (uuid dedup makes that safe).
		start = 0
	}
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return err
		}
	}
	br := bufio.NewReaderSize(f, 1<<20)
	fileCtx := c.fileContext(path)
	off := start
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := br.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(line) == 0 || line[len(line)-1] != '\n' {
			// The writer has not flushed the newline yet: leave the fragment
			// unconsumed so the next pass re-reads the line whole.
			break
		}
		if !c.handleLine(ctx, fileCtx, off, line) {
			// The sink refused an event for a reason that may clear up: hold
			// the offset at this line so the next pass retries it.
			break
		}
		off += int64(len(line))
		if errors.Is(err, io.EOF) {
			break
		}
	}
	stat, _ := f.Stat()
	var size int64
	if stat != nil {
		size = stat.Size()
	}
	return c.saveOffset(path, off, size)
}

// handleLine processes one newline-terminated line. It reports false when the
// line could not be persisted for a reason that may clear up later, so the
// caller keeps the offset at this line instead of skipping the event.
func (c *Collector) handleLine(ctx context.Context, fileCtx source.LineContext, lineStart int64, line []byte) bool {
	// strip trailing newline before parsing
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	if len(line) == 0 {
		return true
	}

	// Source-seam path: use LineParser + Sink.
	if c.lineParser != nil && c.sink != nil {
		lctx := fileCtx
		lctx.LineOffset = lineStart
		records, err := c.lineParser.ParseLine(line, lctx)
		if err != nil {
			c.parseErrors.Add(1)
			return true
		}
		for _, r := range records {
			if isPermanentReject(r) {
				c.emitErrors.Add(1)
				continue
			}
			if err := c.sink.Emit(ctx, r); err != nil {
				c.emitErrors.Add(1)
				return false
			}
			c.ingested.Add(1)
		}
		if len(records) == 0 {
			// Unknown/no-usage line — count as unknown.
			c.unknown.Add(1)
		}
		return true
	}

	// Legacy path: direct parser + store (unchanged behavior).
	ev, err := parser.ParseLine(line)
	if err != nil {
		c.parseErrors.Add(1)
		return true
	}
	switch e := ev.(type) {
	case parser.AssistantEvent:
		return c.persistAssistant(ctx, e)
	case parser.UnknownEvent:
		c.unknown.Add(1)
	case parser.UserEvent:
		// no cost; we still record presence so dashboard can count prompts later
		return c.persistUser(ctx, e)
	}
	return true
}

// fileContext derives the per-file identity a source parser needs to synthesize
// stable ids. Sources like Codex carry no session id inside their events, so it
// comes from the file name; leaving it empty collapses every file onto one
// session and makes equal byte offsets collide on the same uuid.
func (c *Collector) fileContext(path string) source.LineContext {
	sessionUUID := sessionUUIDFromPath(path)
	lc := source.LineContext{Path: path, SessionUUID: sessionUUID}
	if c.sourceName != "" && c.sourceName != source.Claude {
		// Claude carries cwd per event; other sources fall back to a stable
		// per-session project key.
		lc.DefaultCWD = string(c.sourceName) + ":" + sessionUUID
	}
	return lc
}

// sessionUUIDFromPath extracts the session identity from a file name:
// "rollout-<timestamp>-<uuid>.jsonl" yields <uuid>, anything else yields the
// name without its extension.
func sessionUUIDFromPath(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if rest, ok := strings.CutPrefix(name, "rollout-"); ok {
		if _, uuid, found := strings.Cut(rest, "-"); found && uuid != "" {
			return uuid
		}
	}
	return name
}

// isPermanentReject reports whether a record can never be stored, however many
// times it is retried. Those events are dropped so one malformed line cannot
// stall a whole file.
func isPermanentReject(r source.Record) bool {
	if r.UUID == "" || r.SessionID == "" {
		return true
	}
	return r.Source == source.Claude && r.CWD == ""
}

func (c *Collector) persistAssistant(ctx context.Context, e parser.AssistantEvent) bool {
	se := store.Event{
		UUID:              e.DedupUUID(),
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
	return c.insert(ctx, se, cost, taskID)
}

func (c *Collector) persistUser(ctx context.Context, e parser.UserEvent) bool {
	se := store.Event{
		UUID:      e.UUID,
		SessionID: e.Session,
		CWD:       e.CWD,
		Type:      "user",
		TS:        e.TS,
	}
	return c.insert(ctx, se, nil, c.tasks.Resolve(e.Session, e.TS))
}

// insert writes one event through the legacy store path, applying the same
// permanent-reject rule as the source-seam path.
func (c *Collector) insert(ctx context.Context, se store.Event, cost *float64, taskID *string) bool {
	if se.UUID == "" || se.SessionID == "" || se.CWD == "" {
		c.emitErrors.Add(1)
		return true
	}
	if err := c.store.Insert(ctx, se, cost, taskID); err != nil {
		c.emitErrors.Add(1)
		return false
	}
	c.ingested.Add(1)
	return true
}
