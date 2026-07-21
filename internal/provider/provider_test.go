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
