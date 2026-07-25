package statusline

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/usage"
)

func TestReadCacheMissingIsNotAnError(t *testing.T) {
	_, err := ReadCache(filepath.Join(t.TempDir(), "absent.json"))
	if !errors.Is(err, ErrNoCache) {
		t.Fatalf("got %v want ErrNoCache", err)
	}
}

func TestReadCacheCorruptIsTreatedAsMissing(t *testing.T) {
	// A truncated write must not surface a parse error where a quota belongs;
	// the caller's remedy is the same as for a missing file.
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCache(path); !errors.Is(err, ErrNoCache) {
		t.Fatalf("got %v want ErrNoCache", err)
	}
}

func TestReadCacheWithoutTimestampIsTreatedAsMissing(t *testing.T) {
	// Valid JSON with no StoredAt cannot be aged, so it cannot be trusted.
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte(`{"snapshot":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCache(path); !errors.Is(err, ErrNoCache) {
		t.Fatalf("got %v want ErrNoCache", err)
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	snap := usage.Snapshot{FiveHour: &usage.Bucket{Utilization: 42, ResetsAt: now.Add(time.Hour)}}

	if err := WriteCache(path, snap, now); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Snapshot.FiveHour == nil || got.Snapshot.FiveHour.Utilization != 42 {
		t.Errorf("snapshot did not survive: %+v", got.Snapshot)
	}
	if !got.StoredAt.Equal(now) {
		t.Errorf("StoredAt got %v want %v", got.StoredAt, now)
	}
}

func TestWriteCacheIsPrivate(t *testing.T) {
	// The snapshot describes the account's quota, so it must not be world
	// readable even if the surrounding directory is permissive.
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := WriteCache(path, usage.Snapshot{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("got mode %o want 600", perm)
	}
}

func TestWriteCacheLeavesNoTempFiles(t *testing.T) {
	// The write goes through a temp file plus rename; none may be left behind.
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	for range 3 {
		if err := WriteCache(path, usage.Snapshot{}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only the cache file, got %v", names)
	}
}

func TestWriteCacheCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "cache.json")
	if err := WriteCache(path, usage.Snapshot{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestFresh(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		age  time.Duration
		ttl  time.Duration
		want bool
	}{
		{age: 0, ttl: time.Minute, want: true},
		{age: 59 * time.Second, ttl: time.Minute, want: true},
		{age: time.Minute, ttl: time.Minute, want: false},
		{age: time.Hour, ttl: time.Minute, want: false},
	}
	for _, tc := range cases {
		c := Cached{StoredAt: now.Add(-tc.age)}
		if got := c.Fresh(now, tc.ttl); got != tc.want {
			t.Errorf("age %v ttl %v: got %v want %v", tc.age, tc.ttl, got, tc.want)
		}
	}
}
