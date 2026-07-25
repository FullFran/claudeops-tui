package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"os"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/statusline"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// emptyRegistry stands in for the provider registry when a test only cares
// about the Anthropic path.
type emptyRegistry struct{}

func (emptyRegistry) FetchAll(context.Context) []provider.Result { return nil }

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

	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
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
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{snap: snapAt(99)}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
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
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), nil, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{snap: snapAt(99)}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
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
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{snap: snapAt(99)}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, []string{"--refresh"}, f, emptyRegistry{}); err != nil {
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
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), nil, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{err: errors.New("network down")}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
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

	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
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

	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
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
		{args: []string{"--format", "json"}, want: `"provider":"claude"`},
		{args: []string{"--format", "plain"}, want: "5h"},
		{args: []string{"--color"}, want: "#[fg="},
	}
	for _, tc := range cases {
		p := config.ForHome(t.TempDir())
		f := &fakeFetcher{snap: snapAt(42)}
		var out bytes.Buffer
		if err := cmdStatuslineWith(p, &out, tc.args, f, emptyRegistry{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%v: output %q missing %q", tc.args, out.String(), tc.want)
		}
	}
}

func TestStatuslineTTLFlagIsHonoured(t *testing.T) {
	p := config.ForHome(t.TempDir())
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), nil, time.Now().Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	// Default TTL would serve this; a shorter one must not.
	f := &fakeFetcher{snap: snapAt(99)}
	var out bytes.Buffer
	if err := cmdStatuslineWith(p, &out, []string{"--ttl", "10s"}, f, emptyRegistry{}); err != nil {
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
	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("a plan with no quota should render nothing, got %q", out.String())
	}
}

// fakeRegistry returns fixed provider results without touching the network.
type fakeRegistry struct{ usages []provider.Usage }

func (f fakeRegistry) FetchAll(context.Context) []provider.Result {
	out := make([]provider.Result, 0, len(f.usages))
	for _, u := range f.usages {
		out = append(out, provider.Result{Name: u.Provider, Usage: u})
	}
	return out
}

func codexResult(util float64) provider.Usage {
	return provider.Usage{
		Provider: "Codex",
		Windows:  []provider.Window{{Label: "5h", Utilization: util}},
	}
}

func TestStatuslineProviderFlagSelects(t *testing.T) {
	reg := fakeRegistry{usages: []provider.Usage{codexResult(12)}}
	cases := []struct {
		flag string
		want string
	}{
		{flag: "claude", want: "5h 42%"},
		{flag: "codex", want: "5h 12%"},
		{flag: "all", want: "5h 42% │ 5h 12%"},
	}
	for _, tc := range cases {
		p := config.ForHome(t.TempDir())
		var out bytes.Buffer
		if err := cmdStatuslineWith(p, &out, []string{"--provider", tc.flag}, &fakeFetcher{snap: snapAt(42)}, reg); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(out.String()); got != tc.want {
			t.Errorf("--provider %s: got %q want %q", tc.flag, got, tc.want)
		}
	}
}

func TestStatuslineProviderFallsBackToConfig(t *testing.T) {
	// tmux passes an empty string when its user option is unset, which must mean
	// "use the configured default", not "show nothing".
	home := t.TempDir()
	p := config.ForHome(home)
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigPath, []byte("[statusline]\nprovider = \"codex\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	reg := fakeRegistry{usages: []provider.Usage{codexResult(12)}}
	if err := cmdStatuslineWith(p, &out, []string{"--provider", ""}, &fakeFetcher{snap: snapAt(42)}, reg); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 12%" {
		t.Errorf("got %q, expected the configured provider", got)
	}
}

func TestStatuslineProviderErrorsAreSkipped(t *testing.T) {
	// A provider that is merely logged out must not blank the whole segment.
	p := config.ForHome(t.TempDir())
	var out bytes.Buffer
	reg := failingRegistry{}
	if err := cmdStatuslineWith(p, &out, []string{"--provider", "all"}, &fakeFetcher{snap: snapAt(42)}, reg); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 42%" {
		t.Errorf("got %q, expected the working provider only", got)
	}
}

type failingRegistry struct{}

func (failingRegistry) FetchAll(context.Context) []provider.Result {
	return []provider.Result{{Name: "Codex", Err: errors.New("token rejected")}}
}
