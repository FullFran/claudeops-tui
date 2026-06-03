package opencode

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	db, err := ing.openReadOnly()
	if err != nil {
		// DB does not exist or is not accessible — skip silently.
		return nil
	}
	defer db.Close()

	pos, err := ing.wm.LoadSourceWatermark("opencode")
	if err != nil {
		return fmt.Errorf("opencode: load watermark: %w", err)
	}

	var watermark int64
	if pos != "" {
		watermark, err = strconv.ParseInt(pos, 10, 64)
		if err != nil {
			// Corrupt watermark — reset to 0 (re-ingest; dedup prevents doubles).
			watermark = 0
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT m.id, m.session_id, m.time_created, m.data, COALESCE(s.directory, '')
		FROM message m
		LEFT JOIN session s ON s.id = m.session_id
		WHERE m.time_created > ?
		ORDER BY m.time_created ASC`,
		watermark,
	)
	if err != nil {
		// Transient DB error (SQLITE_BUSY, etc.) — skip this poll.
		return nil
	}
	defer rows.Close()

	var maxTC int64 = watermark
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
			Source:      source.Opencode,
			UUID:        "opencode:" + msgID,
			SessionID:   "opencode:" + sessionID,
			CWD:         cwd,
			Type:        "assistant",
			Model:       canonicalModel,
			TS:          ts,
			In:          toks.In,
			Out:         toks.Out,
			CacheRead:   toks.CacheRead,
			CacheCreate: toks.CacheCreate,
		}

		if err := ing.sink.Emit(ctx, r); err != nil {
			// Non-fatal: a single emit failure (e.g. dedup) should not stop the poll.
			_ = err
		}

		if timeCreated > maxTC {
			maxTC = timeCreated
		}
	}
	if err := rows.Err(); err != nil {
		return nil // transient, skip
	}

	// Advance watermark if we saw any rows.
	if maxTC > watermark {
		if err := ing.wm.SaveSourceWatermark("opencode", strconv.FormatInt(maxTC, 10)); err != nil {
			return fmt.Errorf("opencode: save watermark: %w", err)
		}
	}
	return nil
}

// openReadOnly opens the opencode DB in read-only WAL mode via modernc.org/sqlite.
// Uses busy_timeout so transient lock contention (from the opencode process) is
// handled gracefully without returning an error to the caller.
func (ing *Ingester) openReadOnly() (*sql.DB, error) {
	// Verify the file exists before attempting to open — avoids creating a new
	// empty DB at that path (read-only mode should prevent that, but be explicit).
	if _, err := os.Stat(ing.dbPath); err != nil {
		return nil, fmt.Errorf("opencode: db not found: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)",
		filepath.ToSlash(ing.dbPath))
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

// homeDir returns os.UserHomeDir — extracted for test-skipping.
func homeDir() (string, error) {
	return os.UserHomeDir()
}

// fileExists is a small helper used by the integration test.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
