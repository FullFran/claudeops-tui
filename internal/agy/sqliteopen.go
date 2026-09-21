package agy

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// openReadOnlyDB opens a SQLite database read-only using the same passive-read
// strategy as internal/opencode.Ingester.openReadOnly — see that method's doc
// comment for the full rationale. In short: a plain mode=ro open still needs
// to create -wal/-shm sidecars in the source tool's own data directory to
// read a WAL-mode database, and fails outright when that directory is not
// writable; immutable=1 avoids that, but is only safe when both sidecars are
// absent (the database is quiescent) and the header confirms WAL mode (a
// rollback-journal database never has sidecars either, so that check alone
// would misjudge one as quiescent on every poll).
//
// This is a deliberate duplication of opencode's logic rather than a shared
// package. opencode's version is a method bound to a single long-lived
// dbPath field on its Ingester; agy polls many small per-conversation files
// and re-derives immutability per file, per poll, so the method shape there
// does not fit without reshaping opencode's Ingester (and re-verifying its
// already-green WAL/immutable tests) to extract a ~50-line helper. See T1 in
// odd/tasks/agy-support.md for this call.
func openReadOnlyDB(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("agy: db not found: %w", err)
	}
	immutable := !walSidecarsPresent(path) && headerSaysWAL(path)
	db, err := openSQLite(path, immutable)
	if err != nil {
		return nil, err
	}
	if immutable && walSidecarsPresent(path) {
		// The source process came up while we were opening; the handle we
		// hold promised a file that cannot change, and that is no longer true.
		_ = db.Close()
		return openSQLite(path, false)
	}
	return db, nil
}

func openSQLite(path string, immutable bool) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", filepath.ToSlash(path))
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

func walSidecarsPresent(path string) bool {
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			return true
		}
	}
	return false
}

// headerSaysWAL reports whether the database file declares WAL journalling —
// bytes 18 and 19 of the SQLite header are the write and read format
// versions, 2 means WAL. Anything unreadable or unrecognized answers false,
// which costs at most a sidecar and never correctness.
func headerSaysWAL(path string) bool {
	f, err := os.Open(path)
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
