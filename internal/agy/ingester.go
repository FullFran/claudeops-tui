package agy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fullfran/claudeops-tui/internal/source"
)

const (
	// defaultPollInterval mirrors internal/opencode's poll cadence.
	defaultPollInterval = 5 * time.Second

	conversationsDirName = "conversations"
	summariesDBFileName  = "conversation_summaries.db"

	// recentWriteWindow bounds how "still being written" is judged for the
	// timestamp-resolution deferral: a conversation file modified more
	// recently than this is assumed to still be mid-write for its newest
	// step, so an unresolved row is deferred to the next poll rather than
	// dated from the file's mtime. See pollFile / resolveTimestamp.
	recentWriteWindow = 2 * time.Minute
)

// WatermarkStore persists the per-conversation polling watermark. Implemented
// by *store.Store in production — its existing string-keyed
// source_watermarks table needs no schema change, since each conversation
// gets its own key "agy:<conversation-uuid>" — and by a fake in tests.
type WatermarkStore interface {
	LoadSourceWatermark(src string) (string, error)
	SaveSourceWatermark(src, position string) error
}

// fileSnapshot is the (size, mtime) pair the poll loop compares to decide
// whether a conversation file needs rereading. It covers the WAL sidecar too:
// a WAL-mode write can grow "-wal" without touching the main file's mtime at
// all, so watching the main file alone would miss it.
type fileSnapshot struct {
	size, walSize   int64
	mtime, walMtime time.Time
	walPresent      bool
}

func statSnapshot(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	snap := fileSnapshot{size: info.Size(), mtime: info.ModTime()}
	if walInfo, err := os.Stat(path + "-wal"); err == nil {
		snap.walPresent = true
		snap.walSize = walInfo.Size()
		snap.walMtime = walInfo.ModTime()
	}
	return snap, nil
}

func (a fileSnapshot) equal(b fileSnapshot) bool {
	return a.size == b.size && a.mtime.Equal(b.mtime) &&
		a.walPresent == b.walPresent && a.walSize == b.walSize && a.walMtime.Equal(b.walMtime)
}

// Ingester implements source.Ingester for agy's per-conversation SQLite
// files. Unlike opencode's single shared database, agy has one file per
// conversation under <root>/conversations, so a poll fans out across all of
// them, tracks a watermark per conversation, and keeps one file's failure
// from blocking the others.
type Ingester struct {
	root         string
	wm           WatermarkStore
	sink         source.Sink
	pollInterval time.Duration

	// OnWarn receives a message for a non-fatal problem this poll recovered
	// from (a malformed row, an unresolved timestamp that fell back to the
	// file's mtime). Defaults to stderr when nil, mirroring
	// pricing.Calculator.OnWarn.
	OnWarn func(msg string)

	mu       sync.Mutex
	lastErr  error
	failures int

	wsMu       sync.Mutex
	workspaces map[string]string // conversation id -> workspace path, best-effort

	seenMu sync.Mutex
	seen   map[string]fileSnapshot // conversation db path -> snapshot at last full, undeferred read

	// Counters for `claudeops ingest`, which reports them per run. They match
	// the collector's accessors so both kinds of source print the same line.
	ingested    atomic.Int64
	parseErrors atomic.Int64
}

// NewIngester creates an Ingester rooted at agy's data directory (root is the
// directory that directly contains "conversations/" — what DefaultRoot
// returns).
func NewIngester(root string, wm WatermarkStore, sink source.Sink) *Ingester {
	return &Ingester{
		root:         root,
		wm:           wm,
		sink:         sink,
		pollInterval: defaultPollInterval,
	}
}

// Name implements source.Ingester.
func (ing *Ingester) Name() source.Name { return source.Agy }

// LastErr reports the outcome of the most recent poll, mirroring
// internal/opencode.Ingester.LastErr — see its doc comment for why this
// exists (Watch swallows poll's return value, so this is the only place a
// persistent failure stays visible).
func (ing *Ingester) LastErr() error {
	ing.mu.Lock()
	defer ing.mu.Unlock()
	return ing.lastErr
}

// ConsecutiveFailures reports how many polls in a row have failed, counted by
// poll itself for the same reason internal/opencode.Ingester counts it: a
// watchdog sampling on its own clock cannot otherwise tell one failure seen
// three times from three separate failures.
func (ing *Ingester) ConsecutiveFailures() int {
	ing.mu.Lock()
	defer ing.mu.Unlock()
	return ing.failures
}

func (ing *Ingester) setErr(err error) error {
	ing.mu.Lock()
	ing.lastErr = err
	if err != nil {
		ing.failures++
	} else {
		ing.failures = 0
	}
	ing.mu.Unlock()
	return err
}

func (ing *Ingester) warn(msg string) {
	if ing.OnWarn != nil {
		ing.OnWarn(msg)
		return
	}
	fmt.Fprintln(os.Stderr, "claudeops:", msg)
}

// IngestedCount reports how many records this Ingester has emitted.
func (ing *Ingester) IngestedCount() int64 { return ing.ingested.Load() }

// ParseErrorCount reports how many gen_metadata rows were skipped as malformed.
func (ing *Ingester) ParseErrorCount() int64 { return ing.parseErrors.Load() }

// IngestExisting implements source.Ingester: one-shot drain from every
// conversation's watermark.
func (ing *Ingester) IngestExisting(ctx context.Context) error {
	return ing.poll(ctx)
}

// Watch implements source.Ingester: poll loop until ctx.Done. Like
// internal/opencode.Ingester.Watch, it never returns on a failing poll —
// supervisePollErrors (cmd/claudeops/health.go) is what surfaces a poll that
// keeps failing, via LastErr/ConsecutiveFailures.
func (ing *Ingester) Watch(ctx context.Context) error {
	for {
		_ = ing.poll(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ing.pollInterval):
		}
	}
}

// poll lists every conversation file, refreshes the best-effort workspace
// map, and reads each file past its own watermark. One file's failure is
// recorded and skipped rather than aborting the rest — a single corrupted
// conversation should not blind claudeops to every other one.
func (ing *Ingester) poll(ctx context.Context) error {
	paths, err := ing.listConversationDBs()
	if err != nil {
		if os.IsNotExist(err) {
			// agy is not installed — a legitimate no-op, not a failure.
			return ing.setErr(nil)
		}
		return ing.setErr(fmt.Errorf("agy: list conversations: %w", err))
	}

	ing.loadWorkspaces(ctx) // best-effort; a failure here must not block ingestion

	var fileErrs []string
	for _, path := range paths {
		if err := ing.pollFile(ctx, path); err != nil {
			fileErrs = append(fileErrs, fmt.Sprintf("%s: %v", filepath.Base(path), err))
		}
	}
	if len(fileErrs) > 0 {
		return ing.setErr(fmt.Errorf("agy: %d conversation file(s) failed: %s", len(fileErrs), strings.Join(fileErrs, "; ")))
	}
	return ing.setErr(nil)
}

// listConversationDBs returns every "<uuid>.db" file directly under
// <root>/conversations, sorted for deterministic polling order.
func (ing *Ingester) listConversationDBs() ([]string, error) {
	dir := filepath.Join(ing.root, conversationsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".db" {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// pollFile reads one conversation's new gen_metadata rows and emits them.
func (ing *Ingester) pollFile(ctx context.Context, path string) error {
	snap, err := statSnapshot(path)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if prev, ok := ing.getSeen(path); ok && prev.equal(snap) {
		return nil // unchanged since the last full read — nothing to do
	}

	conversationID := strings.TrimSuffix(filepath.Base(path), ".db")

	db, err := openReadOnlyDB(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	wmKey := "agy:" + conversationID
	pos, err := ing.wm.LoadSourceWatermark(wmKey)
	if err != nil {
		return fmt.Errorf("load watermark: %w", err)
	}
	// agy numbers gen_metadata rows from 0, so "nothing emitted yet" is -1: a
	// start of 0 would drop the first model call of every conversation.
	watermark := int64(-1)
	if pos != "" {
		// A corrupt watermark restarts from the beginning; the store dedups on uuid.
		if v, perr := strconv.ParseInt(pos, 10, 64); perr == nil {
			watermark = v
		}
	}

	byMsgID, byIdx, err := loadStepTimestamps(ctx, db)
	if err != nil {
		return fmt.Errorf("load steps: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT idx, data FROM gen_metadata WHERE idx > ? ORDER BY idx ASC`, watermark)
	if err != nil {
		return fmt.Errorf("query gen_metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cwd := ing.workspaceFor(conversationID)
	if cwd == "" {
		// Fallback: a synthetic, source-tagged CWD, exactly like
		// internal/opencode's Ingester does for a session with no directory —
		// applied here rather than left to the Sink's generic fallback so the
		// format matches SessionID's "agy:<id>" shape instead of being
		// double-prefixed.
		cwd = "agy:" + conversationID
	}

	maxIdx := watermark
	warnedTimestamp := false
	deferred := false
	var emitErr error

	for rows.Next() {
		var idx int64
		var data []byte
		if err := rows.Scan(&idx, &data); err != nil {
			continue // malformed row — skip it, not the file
		}

		call, ok, derr := DecodeGenMetadata(data)
		if derr != nil {
			ing.parseErrors.Add(1)
			ing.warn(fmt.Sprintf("agy: %s: malformed gen_metadata row idx=%d, skipping: %v", conversationID, idx, derr))
			continue
		}
		if !ok {
			continue // no usage message — nothing to record for this row
		}

		ts, resolved := resolveTimestamp(call.MessageID, call.LastStepIndex, byMsgID, byIdx)
		if !resolved {
			if time.Since(snap.mtime) < recentWriteWindow {
				// The step that would resolve this row is likely still being
				// written. Stop here without advancing past it — including
				// not saving a watermark for rows before it in this same
				// batch would be wrong too, but those were already emitted,
				// so only this row and anything after it are held back.
				deferred = true
				break
			}
			ts = snap.mtime
			if !warnedTimestamp {
				ing.warn(fmt.Sprintf("agy: %s: could not resolve a call's timestamp from the steps table, using the file's mtime", conversationID))
				warnedTimestamp = true
			}
		}

		uuid := "agy:" + conversationID + ":"
		if call.MessageID != "" {
			uuid += call.MessageID
		} else {
			uuid += "gen:" + strconv.FormatInt(idx, 10)
		}

		r := source.Record{
			Source:      source.Agy,
			UUID:        uuid,
			SessionID:   "agy:" + conversationID,
			CWD:         cwd,
			Type:        "assistant",
			Model:       NormalizeModel(call.Model),
			TS:          ts,
			In:          call.In,
			Out:         call.Out,
			CacheRead:   call.CacheRead,
			CacheCreate: call.CacheCreate,
		}

		if err := ing.sink.Emit(ctx, r); err != nil {
			// Stop here so the watermark never moves past a row we failed to
			// emit; the next poll retries it (dedup makes the retry idempotent).
			emitErr = fmt.Errorf("emit %s: %w", r.UUID, err)
			break
		}
		ing.ingested.Add(1)
		maxIdx = idx
	}

	if emitErr == nil {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("scan gen_metadata: %w", err)
		}
	}

	if maxIdx > watermark {
		if err := ing.wm.SaveSourceWatermark(wmKey, strconv.FormatInt(maxIdx, 10)); err != nil {
			return fmt.Errorf("save watermark: %w", err)
		}
	}

	if emitErr != nil {
		return emitErr
	}
	if !deferred {
		// Only a file drained fully, with nothing held back, is safe to skip
		// on an unchanged snapshot next poll — a deferred file must be
		// revisited even if nothing about it changes in the meantime.
		ing.markSeen(path, snap)
	}
	return nil
}

// resolveTimestamp applies the join order from odd/tasks/agy-support.md: the
// step whose usage message id matches first, then the step named by
// last_step_index, otherwise unresolved.
func resolveTimestamp(messageID, lastStepIndex string, byMsgID map[string]time.Time, byIdx map[int64]time.Time) (time.Time, bool) {
	if messageID != "" {
		if ts, ok := byMsgID[messageID]; ok {
			return ts, true
		}
	}
	if lastStepIndex != "" {
		if idx, err := strconv.ParseInt(lastStepIndex, 10, 64); err == nil {
			if ts, ok := byIdx[idx]; ok {
				return ts, true
			}
		}
	}
	return time.Time{}, false
}

// loadStepTimestamps scans the steps table once per poll and builds both
// timestamp-resolution indexes: by the usage message id a step's metadata
// carries (when present), and by the step's own idx (for the last_step_index
// fallback). A step whose metadata is malformed or carries no timestamp is
// skipped — it simply cannot resolve anything, not a reason to fail the file.
func loadStepTimestamps(ctx context.Context, db *sql.DB) (byMsgID map[string]time.Time, byIdx map[int64]time.Time, err error) {
	rows, err := db.QueryContext(ctx, `SELECT idx, metadata FROM steps`)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	byMsgID = map[string]time.Time{}
	byIdx = map[int64]time.Time{}
	for rows.Next() {
		var idx int64
		var meta []byte
		if err := rows.Scan(&idx, &meta); err != nil {
			continue
		}
		msgID, created, ok := DecodeStepMetadata(meta)
		if !ok || created.IsZero() {
			continue
		}
		byIdx[idx] = created
		if msgID != "" {
			byMsgID[msgID] = created
		}
	}
	return byMsgID, byIdx, rows.Err()
}

func (ing *Ingester) workspaceFor(conversationID string) string {
	ing.wsMu.Lock()
	defer ing.wsMu.Unlock()
	return ing.workspaces[conversationID]
}

// loadWorkspaces refreshes the conversation -> workspace path map from
// conversation_summaries.db. It is best-effort: any failure (file absent,
// unreadable, unexpected schema) leaves the existing map untouched rather
// than aborting the poll — cwd simply falls back to the synthetic
// "agy:<conversation-id>" form (see pollFile) until it succeeds.
func (ing *Ingester) loadWorkspaces(ctx context.Context) {
	path := filepath.Join(ing.root, summariesDBFileName)
	m, err := loadWorkspaceMap(ctx, path)
	if err != nil {
		return
	}
	ing.wsMu.Lock()
	ing.workspaces = m
	ing.wsMu.Unlock()
}

func loadWorkspaceMap(ctx context.Context, path string) (map[string]string, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := openReadOnlyDB(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, `SELECT conversation_id, workspace_uris FROM conversation_summaries`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var id, uris string
		if err := rows.Scan(&id, &uris); err != nil {
			continue
		}
		out[id] = firstFileURIPath(uris)
	}
	return out, rows.Err()
}

// firstFileURIPath extracts the first file:// URI from a workspace_uris cell
// and URL-decodes it to a filesystem path. The column's exact shape is not
// pinned by the verified schema — it may hold a JSON array, a single URI, or
// a comma/newline-separated list — so all three are tried in turn. Anything
// that yields no file:// URI resolves to an empty cwd, which the caller falls
// back from.
func firstFileURIPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		for _, u := range arr {
			if p, ok := fileURIToPath(u); ok {
				return p
			}
		}
		return ""
	}

	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	for _, f := range fields {
		if p, ok := fileURIToPath(strings.TrimSpace(f)); ok {
			return p
		}
	}
	return ""
}

func fileURIToPath(u string) (string, bool) {
	rest, ok := strings.CutPrefix(u, "file://")
	if !ok {
		return "", false
	}
	decoded, err := url.PathUnescape(rest)
	if err != nil {
		decoded = rest
	}
	if decoded == "" {
		return "", false
	}
	return decoded, true
}

func (ing *Ingester) getSeen(path string) (fileSnapshot, bool) {
	ing.seenMu.Lock()
	defer ing.seenMu.Unlock()
	snap, ok := ing.seen[path]
	return snap, ok
}

func (ing *Ingester) markSeen(path string, snap fileSnapshot) {
	ing.seenMu.Lock()
	defer ing.seenMu.Unlock()
	if ing.seen == nil {
		ing.seen = map[string]fileSnapshot{}
	}
	ing.seen[path] = snap
}

// DefaultRoot resolves agy's data directory: ~/.gemini/antigravity-cli. Agy
// does not honor an XDG or product-specific override env var (unlike
// opencode's XDG_DATA_HOME), so there is exactly one path to probe.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", "antigravity-cli")
}
