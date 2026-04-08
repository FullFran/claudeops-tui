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
	_, err := s.db.Exec(schemaSQL)
	return err
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
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (id, project_id, first_seen, last_seen) VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET last_seen = excluded.last_seen`,
		ev.SessionID, projectID, tsStr, tsStr); err != nil {
		return err
	}

	// insert event (idempotent on uuid)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events (uuid, session_id, ts, type, model, in_tokens, out_tokens, cache_read_tokens, cache_create_tokens, cost_eur, task_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(uuid) DO NOTHING`,
		ev.UUID, ev.SessionID, tsStr, ev.Type, nullString(ev.Model),
		ev.InTokens, ev.OutTokens, ev.CacheReadTokens, ev.CacheCreateTokens,
		nullFloat(costEUR), nullString(strDeref(taskID)),
	); err != nil {
		return err
	}
	return tx.Commit()
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
