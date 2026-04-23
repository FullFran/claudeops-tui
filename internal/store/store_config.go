package store

import (
	"context"
	"database/sql"
	"errors"
)

// ConfigGet retrieves a value from the config table by key.
// Returns ("", false, nil) when the key does not exist.
func (s *Store) ConfigGet(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM config WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// ConfigSet inserts or updates a key/value pair in the config table.
func (s *Store) ConfigSet(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO config(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}
