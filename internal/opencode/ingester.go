package opencode

import (
	"context"
	"database/sql"
	"fmt"
	"io"
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

	mu       sync.Mutex
	lastErr  error
	failures int
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

// ConsecutiveFailures reports how many polls in a row have failed, reset to
// zero by the first one that completes.
//
// It is counted here rather than by whoever is watching because only this side
// knows when a poll happened. lastErr is a level, not an event: a watchdog
// sampling it on its own clock cannot tell one failure it has seen three times
// from three separate failures, and a cold drain of a large database takes long
// enough for those to be very different claims.
func (ing *Ingester) ConsecutiveFailures() int {
	ing.mu.Lock()
	defer ing.mu.Unlock()
	return ing.failures
}

// setErr records the outcome of a poll and returns it unchanged.
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
// When the database is in WAL mode and both sidecars are absent, the file is
// self-contained: only a clean shutdown removes them, and it checkpoints
// everything first. immutable=1 reads that file without asking for the index,
// which is both passive and the only thing that works on a read-only mount.
//
// immutable=1 also tells SQLite the file cannot change, so it takes no locks
// and revalidates nothing. That promise is ours to keep and we cannot: opencode
// may start between the check and the read and checkpoint pages out from under
// us, which surfaces as "database disk image is malformed". Three things bound
// it. The mode is only chosen for a database whose header says WAL, so a
// rollback-journal database — which has no sidecars ever — never takes this
// path. The sidecars are re-checked once the handle is live, so the common race
// is caught before a single row is read. And when the write lands mid-scan the
// query fails as a whole rather than returning torn rows: poll returns on
// rows.Err before it saves the watermark, so nothing is skipped, the failure is
// reported, and the next poll five seconds later sees the sidecars opencode
// just created and takes the ordinary path.
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
	immutable := !ing.walSidecarsPresent() && ing.headerSaysWAL()
	db, err := ing.open(immutable)
	if err != nil {
		return nil, err
	}
	if immutable && ing.walSidecarsPresent() {
		// opencode came up while we were opening. The handle we hold promised
		// SQLite a file that cannot change, and that is no longer true.
		_ = db.Close()
		return ing.open(false)
	}
	return db, nil
}

// open builds the DSN and returns a live handle.
func (ing *Ingester) open(immutable bool) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)",
		filepath.ToSlash(ing.dbPath))
	if immutable {
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

// headerSaysWAL reports whether the database file declares WAL journalling.
//
// Bytes 18 and 19 of the SQLite header are the write and read format versions;
// 2 means WAL. Without this check, "no sidecars on disk" would also be true of
// every rollback-journal database — which never has them — and the immutable
// path would then be taken on every poll of a file opencode is actively
// writing. Anything unreadable or unrecognised answers false, which costs at
// most a sidecar and never correctness.
func (ing *Ingester) headerSaysWAL() bool {
	f, err := os.Open(ing.dbPath)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	var hdr [20]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return false
	}
	const walFormat = 2
	return hdr[18] == walFormat && hdr[19] == walFormat
}

// DefaultDBPath resolves the opencode database, preferring a candidate that
// actually exists.
//
// opencode resolves its data directory as `XDG_DATA_HOME || ~/.local/share`, so
// claudeops has to as well: anyone who relocates it otherwise gets a silent
// zero, with nothing to indicate we looked somewhere else. But resolving is not
// enough on its own. The variable can be exported after opencode has already
// written a database under the conventional path, or exported only for some
// shells, and a resolver that trusts it blindly then declares opencode absent
// while its history sits untouched one directory away — which, since
// `claudeops reingest` clears the store before rebuilding it, would drop that
// history for good.
//
// So both candidates are probed and the first that exists wins. When neither
// does, the XDG-derived path is returned: that is where opencode would write,
// and callers only use it to conclude opencode is not installed.
//
// Returns "" when no candidate can be built at all.
func DefaultDBPath() string {
	candidates := dbPathCandidates()
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// dbPathCandidates lists the database locations to probe, most specific first.
// The second entry is omitted when it would duplicate the first.
//
// The XDG_DATA_HOME test is "non-empty", not "absolute". The spec says a
// relative value must be ignored, but opencode does not implement that rule,
// and the job here is to find opencode's file rather than to be right about the
// spec. A relative value that resolves to nothing is caught by the existence
// probe above.
func dbPathCandidates() []string {
	var out []string
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		out = append(out, filepath.Join(v, "opencode", "opencode.db"))
	}
	if home, err := homeDir(); err == nil {
		conventional := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
		if len(out) == 0 || out[0] != conventional {
			out = append(out, conventional)
		}
	}
	return out
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
