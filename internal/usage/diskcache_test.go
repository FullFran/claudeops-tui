package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// usageServer counts requests so a test can assert the endpoint was spared.
func usageServer(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":42,"resets_at":"2030-01-01T00:00:00Z"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func clientFor(t *testing.T, srv *httptest.Server, cachePath string) *Client {
	t.Helper()
	creds := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(creds, []byte(
		`{"claudeAiOauth":{"accessToken":"tok","expiresAt":99999999999999}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(creds)
	c.UsageURL = srv.URL
	c.HTTP = srv.Client()
	c.DiskCachePath = cachePath
	return c
}

func TestSecondProcessReusesTheSharedCache(t *testing.T) {
	// The whole point: a status bar and an open dashboard are separate
	// processes. Without a shared cache each polls on its own schedule and the
	// request rates add up, which is how an account gets rate-limited by its
	// own status bar.
	var hits int32
	srv := usageServer(t, &hits)
	cache := filepath.Join(t.TempDir(), "snapshot.json")

	first := clientFor(t, srv, cache)
	if _, err := first.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("first call should fetch once, got %d", hits)
	}

	// A distinct client stands in for a distinct process: no shared memory,
	// only the file.
	second := clientFor(t, srv, cache)
	snap, err := second.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("the second process should have reused the cache, endpoint hit %d times", hits)
	}
	if snap.FiveHour == nil || snap.FiveHour.Utilization != 42 {
		t.Errorf("cached snapshot did not survive: %+v", snap.FiveHour)
	}
}

func TestSharedCacheExpires(t *testing.T) {
	var hits int32
	srv := usageServer(t, &hits)
	cache := filepath.Join(t.TempDir(), "snapshot.json")

	first := clientFor(t, srv, cache)
	first.CacheTTL = 50 * time.Millisecond
	if _, err := first.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)

	second := clientFor(t, srv, cache)
	second.CacheTTL = 50 * time.Millisecond
	if _, err := second.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("an expired shared cache should refetch, got %d hits", hits)
	}
}

func TestNoDiskCachePathKeepsOldBehaviour(t *testing.T) {
	// Leaving the path empty must not start writing files somewhere.
	var hits int32
	srv := usageServer(t, &hits)

	a := clientFor(t, srv, "")
	b := clientFor(t, srv, "")
	if _, err := a.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("without a shared cache each client fetches, got %d", hits)
	}
}

func TestCorruptSharedCacheIsJustAMiss(t *testing.T) {
	// A half-written file must cost one extra request, never an error surfaced
	// to a caller that only asked for a quota.
	var hits int32
	srv := usageServer(t, &hits)
	cache := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(cache, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := clientFor(t, srv, cache)
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatalf("a corrupt cache must not fail the call: %v", err)
	}
	if hits != 1 {
		t.Errorf("expected one fetch, got %d", hits)
	}
}

func TestSharedCacheIsPrivate(t *testing.T) {
	// It describes the account's quota.
	var hits int32
	srv := usageServer(t, &hits)
	cache := filepath.Join(t.TempDir(), "snapshot.json")
	c := clientFor(t, srv, cache)
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("got mode %o want 600", perm)
	}
}

func TestSharedCacheLeavesNoTempFiles(t *testing.T) {
	var hits int32
	srv := usageServer(t, &hits)
	dir := t.TempDir()
	cache := filepath.Join(dir, "snapshot.json")
	for range 3 {
		c := clientFor(t, srv, cache)
		c.CacheTTL = time.Nanosecond // force a fetch and a write each time
		if _, err := c.Get(context.Background()); err != nil {
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
