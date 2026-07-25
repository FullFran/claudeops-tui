// Package statusline renders a one-line usage summary for a terminal status
// bar, backed by an on-disk cache.
//
// The usage client already caches, but only in process. A status bar invokes a
// fresh process on every refresh — every two seconds in a typical tmux setup —
// so that cache never survives to be used and every draw would hit the network.
// This package keeps the snapshot on disk instead, so the endpoint is touched
// once per TTL no matter how often the bar redraws.
package statusline

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// DefaultTTL is how long a cached snapshot is served before a refresh is
// attempted. Quota buckets move slowly; the 5h window shifts by about 0.3% a
// minute at full tilt, so a minute of staleness is invisible in a status bar
// and keeps the request rate negligible.
const DefaultTTL = time.Minute

// Cached is the on-disk representation. The snapshot is stored as fetched;
// StoredAt records when we wrote it, which is what TTL is measured against.
// Snapshot.FetchedAt is the client's own timestamp and may be older.
//
// Providers holds the registry-backed services (Codex, Copilot, Gemini and any
// user-defined ones). Anthropic is not among them: it has its own client and
// lands in Snapshot. Only successful fetches are stored, so a provider that was
// failing simply disappears from the bar rather than caching an error.
type Cached struct {
	Snapshot  usage.Snapshot   `json:"snapshot"`
	Providers []provider.Usage `json:"providers,omitempty"`
	StoredAt  time.Time        `json:"stored_at"`
}

// Age reports how long ago the entry was written.
func (c Cached) Age(now time.Time) time.Duration { return now.Sub(c.StoredAt) }

// Fresh reports whether the entry is still within ttl.
func (c Cached) Fresh(now time.Time, ttl time.Duration) bool {
	return c.Age(now) < ttl
}

// ErrNoCache reports that no usable cache entry exists yet.
var ErrNoCache = errors.New("statusline: no cached snapshot")

// ReadCache loads the cached snapshot.
//
// A missing file is an ordinary first run, not a failure. A corrupt file is
// reported the same way: the caller's remedy is identical either way, and a
// status bar must never surface a parse error where a quota belongs.
func ReadCache(path string) (Cached, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Cached{}, ErrNoCache
		}
		return Cached{}, err
	}
	var c Cached
	if err := json.Unmarshal(b, &c); err != nil {
		return Cached{}, ErrNoCache
	}
	if c.StoredAt.IsZero() {
		return Cached{}, ErrNoCache
	}
	return c, nil
}

// WriteCache stores the snapshot atomically.
//
// Several panes can redraw at once, so a plain write could be observed
// half-finished by a concurrent reader. Write to a temporary file in the same
// directory and rename, which is atomic on POSIX. Mode 0600 because the
// snapshot describes the account's quota.
func WriteCache(path string, snap usage.Snapshot, providers []provider.Usage, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(Cached{Snapshot: snap, Providers: providers, StoredAt: now})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".usage-cache-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
