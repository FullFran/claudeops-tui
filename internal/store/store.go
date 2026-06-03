// Package store persists events, sessions, projects, tasks and offsets in SQLite.
// Single-writer discipline: only one process should open the store for writes.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps a *sql.DB with schema management and typed methods.
type Store struct {
	db *sql.DB
}

// Event is a flat representation of an ingested Claude Code event.
// Token counts are split into the four pricing classes.
type Event struct {
	UUID              string
	SessionID         string
	CWD               string // working directory of the session
	Type              string // "assistant", "user", etc.
	Model             string // empty for non-assistant events
	TS                time.Time
	InTokens          int64
	OutTokens         int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	Source            string // ingestion origin: "claude", "codex", "opencode"; defaults to "claude"
}

// Open creates (or opens) the SQLite file and runs migrations.
// WAL is enabled and foreign keys are enforced.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return err
	}
	return s.migrateAddSource()
}

// migrateAddSource adds the source column to events and sessions if absent.
// It is idempotent: protected by PRAGMA table_info so re-running never errors.
// MUST only be called from migrate() (read-write open path).
func (s *Store) migrateAddSource() error {
	for _, tbl := range []string{"events", "sessions"} {
		has, err := s.columnExists(tbl, "source")
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := s.db.Exec(
			"ALTER TABLE " + tbl + " ADD COLUMN source TEXT NOT NULL DEFAULT 'claude'",
		); err != nil {
			return err
		}
	}
	// CREATE INDEX IF NOT EXISTS is safe to run always.
	_, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_events_source ON events(source)`)
	return err
}

// columnExists returns true when the given column is present in the table.
// It uses PRAGMA table_info which works on any SQLite version.
func (s *Store) columnExists(table, column string) (bool, error) {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dflt interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// OpenReadOnly opens an existing SQLite file in read-only mode.
// It does NOT run migrations and returns an error if the file does not exist.
func OpenReadOnly(path string) (*Store, error) {
	// Use immutable=1 for safer concurrent read-only access.
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)",
		filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenReadOnly %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the raw handle for tests.
func (s *Store) DB() *sql.DB { return s.db }

// Insert upserts the project (by cwd) and session (by id), then writes the event.
// All in one transaction. Idempotent on event uuid.
func (s *Store) Insert(ctx context.Context, ev Event, costEUR *float64, taskID *string) error {
	if ev.UUID == "" || ev.SessionID == "" || ev.CWD == "" {
		return errors.New("store.Insert: uuid, session_id and cwd are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// upsert project
	projectName := projectNameFromCWD(ev.CWD)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects (cwd, name) VALUES (?, ?)
		 ON CONFLICT(cwd) DO UPDATE SET name=excluded.name`,
		ev.CWD, projectName); err != nil {
		return err
	}
	var projectID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE cwd = ?`, ev.CWD).Scan(&projectID); err != nil {
		return err
	}

	// upsert session
	tsStr := ev.TS.UTC().Format(time.RFC3339Nano)
	sessionSource := ev.Source
	if sessionSource == "" {
		sessionSource = "claude"
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (id, project_id, first_seen, last_seen, source) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET last_seen = excluded.last_seen`,
		ev.SessionID, projectID, tsStr, tsStr, sessionSource); err != nil {
		return err
	}

	// Resolve source: default to "claude" if not set, to preserve backward compat.
	source := ev.Source
	if source == "" {
		source = "claude"
	}

	// insert event (idempotent on uuid)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events (uuid, session_id, ts, type, model, in_tokens, out_tokens, cache_read_tokens, cache_create_tokens, cost_eur, task_id, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(uuid) DO NOTHING`,
		ev.UUID, ev.SessionID, tsStr, ev.Type, nullString(ev.Model),
		ev.InTokens, ev.OutTokens, ev.CacheReadTokens, ev.CacheCreateTokens,
		nullFloat(costEUR), nullString(strDeref(taskID)), source,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveSourceWatermark persists the last-seen position for a polling source.
// The position is an opaque string (typically a time_created epoch-ms string
// or a rowid). Idempotent: upserts on the source primary key.
func (s *Store) SaveSourceWatermark(source, position string) error {
	_, err := s.db.Exec(
		`INSERT INTO source_watermarks (source, position) VALUES (?, ?)
		 ON CONFLICT(source) DO UPDATE SET position=excluded.position`,
		source, position)
	return err
}

// LoadSourceWatermark returns the persisted watermark for source, or "" if none.
func (s *Store) LoadSourceWatermark(source string) (string, error) {
	var pos string
	err := s.db.QueryRow(
		`SELECT position FROM source_watermarks WHERE source = ?`, source,
	).Scan(&pos)
	if err != nil {
		// sql.ErrNoRows means no watermark yet — that is not an error.
		return "", nil
	}
	return pos, nil
}

// SaveOffset records how many bytes have been processed from a file.
func (s *Store) SaveOffset(path string, offset, size int64) error {
	_, err := s.db.Exec(
		`INSERT INTO file_offsets (path, offset, size) VALUES (?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET offset=excluded.offset, size=excluded.size`,
		path, offset, size)
	return err
}

// LoadOffsets returns the persisted offset per known file.
func (s *Store) LoadOffsets() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT path, offset FROM file_offsets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var p string
		var off int64
		if err := rows.Scan(&p, &off); err != nil {
			return nil, err
		}
		out[p] = off
	}
	return out, rows.Err()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullFloat(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func strDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func projectNameFromCWD(cwd string) string {
	return filepath.Base(cwd)
}
