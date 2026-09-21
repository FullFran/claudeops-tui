package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeProvider is a controllable Provider for registry tests.
type fakeProvider struct {
	name      string
	available bool
	usage     Usage
	err       error
	fetched   bool
	calls     int
}

func (f *fakeProvider) Name() string    { return f.name }
func (f *fakeProvider) Available() bool { return f.available }
func (f *fakeProvider) Fetch(ctx context.Context) (Usage, error) {
	f.fetched = true
	f.calls++
	return f.usage, f.err
}

// fakeLocalProvider is a fakeProvider that also declares itself local, the
// way Antigravity does: it reads on-disk state a sibling process may have
// just rewritten, so the registry must never serve it from the TTL cache or
// back it off after an error.
type fakeLocalProvider struct {
	fakeProvider
}

func (f *fakeLocalProvider) Local() bool { return true }

func TestRegistryFetchAll(t *testing.T) {
	tests := []struct {
		name      string
		providers []*fakeProvider
		wantNames []string
	}{
		{
			name: "skips unavailable providers",
			providers: []*fakeProvider{
				{name: "Claude", available: true, usage: Usage{Provider: "Claude"}},
				{name: "Codex", available: false},
			},
			wantNames: []string{"Claude"},
		},
		{
			name: "captures per-provider error without aborting others",
			providers: []*fakeProvider{
				{name: "Claude", available: true, err: errors.New("boom")},
				{name: "Codex", available: true, usage: Usage{Provider: "Codex"}},
			},
			wantNames: []string{"Claude", "Codex"},
		},
		{
			name:      "empty registry yields no results",
			providers: nil,
			wantNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{}
			for _, p := range tt.providers {
				r.Register(p)
			}

			results := r.FetchAll(context.Background())

			if len(results) != len(tt.wantNames) {
				t.Fatalf("got %d results, want %d", len(results), len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if results[i].Name != want {
					t.Errorf("result[%d].Name = %q, want %q", i, results[i].Name, want)
				}
			}
			// Unavailable providers must never be fetched.
			for _, p := range tt.providers {
				if !p.available && p.fetched {
					t.Errorf("provider %q was fetched despite being unavailable", p.name)
				}
			}
		})
	}
}

func TestRegistryFetchAllPropagatesError(t *testing.T) {
	wantErr := errors.New("auth expired")
	r := NewRegistry(&fakeProvider{name: "Codex", available: true, err: wantErr})

	results := r.FetchAll(context.Background())

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if !errors.Is(results[0].Err, wantErr) {
		t.Errorf("Err = %v, want %v", results[0].Err, wantErr)
	}
}

func TestRegistryCachesSuccessfulFetches(t *testing.T) {
	tests := []struct {
		name      string
		ttl       time.Duration
		advance   time.Duration
		wantCalls int
	}{
		{name: "second call inside the TTL is served from cache", ttl: time.Minute, advance: 30 * time.Second, wantCalls: 1},
		{name: "second call after the TTL refetches", ttl: time.Minute, advance: 2 * time.Minute, wantCalls: 2},
		{name: "zero TTL falls back to the default", ttl: 0, advance: time.Minute, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &fakeProvider{name: "Codex", available: true, usage: Usage{Provider: "Codex"}}
			now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
			r := NewRegistry(p)
			r.TTL = tt.ttl
			r.now = func() time.Time { return now }

			first := r.FetchAll(context.Background())
			now = now.Add(tt.advance)
			second := r.FetchAll(context.Background())

			if p.calls != tt.wantCalls {
				t.Errorf("Fetch called %d times, want %d", p.calls, tt.wantCalls)
			}
			if len(first) != 1 || len(second) != 1 {
				t.Fatalf("want one result per call, got %d and %d", len(first), len(second))
			}
			if second[0].Usage.Provider != "Codex" || second[0].Err != nil {
				t.Errorf("cached result wrong: %+v", second[0])
			}
		})
	}
}

func TestRegistryBacksOffFailingProviders(t *testing.T) {
	tests := []struct {
		name      string
		advances  []time.Duration
		wantCalls int
	}{
		{name: "failures inside the backoff window are not retried", advances: []time.Duration{10 * time.Second, 10 * time.Second}, wantCalls: 1},
		{name: "retry once the first window elapses", advances: []time.Duration{2 * time.Minute}, wantCalls: 2},
		{name: "window doubles after each consecutive failure", advances: []time.Duration{2 * time.Minute, 90 * time.Second}, wantCalls: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantErr := errors.New("network down")
			p := &fakeProvider{name: "Codex", available: true, err: wantErr}
			now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
			r := NewRegistry(p)
			r.ErrBackoff = time.Minute
			r.now = func() time.Time { return now }

			results := r.FetchAll(context.Background())
			for _, adv := range tt.advances {
				now = now.Add(adv)
				results = r.FetchAll(context.Background())
			}

			if p.calls != tt.wantCalls {
				t.Errorf("Fetch called %d times, want %d", p.calls, tt.wantCalls)
			}
			if len(results) != 1 || !errors.Is(results[0].Err, wantErr) {
				t.Errorf("error not surfaced from cache: %+v", results)
			}
		})
	}
}

func TestRegistrySuccessClearsBackoff(t *testing.T) {
	p := &fakeProvider{name: "Codex", available: true, err: errors.New("boom")}
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	r := NewRegistry(p)
	r.TTL = time.Minute
	r.ErrBackoff = time.Minute
	r.now = func() time.Time { return now }

	r.FetchAll(context.Background())
	now = now.Add(2 * time.Minute)
	p.err = nil
	p.usage = Usage{Provider: "Codex"}
	r.FetchAll(context.Background())

	// After a success the failure streak resets, so a later failure waits only
	// the base backoff again.
	now = now.Add(2 * time.Minute)
	p.err = errors.New("boom again")
	r.FetchAll(context.Background())
	now = now.Add(90 * time.Second)
	r.FetchAll(context.Background())

	if p.calls != 4 {
		t.Errorf("Fetch called %d times, want 4", p.calls)
	}
}

func TestRegistryFetchAllAlwaysFetchesLocalProviders(t *testing.T) {
	local := &fakeLocalProvider{fakeProvider{name: "Antigravity", available: true, usage: Usage{Provider: "Antigravity"}}}
	network := &fakeProvider{name: "Codex", available: true, usage: Usage{Provider: "Codex"}}
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	r := NewRegistry(local, network)
	r.TTL = time.Minute
	r.now = func() time.Time { return now }

	r.FetchAll(context.Background())
	now = now.Add(10 * time.Second) // well within TTL
	results := r.FetchAll(context.Background())

	if local.calls != 2 {
		t.Errorf("local provider fetched %d times, want 2 (never served from the TTL cache)", local.calls)
	}
	if network.calls != 1 {
		t.Errorf("network provider fetched %d times, want 1 (still served from cache within TTL)", network.calls)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

func TestRegistryLocalProviderErrorIsNotBackedOff(t *testing.T) {
	wantErr := errors.New("read failed")
	local := &fakeLocalProvider{fakeProvider{name: "Antigravity", available: true, err: wantErr}}
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	r := NewRegistry(local)
	r.ErrBackoff = time.Minute
	r.now = func() time.Time { return now }

	r.FetchAll(context.Background())
	now = now.Add(time.Second) // well inside what would be the network backoff window
	results := r.FetchAll(context.Background())

	if local.calls != 2 {
		t.Errorf("local provider fetched %d times, want 2 (a failing local read is cheap to retry)", local.calls)
	}
	if len(results) != 1 || !errors.Is(results[0].Err, wantErr) {
		t.Errorf("error not surfaced from the fresh fetch: %+v", results)
	}
}

func TestRegistryFetchLocalReturnsOnlyAvailableLocalProviders(t *testing.T) {
	local := &fakeLocalProvider{fakeProvider{name: "Antigravity", available: true, usage: Usage{Provider: "Antigravity"}}}
	network := &fakeProvider{name: "Codex", available: true, usage: Usage{Provider: "Codex"}}
	unavailableLocal := &fakeLocalProvider{fakeProvider{name: "Other", available: false}}
	r := NewRegistry(local, network, unavailableLocal)

	results := r.FetchLocal(context.Background())

	if len(results) != 1 || results[0].Name != "Antigravity" {
		t.Errorf("FetchLocal() = %+v, want only the available local provider", results)
	}
	if network.calls != 0 {
		t.Errorf("FetchLocal must not touch network providers, got %d calls", network.calls)
	}
	if unavailableLocal.calls != 0 {
		t.Errorf("FetchLocal must not fetch an unavailable local provider, got %d calls", unavailableLocal.calls)
	}
}
