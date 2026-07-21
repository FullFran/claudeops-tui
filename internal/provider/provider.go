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
	"sync"
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

// Cache defaults. The TUI refreshes on a 2s tick, so without caching every
// provider would be polled ~1800 times per hour; these windows keep the poll
// rate close to the usage package's own 5-minute cache.
const (
	DefaultTTL        = 5 * time.Minute
	DefaultErrBackoff = time.Minute
	maxErrBackoff     = 15 * time.Minute
)

// Registry holds the set of registered providers and caches their snapshots.
type Registry struct {
	providers []Provider

	// TTL is how long a successful snapshot is reused. Zero means DefaultTTL.
	TTL time.Duration
	// ErrBackoff is how long a failing provider is skipped; it doubles on each
	// consecutive failure up to 15 minutes. Zero means DefaultErrBackoff.
	ErrBackoff time.Duration

	mu    sync.Mutex
	cache map[string]*cacheEntry
	now   func() time.Time
}

// cacheEntry is one provider's last outcome and how long it stays valid.
type cacheEntry struct {
	result   Result
	until    time.Time
	failures int
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
//
// Results are cached for TTL, and a failing provider is backed off with an
// exponentially growing window, so a caller that ticks every couple of seconds
// still hits each remote endpoint only a few times per hour.
func (r *Registry) FetchAll(ctx context.Context) []Result {
	out := make([]Result, 0, len(r.providers))
	for _, p := range r.providers {
		if !p.Available() {
			continue
		}
		name := p.Name()
		if cached, ok := r.cached(name); ok {
			out = append(out, cached)
			continue
		}
		u, err := p.Fetch(ctx)
		out = append(out, r.store(name, Result{Name: name, Usage: u, Err: err}))
	}
	return out
}

// cached returns the still-valid cached result for name, if any.
func (r *Registry) cached(name string) (Result, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.cache[name]
	if !ok || !r.clock().Before(e.until) {
		return Result{}, false
	}
	return e.result, true
}

// store caches res and returns it. Failures extend the window on each
// consecutive miss; a success resets the streak to the plain TTL.
func (r *Registry) store(name string, res Result) Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		r.cache = map[string]*cacheEntry{}
	}
	e, ok := r.cache[name]
	if !ok {
		e = &cacheEntry{}
		r.cache[name] = e
	}
	e.result = res
	if res.Err != nil {
		e.failures++
		e.until = r.clock().Add(r.errBackoff(e.failures))
		return res
	}
	e.failures = 0
	e.until = r.clock().Add(r.ttl())
	return res
}

func (r *Registry) ttl() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return DefaultTTL
}

// errBackoff doubles the base window per consecutive failure, capped.
func (r *Registry) errBackoff(failures int) time.Duration {
	base := r.ErrBackoff
	if base <= 0 {
		base = DefaultErrBackoff
	}
	d := base
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= maxErrBackoff {
			return maxErrBackoff
		}
	}
	return d
}

func (r *Registry) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}
