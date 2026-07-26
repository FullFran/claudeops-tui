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
// attempted.
//
// It matches usage.Client's own default rather than undercutting it. The
// endpoint is undocumented and shared with Claude Code itself, and a minute was
// five times more aggressive than the value the client had already settled on
// for exactly that reason — which is how a status bar managed to rate-limit an
// account on its own.
//
// Nothing is lost by waiting: at full tilt the 5h window moves about 0.3% a
// minute, so five minutes of staleness is a couple of percent in a bar that
// rounds to whole numbers.
const DefaultTTL = 5 * time.Minute

// Cached is the on-disk representation. The snapshot is stored as fetched;
// StoredAt records when we wrote it, which is what TTL is measured against.
// Snapshot.FetchedAt is the client's own timestamp and may be older.
//
// Providers holds the registry-backed services (Codex, Copilot, Gemini and any
// user-defined ones). Anthropic is not among them: it has its own client and
// lands in Snapshot. Only successful fetches are stored, so a provider that was
// failing simply disappears from the bar rather than caching an error.
//
// The two sections expire independently, hence two timestamps. A render only
// fetches the sources it is about to show, and carries the rest forward from
// the previous file — so a single StoredAt covering both meant every Claude
// refresh renewed the lease on provider data nobody had fetched. On a bar
// sitting in a Claude pane the Codex number could then never expire, and was
// observed frozen for over an hour while the endpoint had long since moved on.
type Cached struct {
	Snapshot  usage.Snapshot   `json:"snapshot"`
	Providers []provider.Usage `json:"providers,omitempty"`
	StoredAt  time.Time        `json:"stored_at"`
	// ProvidersStoredAt is when Providers was last actually fetched. Absent in
	// files written before the split, where the zero value reads as stale and
	// costs one extra refresh — the safe direction.
	ProvidersStoredAt time.Time `json:"providers_stored_at,omitempty"`
}

// NewCached stamps both sections with the same time, for a write that follows a
// refresh of everything.
func NewCached(snap usage.Snapshot, providers []provider.Usage, now time.Time) Cached {
	return Cached{Snapshot: snap, Providers: providers, StoredAt: now, ProvidersStoredAt: now}
}

// Age reports how long ago the snapshot section was written.
func (c Cached) Age(now time.Time) time.Duration { return now.Sub(c.StoredAt) }

// Fresh reports whether the snapshot section is still within ttl.
func (c Cached) Fresh(now time.Time, ttl time.Duration) bool {
	return c.Age(now) < ttl
}

// ProvidersFresh reports whether the provider section is still within ttl. A
// zero timestamp is stale, so a pre-split file refetches once and then carries
// its own timestamp.
func (c Cached) ProvidersFresh(now time.Time, ttl time.Duration) bool {
	if c.ProvidersStoredAt.IsZero() {
		return false
	}
	return now.Sub(c.ProvidersStoredAt) < ttl
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
	// Either section alone makes the file worth keeping: a first run that only
	// asked for Codex writes providers with no snapshot, and discarding that
	// would throw away the fetch it just paid for.
	if c.StoredAt.IsZero() && c.ProvidersStoredAt.IsZero() {
		return Cached{}, ErrNoCache
	}
	return c, nil
}

// WriteCache stores the entry atomically.
//
// It takes the whole Cached rather than a timestamp, because the caller is the
// only one that knows which sections it actually refreshed — see NewCached for
// the everything-at-once case.
//
// Several panes can redraw at once, so a plain write could be observed
// half-finished by a concurrent reader. Write to a temporary file in the same
// directory and rename, which is atomic on POSIX. Mode 0600 because the
// snapshot describes the account's quota.
func WriteCache(path string, c Cached) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
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
