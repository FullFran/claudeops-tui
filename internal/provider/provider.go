// Package provider defines a pluggable abstraction over live
// subscription/quota tracking for AI coding services (Claude, Codex, ...).
//
// Each service exposes usage differently — Anthropic via an OAuth usage
// endpoint, OpenAI/Codex via the ChatGPT backend, others via cookies or CLI
// files. A Provider hides those differences behind one normalized Usage
// snapshot so the TUI can render any number of providers uniformly, the way
// CodexBar does on macOS.
package provider

import (
	"context"
	"time"
)

// Window is one normalized quota window for any provider.
//
// Utilization is a percentage in the range [0, 100] to match the TUI's bar
// renderer. ResetsAt is the zero time when the provider does not report a
// reset instant for this window.
type Window struct {
	Label       string
	Utilization float64
	ResetsAt    time.Time
}

// Usage is a provider's normalized live-quota snapshot.
type Usage struct {
	// Provider is the display name, e.g. "Claude" or "Codex".
	Provider string
	// Windows are the quota windows in display order (session, weekly, ...).
	Windows []Window
	// Note is an optional single line of extra context (credits, plan, ...).
	Note string
	// FetchedAt is when this snapshot was retrieved.
	FetchedAt time.Time
}

// Provider fetches live subscription/quota usage for one service.
//
// Implementations must be safe for concurrent use: Available and Fetch may be
// called from the TUI's async refresh command.
type Provider interface {
	// Name is the stable display name, e.g. "Claude" or "Codex".
	Name() string
	// Available reports whether credentials for this provider are present on
	// disk. A false result means the provider is skipped silently (the user
	// simply does not use that service).
	Available() bool
	// Fetch retrieves the current usage snapshot. It should return a
	// descriptive error rather than panicking on auth or transport failure.
	Fetch(ctx context.Context) (Usage, error)
}

// Result pairs a provider's name with the outcome of a fetch so the TUI can
// render a per-provider error line without aborting the others.
type Result struct {
	Name  string
	Usage Usage
	Err   error
}

// Registry holds the set of registered providers.
type Registry struct {
	providers []Provider
}

// NewRegistry builds a Registry from the given providers.
func NewRegistry(providers ...Provider) *Registry {
	return &Registry{providers: providers}
}

// Register appends a provider to the registry.
func (r *Registry) Register(p Provider) {
	r.providers = append(r.providers, p)
}

// Providers returns the registered providers in registration order.
func (r *Registry) Providers() []Provider {
	return r.providers
}

// FetchAll fetches usage for every available provider, in registration order.
// Providers whose Available reports false are skipped entirely. Each fetch is
// isolated: an error from one provider is captured in its Result and never
// prevents the others from being fetched.
func (r *Registry) FetchAll(ctx context.Context) []Result {
	out := make([]Result, 0, len(r.providers))
	for _, p := range r.providers {
		if !p.Available() {
			continue
		}
		u, err := p.Fetch(ctx)
		out = append(out, Result{Name: p.Name(), Usage: u, Err: err})
	}
	return out
}
