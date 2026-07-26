package usage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// The in-process cache is enough for one long-lived program. It is useless the
// moment a second consumer exists: a status bar spawns a fresh process on every
// redraw, so its cache never survives to be used, and running it alongside the
// TUI meant two independent callers each polling the same endpoint on their own
// schedule. Their request rates simply added up.
//
// A cache on disk fixes both. It survives process exit, and every consumer that
// points at the same file shares one refresh — opening the dashboard while a
// status bar is running now costs nothing extra.
//
// This lives in the usage package rather than in a consumer so there is exactly
// one place that decides when the endpoint gets called.

// diskEntry is the on-disk snapshot. StoredAt is what TTL is measured against;
// Snapshot.FetchedAt is the client's own stamp and can be older.
type diskEntry struct {
	Snapshot Snapshot  `json:"snapshot"`
	StoredAt time.Time `json:"stored_at"`
	// History accumulates what utilisation actually did over time. It lives
	// here because this file is already written on every refresh and shared
	// between processes, so the samples come for free and every consumer sees
	// the same series.
	History History `json:"history,omitempty"`
}

// readDisk returns the cached snapshot when one exists and is within ttl.
//
// Every failure is treated as a miss. A corrupt or half-written file must cost
// one extra request, never an error surfaced to a caller that only asked for a
// quota.
func (c *Client) readDisk(now time.Time, ttl time.Duration) (Snapshot, bool) {
	if c.DiskCachePath == "" || ttl <= 0 {
		return Snapshot{}, false
	}
	b, err := os.ReadFile(c.DiskCachePath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			// Unreadable for some other reason; still just a miss.
			return Snapshot{}, false
		}
		return Snapshot{}, false
	}
	var e diskEntry
	if err := json.Unmarshal(b, &e); err != nil || e.StoredAt.IsZero() {
		return Snapshot{}, false
	}
	if now.Sub(e.StoredAt) >= ttl {
		return Snapshot{}, false
	}
	return e.Snapshot, true
}

// writeDisk stores the snapshot atomically.
//
// Several processes can be redrawing at once, so a plain write could be read
// half-finished. Write to a temporary file in the same directory and rename,
// which is atomic on POSIX. Mode 0600 because the snapshot describes the
// account's quota.
func (c *Client) writeDisk(snap Snapshot, now time.Time) {
	if c.DiskCachePath == "" {
		return
	}
	// Carry the existing series forward and extend it.
	hist := c.readHistory()
	hist.Record(snap, now)
	dir := filepath.Dir(c.DiskCachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	b, err := json.Marshal(diskEntry{Snapshot: snap, StoredAt: now, History: hist})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".usage-cache-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	// A failed write is not worth reporting: the snapshot is already in hand and
	// the only cost is fetching again sooner.
	_ = os.Rename(tmpName, c.DiskCachePath)
}

// readHistory loads the recorded series, or an empty one.
func (c *Client) readHistory() History {
	if c.DiskCachePath == "" {
		return History{}
	}
	b, err := os.ReadFile(c.DiskCachePath)
	if err != nil {
		return History{}
	}
	var e diskEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return History{}
	}
	return e.History
}

// Forecasts projects quota exhaustion from the recorded series. Empty when
// there is not enough observed signal to be honest about it.
func (c *Client) Forecasts(snap Snapshot, now time.Time) []Forecast {
	return c.readHistory().Forecasts(snap, now)
}

// DiskAge reports how old the shared cache is, or false when there is none.
// Diagnostics only.
func (c *Client) DiskAge(now time.Time) (time.Duration, bool) {
	if c.DiskCachePath == "" {
		return 0, false
	}
	b, err := os.ReadFile(c.DiskCachePath)
	if err != nil {
		return 0, false
	}
	var e diskEntry
	if err := json.Unmarshal(b, &e); err != nil || e.StoredAt.IsZero() {
		return 0, false
	}
	return now.Sub(e.StoredAt), true
}
