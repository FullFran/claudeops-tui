package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/statusline"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

type fakeFetcher struct {
	snap  usage.Snapshot
	err   error
	calls int
}

func (f *fakeFetcher) Get(context.Context) (usage.Snapshot, error) {
	f.calls++
	return f.snap, f.err
}

func snapAt(util float64) usage.Snapshot {
	return usage.Snapshot{FiveHour: &usage.Bucket{Utilization: util, ResetsAt: time.Now().Add(time.Hour)}}
}

func TestStatuslineFetchesWhenNoCache(t *testing.T) {
	p := config.ForHome(t.TempDir())
	f := &fakeFetcher{snap: snapAt(42)}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 42%" {
		t.Errorf("got %q want %q", got, "5h 42%")
	}
	if f.calls != 1 {
		t.Errorf("expected one fetch, got %d", f.calls)
	}
}

func TestStatuslineServesFreshCacheWithoutFetching(t *testing.T) {
	// This is the whole reason the package exists: a bar redrawing every two
	// seconds must not reach the network.
	p := config.ForHome(t.TempDir())
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), time.Now()); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{snap: snapAt(99)}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 17%" {
		t.Errorf("served %q, expected the cached value", got)
	}
	if f.calls != 0 {
		t.Errorf("fresh cache must not fetch, got %d calls", f.calls)
	}
}

func TestStatuslineRefetchesWhenCacheIsStale(t *testing.T) {
	p := config.ForHome(t.TempDir())
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{snap: snapAt(99)}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 99%" {
		t.Errorf("got %q want the freshly fetched value", got)
	}
	if f.calls != 1 {
		t.Errorf("expected one fetch, got %d", f.calls)
	}
}

func TestStatuslineRefreshFlagBypassesFreshCache(t *testing.T) {
	p := config.ForHome(t.TempDir())
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), time.Now()); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{snap: snapAt(99)}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, []string{"--refresh"}, f); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 99%" {
		t.Errorf("got %q want the refreshed value", got)
	}
}

func TestStatuslineFallsBackToStaleOnFetchFailure(t *testing.T) {
	// Stale beats blank: a quota from an hour ago still says roughly where you
	// stand, while an empty segment reads as a broken bar.
	p := config.ForHome(t.TempDir())
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{err: errors.New("network down")}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f); err != nil {
		t.Fatalf("a failed fetch must not fail the command: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 17%" {
		t.Errorf("got %q want the stale cached value", got)
	}
}

func TestStatuslineSilentWhenFetchFailsAndNoCache(t *testing.T) {
	// Nothing useful to say, so say nothing and exit zero. A status bar is not
	// where you learn that a token expired.
	p := config.ForHome(t.TempDir())
	f := &fakeFetcher{err: errors.New("no credentials")}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f); err != nil {
		t.Fatalf("expected a silent success, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output, got %q", out.String())
	}
}

func TestStatuslineWritesCacheAfterFetch(t *testing.T) {
	p := config.ForHome(t.TempDir())
	f := &fakeFetcher{snap: snapAt(42)}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f); err != nil {
		t.Fatal(err)
	}
	c, err := statusline.ReadCache(p.UsageCachePath)
	if err != nil {
		t.Fatalf("fetch did not populate the cache: %v", err)
	}
	if c.Snapshot.FiveHour == nil || c.Snapshot.FiveHour.Utilization != 42 {
		t.Errorf("cached the wrong snapshot: %+v", c.Snapshot)
	}
}

func TestStatuslineFormats(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"--format", "json"}, want: `"five_hour"`},
		{args: []string{"--format", "plain"}, want: "5h"},
		{args: []string{"--color"}, want: "#[fg="},
	}
	for _, tc := range cases {
		p := config.ForHome(t.TempDir())
		f := &fakeFetcher{snap: snapAt(42)}
		var out bytes.Buffer
		if err := cmdStatuslineWith(p, &out, tc.args, f); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%v: output %q missing %q", tc.args, out.String(), tc.want)
		}
	}
}

func TestStatuslineTTLFlagIsHonoured(t *testing.T) {
	p := config.ForHome(t.TempDir())
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), time.Now().Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	// Default TTL would serve this; a shorter one must not.
	f := &fakeFetcher{snap: snapAt(99)}
	var out bytes.Buffer
	if err := cmdStatuslineWith(p, &out, []string{"--ttl", "10s"}, f); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Errorf("a 10s TTL should have expired a 30s old entry, got %d calls", f.calls)
	}
}

func TestStatuslineEmptySnapshotPrintsNothing(t *testing.T) {
	p := config.ForHome(t.TempDir())
	f := &fakeFetcher{snap: usage.Snapshot{}}
	var out bytes.Buffer
	if err := cmdStatuslineWith(p, &out, nil, f); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("a plan with no quota should render nothing, got %q", out.String())
	}
}
