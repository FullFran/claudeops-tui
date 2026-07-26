package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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

// statuslinePaths gives one test its own home with the status line pinned to a
// provider.
//
// Without a config the command falls back to "auto", which asks the *host's*
// tmux which agent occupies the active pane — so the result depended on what
// the developer happened to be looking at while the suite ran, and the same
// test resolved to Claude in one run and Codex in the next. None of the tests
// here are about detection; they are about caching, formatting and selection,
// so the provider is stated rather than discovered.
func statuslinePaths(t *testing.T, want string) config.Paths {
	t.Helper()
	p := config.ForHome(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(p.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// The agent map is written out as well so the file is a faithful stand-in
	// for a real config rather than a fragment that silently drops defaults.
	body := "[statusline]\nenabled = true\nprovider = \"" + want + "\"\n" +
		"[statusline.agents]\nclaude = \"claude\"\ncodex = \"codex\"\n" +
		"crush = \"codex\"\nopencode = \"codex\"\n"
	if err := os.WriteFile(p.ConfigPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func snapAt(util float64) usage.Snapshot {
	return usage.Snapshot{FiveHour: &usage.Bucket{Utilization: util, ResetsAt: time.Now().Add(time.Hour)}}
}

func TestStatuslineFetchesWhenNoCache(t *testing.T) {
	p := statuslinePaths(t, "claude")
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
	p := statuslinePaths(t, "claude")
	if err := statusline.WriteCache(p.UsageCachePath, statusline.NewCached(snapAt(17), nil, time.Now())); err != nil {
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
	p := statuslinePaths(t, "claude")
	if err := statusline.WriteCache(p.UsageCachePath, statusline.NewCached(snapAt(17), nil, time.Now().Add(-time.Hour))); err != nil {
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
	p := statuslinePaths(t, "claude")
	if err := statusline.WriteCache(p.UsageCachePath, statusline.NewCached(snapAt(17), nil, time.Now())); err != nil {
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
	p := statuslinePaths(t, "claude")
	if err := statusline.WriteCache(p.UsageCachePath, statusline.NewCached(snapAt(17), nil, time.Now().Add(-time.Hour))); err != nil {
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
	p := statuslinePaths(t, "claude")
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
	p := statuslinePaths(t, "claude")
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
		{args: []string{"--color=tmux"}, want: "#[fg="},
	}
	for _, tc := range cases {
		p := statuslinePaths(t, "claude")
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
	p := statuslinePaths(t, "claude")
	if err := statusline.WriteCache(p.UsageCachePath, statusline.NewCached(snapAt(17), nil, time.Now().Add(-30*time.Second))); err != nil {
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
	p := statuslinePaths(t, "claude")
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
		p := statuslinePaths(t, "claude")
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
	p := statuslinePaths(t, "claude")
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

func TestStatuslineDisabledPrintsNothing(t *testing.T) {
	// Disabled must short-circuit before any work: no fetch, no cache read.
	p := statuslinePaths(t, "claude")
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigPath, []byte("[statusline]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{snap: snapAt(42)}
	var out bytes.Buffer
	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("disabled should print nothing, got %q", out.String())
	}
	if f.calls != 0 {
		t.Errorf("disabled should not fetch, got %d calls", f.calls)
	}
}

func TestStatuslineEnabledByDefaultForOldConfigs(t *testing.T) {
	// A config file written before the key existed has no [statusline] section.
	// That must keep working rather than silently turning the feature off.
	//
	// The point here is the default for `enabled`, so the file deliberately
	// carries no statusline section at all — which also means no provider to
	// pin. It is passed on the command line instead, keeping the test off the
	// host's tmux.
	p := config.ForHome(t.TempDir())
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigPath, []byte("[usage]\ncache_ttl_seconds = 300\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdStatuslineWith(p, &out, []string{"--provider", "claude"},
		&fakeFetcher{snap: snapAt(42)}, emptyRegistry{}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "5h 42%" {
		t.Errorf("got %q, expected the status line to render", out.String())
	}
}

func TestStatuslineEnableDisableRoundTrip(t *testing.T) {
	p := statuslinePaths(t, "claude")
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, []string{"disable"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cmdStatuslineWith(p, &out, nil, &fakeFetcher{snap: snapAt(42)}, emptyRegistry{}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("after disable, got %q", out.String())
	}

	out.Reset()
	if err := cmdStatuslineWith(p, &out, []string{"enable"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cmdStatuslineWith(p, &out, nil, &fakeFetcher{snap: snapAt(42)}, emptyRegistry{}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "5h 42%" {
		t.Errorf("after enable, got %q", out.String())
	}
}

func TestStatuslineToggleKeepsOtherSettings(t *testing.T) {
	// Writing the config must not clobber unrelated keys the user set.
	p := statuslinePaths(t, "claude")
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := "[usage]\ncache_ttl_seconds = 42\n\n[statusline]\nprovider = \"codex\"\n"
	if err := os.WriteFile(p.ConfigPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdStatuslineWith(p, &out, []string{"disable"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	after, err := config.Load(p.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Usage.CacheTTLSeconds != 42 {
		t.Errorf("unrelated setting lost: cache_ttl_seconds = %d", after.Usage.CacheTTLSeconds)
	}
	if after.Statusline.Provider != "codex" {
		t.Errorf("provider lost: %q", after.Statusline.Provider)
	}
	if after.Statusline.IsEnabled() {
		t.Error("disable did not persist")
	}
}

func TestStatuslineStatusSubcommand(t *testing.T) {
	p := statuslinePaths(t, "claude")
	var out bytes.Buffer
	if err := cmdStatuslineWith(p, &out, []string{"status"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"enabled", "provider:", "config:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status output missing %q:\n%s", want, out.String())
		}
	}
}

type countingRegistry struct {
	usages []provider.Usage
	calls  *int
}

func (c countingRegistry) FetchAll(context.Context) []provider.Result {
	*c.calls++
	out := make([]provider.Result, 0, len(c.usages))
	for _, u := range c.usages {
		out = append(out, provider.Result{Name: u.Provider, Usage: u})
	}
	return out
}

func TestStatuslineOnlyAsksSourcesThatCanAnswer(t *testing.T) {
	// Polling every provider to render one of them spends a request against
	// each of the others' rate limits, for output nobody asked for.
	cases := []struct {
		flag         string
		wantClaude   bool
		wantRegistry bool
	}{
		{flag: "claude", wantClaude: true, wantRegistry: false},
		{flag: "codex", wantClaude: false, wantRegistry: true},
		{flag: "all", wantClaude: true, wantRegistry: true},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			p := statuslinePaths(t, "claude")
			regCalls := 0
			f := &fakeFetcher{snap: snapAt(42)}
			reg := countingRegistry{usages: []provider.Usage{codexResult(12)}, calls: &regCalls}

			var out bytes.Buffer
			if err := cmdStatuslineWith(p, &out, []string{"--provider", tc.flag}, f, reg); err != nil {
				t.Fatal(err)
			}
			if got := f.calls > 0; got != tc.wantClaude {
				t.Errorf("claude fetched=%v want %v", got, tc.wantClaude)
			}
			if got := regCalls > 0; got != tc.wantRegistry {
				t.Errorf("registry fetched=%v want %v", got, tc.wantRegistry)
			}
		})
	}
}

func TestStatuslineKeepsCachedDataForSourcesItSkipped(t *testing.T) {
	// Skipping a source must not drop what is already known about it, or
	// switching provider would start from nothing.
	p := statuslinePaths(t, "claude")
	if err := statusline.WriteCache(p.UsageCachePath, statusline.NewCached(snapAt(17),
		[]provider.Usage{codexResult(12)}, time.Now().Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}
	regCalls := 0
	var out bytes.Buffer
	reg := countingRegistry{calls: &regCalls}
	if err := cmdStatuslineWith(p, &out, []string{"--provider", "claude"}, &fakeFetcher{snap: snapAt(42)}, reg); err != nil {
		t.Fatal(err)
	}
	c, err := statusline.ReadCache(p.UsageCachePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Providers) != 1 {
		t.Errorf("cached provider data was dropped: %+v", c.Providers)
	}
}

func TestStatuslineForecastWarnsOnlyWhenGrounded(t *testing.T) {
	// The warning has to come from observed history, not from the current
	// reading, so the test writes a series the way a running client would.
	now := time.Now()
	p := statuslinePaths(t, "claude")

	writeHistory := func(from, to float64, span time.Duration, resetsIn time.Duration) {
		c := usage.New(p.ClaudeCreds)
		c.DiskCachePath = p.SnapshotCachePath
		var h usage.History
		h.Record(usage.Snapshot{FiveHour: &usage.Bucket{Utilization: from}}, now.Add(-span))
		h.Record(usage.Snapshot{FiveHour: &usage.Bucket{Utilization: to}}, now)
		writeSnapshotCache(t, p.SnapshotCachePath, h, now)
		_ = resetsIn
	}

	// Rising fast, window resets far away → worth warning.
	writeHistory(10, 30, time.Hour, 5*time.Hour)
	snap := usage.Snapshot{FiveHour: &usage.Bucket{Utilization: 30, ResetsAt: now.Add(5 * time.Hour)}}
	if w := exhaustionWarning(p, snap, now); w == "" {
		t.Error("a window on course to run out before its reset should warn")
	} else if !strings.Contains(w, "5h") {
		t.Errorf("the warning should name the window: %q", w)
	}

	// Same rate, but the reset lands first → no warning.
	snapSoon := usage.Snapshot{FiveHour: &usage.Bucket{Utilization: 30, ResetsAt: now.Add(30 * time.Minute)}}
	if w := exhaustionWarning(p, snapSoon, now); w != "" {
		t.Errorf("a reset that lands first is not a problem, got %q", w)
	}

	// No history at all → silence, not a guess.
	empty := config.ForHome(t.TempDir())
	if w := exhaustionWarning(empty, snap, now); w != "" {
		t.Errorf("without observation there is nothing to say, got %q", w)
	}
}

// writeSnapshotCache seeds the shared cache the way usage.Client would.
func writeSnapshotCache(t *testing.T, path string, h usage.History, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{
		"snapshot":  usage.Snapshot{},
		"stored_at": now,
		"history":   h,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func codexAt(util float64, fetched time.Time) provider.Usage {
	return provider.Usage{
		Provider:  "Codex",
		Windows:   []provider.Window{{Label: "7d", Utilization: util, ResetsAt: fetched.Add(72 * time.Hour)}},
		Source:    "opencode",
		FetchedAt: fetched,
	}
}

func TestStatuslineClaudeRefreshDoesNotRenewProviderCache(t *testing.T) {
	// Rendering Claude must not renew the lease on provider data it never
	// fetched. Writing the whole file back with one fresh timestamp meant a bar
	// sitting on Claude kept the Codex entry alive indefinitely — observed in
	// the wild as a Codex percentage frozen for over an hour while the endpoint
	// had long since moved on.
	p := statuslinePaths(t, "claude")
	stale := time.Now().Add(-time.Hour)
	if err := statusline.WriteCache(p.UsageCachePath, statusline.NewCached(snapAt(17), []provider.Usage{codexAt(0, stale)}, stale)); err != nil {
		t.Fatal(err)
	}

	f := &fakeFetcher{snap: snapAt(42)}
	var regCalls int
	reg := countingRegistry{usages: []provider.Usage{codexAt(4, time.Now())}, calls: &regCalls}
	var out bytes.Buffer

	// Claude only: the registry is deliberately not consulted.
	if err := cmdStatuslineWith(p, &out, []string{"--provider", "claude"}, f, reg); err != nil {
		t.Fatal(err)
	}
	if regCalls != 0 {
		t.Fatalf("a claude-only render must not fetch providers, got %d calls", regCalls)
	}

	// Codex was last fetched an hour ago, so asking for it now must hit the
	// registry rather than serve whatever the previous run copied forward.
	out.Reset()
	if err := cmdStatuslineWith(p, &out, []string{"--provider", "codex"}, f, reg); err != nil {
		t.Fatal(err)
	}
	if regCalls != 1 {
		t.Fatalf("stale provider data must be refetched, got %d registry calls", regCalls)
	}
	if got := strings.TrimSpace(out.String()); !strings.Contains(got, "4%") {
		t.Errorf("got %q, want the freshly fetched 4%%", got)
	}
}

func TestStatuslineProviderRefreshDoesNotRenewSnapshotCache(t *testing.T) {
	// The same defect in the other direction: rendering Codex carries the
	// Anthropic snapshot forward, and must not mark it fresh in the process.
	p := statuslinePaths(t, "claude")
	stale := time.Now().Add(-time.Hour)
	if err := statusline.WriteCache(p.UsageCachePath, statusline.NewCached(snapAt(17), []provider.Usage{codexAt(0, stale)}, stale)); err != nil {
		t.Fatal(err)
	}

	f := &fakeFetcher{snap: snapAt(42)}
	var regCalls int
	reg := countingRegistry{usages: []provider.Usage{codexAt(4, time.Now())}, calls: &regCalls}
	var out bytes.Buffer

	if err := cmdStatuslineWith(p, &out, []string{"--provider", "codex"}, f, reg); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatalf("a codex-only render must not fetch Anthropic, got %d calls", f.calls)
	}

	out.Reset()
	if err := cmdStatuslineWith(p, &out, []string{"--provider", "claude"}, f, reg); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("stale snapshot must be refetched, got %d fetches", f.calls)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 42%" {
		t.Errorf("got %q, want the freshly fetched 5h 42%%", got)
	}
}

func TestStatuslineDefaultsDoNotReadTheHostTerminal(t *testing.T) {
	// Without a pinned provider the command asks the *host's* tmux which agent
	// is in the active pane, so the suite's result depended on what the
	// developer happened to be looking at: the same test resolved to Claude in
	// one run and Codex in the next, failing half a dozen assertions for
	// reasons that had nothing to do with the code under test.
	//
	// A fake tmux on PATH reports an opencode pane, which the default agent map
	// resolves to Codex. A test that means to exercise the Claude path must be
	// unmoved by that.
	dir := t.TempDir()
	fake := filepath.Join(dir, "tmux")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'opencode\\t1\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "/tmp/fake-tmux,1,0")

	p := statuslinePaths(t, "claude")
	f := &fakeFetcher{snap: snapAt(42)}
	var out bytes.Buffer
	if err := cmdStatuslineWith(p, &out, nil, f, emptyRegistry{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "5h 42%" {
		t.Errorf("got %q, want the Claude path regardless of the host terminal", got)
	}
	if f.calls != 1 {
		t.Errorf("expected one Anthropic fetch, got %d", f.calls)
	}
}
