package provider

import (
	"context"
	"errors"
	"testing"
)

// fakeProvider is a controllable Provider for registry tests.
type fakeProvider struct {
	name      string
	available bool
	usage     Usage
	err       error
	fetched   bool
}

func (f *fakeProvider) Name() string    { return f.name }
func (f *fakeProvider) Available() bool { return f.available }
func (f *fakeProvider) Fetch(ctx context.Context) (Usage, error) {
	f.fetched = true
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
