package opencode

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// defaultPollInterval is how long Watch sleeps between polls.
const defaultPollInterval = 5 * time.Second

// WatermarkStore persists the per-source polling watermark.
// Implemented by *store.Store in production; fakeStore in tests.
type WatermarkStore interface {
	LoadSourceWatermark(src string) (string, error)
	SaveSourceWatermark(src, position string) error
}

// Ingester implements source.Ingester for the opencode SQLite DB.
// It opens the DB read-only, polls for messages newer than the watermark,
// decodes and normalizes each row, and emits source.Record values via a Sink.
type Ingester struct {
	dbPath       string
	wm           WatermarkStore
	sink         source.Sink
	pollInterval time.Duration

	mu      sync.Mutex
	lastErr error
}

// NewIngester creates an Ingester targeting the opencode DB at dbPath.
func NewIngester(dbPath string, wm WatermarkStore, sink source.Sink) *Ingester {
	return &Ingester{
		dbPath:       dbPath,
		wm:           wm,
		sink:         sink,
		pollInterval: defaultPollInterval,
	}
}

// Name implements source.Ingester.
func (ing *Ingester) Name() source.Name { return source.Opencode }

// LastErr reports the outcome of the most recent poll: nil when it completed,
// otherwise the failure that aborted it. Watch discards poll's return value, so
// this is how a persistent failure (schema drift, unreadable DB) stays visible.
func (ing *Ingester) LastErr() error {
	ing.mu.Lock()
	defer ing.mu.Unlock()
	return ing.lastErr
}

// setErr records the outcome of a poll and returns it unchanged.
func (ing *Ingester) setErr(err error) error {
	ing.mu.Lock()
	ing.lastErr = err
	ing.mu.Unlock()
	return err
}

// IngestExisting implements source.Ingester: one-shot drain from the watermark.
func (ing *Ingester) IngestExisting(ctx context.Context) error {
	return ing.poll(ctx)
}

// Watch implements source.Ingester: poll loop until ctx.Done.
func (ing *Ingester) Watch(ctx context.Context) error {
	for {
		_ = ing.poll(ctx) // best-effort; tolerate transient errors
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ing.pollInterval):
		}
	}
}

// poll opens the DB read-only, queries rows past the watermark, decodes and emits.
func (ing *Ingester) poll(ctx context.Context) error {
	if _, err := os.Stat(ing.dbPath); err != nil {
		if os.IsNotExist(err) {
			// opencode is not installed — a legitimate no-op, not a failure.
			return ing.setErr(nil)
		}
		return ing.setErr(fmt.Errorf("opencode: stat db: %w", err))
	}

	db, err := ing.openReadOnly()
	if err != nil {
		return ing.setErr(fmt.Errorf("opencode: open db: %w", err))
	}
	defer func() { _ = db.Close() }()

	pos, err := ing.wm.LoadSourceWatermark("opencode")
	if err != nil {
		return ing.setErr(fmt.Errorf("opencode: load watermark: %w", err))
	}

	var watermark int64
	if pos != "" {
		watermark, err = strconv.ParseInt(pos, 10, 64)
		if err != nil {
			// Corrupt watermark — reset to 0 (re-ingest; dedup prevents doubles).
			watermark = 0
		}
	}

	// The predicate is inclusive: rows sharing the watermark's time_created are
	// re-read on every poll. That covers rows committed after the previous
	// snapshot at the same millisecond, and lets the store's corrective upsert
	// repair a row whose token counts were still being written when first read.
	// Re-reads are free — the store dedups on uuid.
	rows, err := db.QueryContext(ctx, `
		SELECT m.id, m.session_id, m.time_created, m.data, COALESCE(s.directory, '')
		FROM message m
		LEFT JOIN session s ON s.id = m.session_id
		WHERE m.time_created >= ?
		ORDER BY m.time_created ASC`,
		watermark,
	)
	if err != nil {
		return ing.setErr(fmt.Errorf("opencode: query messages: %w", err))
	}
	defer func() { _ = rows.Close() }()

	maxTC := watermark
	var emitErr error
	for rows.Next() {
		var msgID, sessionID, rawData, directory string
		var timeCreated int64

		if err := rows.Scan(&msgID, &sessionID, &timeCreated, &rawData, &directory); err != nil {
			continue // skip malformed rows
		}

		d, err := DecodeMessageData([]byte(rawData))
		if err != nil {
			continue // skip unparseable blobs
		}
		if d.Role != "assistant" {
			continue // only assistant rows produce cost events
		}

		canonicalModel := NormalizeModel(d.ProviderID, d.ModelID)
		toks := d.ToTokenRecord()

		cwd := directory
		if cwd == "" {
			// Fallback: use a synthetic CWD so store.Insert's non-empty-cwd
			// invariant is never violated. filepath.Base yields "opencode:ses-id"
			// as the project name — acceptable, source-tagged.
			cwd = "opencode:" + sessionID
		}

		ts := time.UnixMilli(timeCreated).UTC()

		r := source.Record{
			Source:          source.Opencode,
			UUID:            "opencode:" + msgID,
			SessionID:       "opencode:" + sessionID,
			CWD:             cwd,
			Type:            "assistant",
			Model:           canonicalModel,
			TS:              ts,
			In:              toks.In,
			Out:             toks.Out,
			CacheRead:       toks.CacheRead,
			CacheCreate:     toks.CacheCreate,
			ReportedCostUSD: d.BilledCostUSD(),
		}

		if err := ing.sink.Emit(ctx, r); err != nil {
			// Stop here so the watermark never moves past a row we failed to
			// emit; the next poll retries it (dedup makes the retry idempotent).
			emitErr = fmt.Errorf("opencode: emit %s: %w", r.UUID, err)
			break
		}

		if timeCreated > maxTC {
			maxTC = timeCreated
		}
	}
	if emitErr == nil {
		if err := rows.Err(); err != nil {
			return ing.setErr(fmt.Errorf("opencode: scan messages: %w", err))
		}
	}

	// Advance watermark if we saw any rows.
	if maxTC > watermark {
		if err := ing.wm.SaveSourceWatermark("opencode", strconv.FormatInt(maxTC, 10)); err != nil {
			return ing.setErr(fmt.Errorf("opencode: save watermark: %w", err))
		}
	}
	return ing.setErr(emitErr)
}

// openReadOnly opens the opencode DB read-only via modernc.org/sqlite.
//
// Which DSN it uses depends on whether opencode's WAL sidecars are on disk,
// because a plain read-only open is not as passive as it sounds. To read a
// WAL-mode database SQLite needs the -shm index, and it will happily create
// both sidecars to get one — so the ordinary path has claudeops writing into
// opencode's data directory just to look at it, and fails outright when that
// directory is not writable.
//
// When both sidecars are absent the database file is self-contained: only a
// clean shutdown removes them, and it checkpoints everything first. immutable=1
// reads that file without asking for the index, which is both passive and the
// only thing that works on a read-only mount. Should opencode start up in the
// meantime, the rows it then writes are newer than anything in our snapshot, so
// they land past the watermark and the next poll picks them up.
//
// When the sidecars do exist the WAL may hold committed rows the main file has
// not absorbed yet, so the normal path is the only correct one — immutable
// would quietly serve stale data. busy_timeout keeps it patient while the
// opencode process holds a lock.
func (ing *Ingester) openReadOnly() (*sql.DB, error) {
	// Verify the file exists before attempting to open — avoids creating a new
	// empty DB at that path (read-only mode should prevent that, but be explicit).
	if _, err := os.Stat(ing.dbPath); err != nil {
		return nil, fmt.Errorf("opencode: db not found: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)",
		filepath.ToSlash(ing.dbPath))
	if !ing.walSidecarsPresent() {
		dsn += "&immutable=1"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// walSidecarsPresent reports whether either WAL sidecar is on disk. Either one
// is enough to mean the database is not known to be quiescent, which is the
// conservative reading: it keeps openReadOnly on the path that can see the WAL.
func (ing *Ingester) walSidecarsPresent() bool {
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(ing.dbPath + suffix); err == nil {
			return true
		}
	}
	return false
}

// DefaultDBPath resolves the opencode database the same way opencode itself
// does: under $XDG_DATA_HOME when that is set, otherwise the conventional
// ~/.local/share. Honoring it matters for anyone who relocates their data
// directory — without it claudeops looks in a path opencode never writes to and
// reports no opencode usage at all, with nothing to indicate why.
//
// Returns "" when neither the variable nor a home directory can be resolved;
// callers treat that as "opencode is not installed".
func DefaultDBPath() string {
	if dir := dataHome(); dir != "" {
		return filepath.Join(dir, "opencode", "opencode.db")
	}
	return ""
}

// dataHome returns $XDG_DATA_HOME, or ~/.local/share when it is unset. A
// relative XDG_DATA_HOME is ignored, as the spec requires.
func dataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(v) {
		return v
	}
	home, err := homeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share")
}

// homeDir returns os.UserHomeDir — extracted for test-skipping.
func homeDir() (string, error) {
	return os.UserHomeDir()
}

// fileExists is a small helper used by the integration test.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
