package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/statusline"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// snapshotFetcher is the slice of usage.Client this command needs, so tests can
// substitute it without a network.
type snapshotFetcher interface {
	Get(ctx context.Context) (usage.Snapshot, error)
}

// registryFetcher is the same for the provider registry.
type registryFetcher interface {
	FetchAll(ctx context.Context) []provider.Result
}

func cmdStatusline(args []string) error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	return cmdStatuslineWith(p, os.Stdout, args, nil, nil)
}

// cmdStatuslineWith renders the status line.
//
// Contract: it prints something useful or nothing at all, and it exits zero
// either way. A status bar is not a place to learn that a token expired, and a
// non-zero exit there just leaves a gap in the bar with no explanation.
//
// fetch and registry are injected by tests; nil means build the real ones.
func cmdStatuslineWith(p config.Paths, out io.Writer, args []string, fetch snapshotFetcher, registry registryFetcher) error {
	settings, _ := config.Load(p.ConfigPath) // missing file yields defaults

	// Subcommands come before flags so `statusline disable` reads naturally and
	// cannot be confused with a value for some flag.
	if len(args) > 0 {
		switch args[0] {
		case "enable":
			return setStatuslineEnabled(p, out, true)
		case "disable":
			return setStatuslineEnabled(p, out, false)
		case "status":
			state := "disabled"
			if settings.Statusline.IsEnabled() {
				state = "enabled"
			}
			_, _ = fmt.Fprintf(out, "statusline: %s\n", state)
			_, _ = fmt.Fprintf(out, "provider:   %s\n", providerOrDefault(settings))
			_, _ = fmt.Fprintf(out, "config:     %s\n", p.ConfigPath)
			return nil
		}
	}

	fs := flag.NewFlagSet("statusline", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		prov = fs.String("provider", "",
			`which quota to show: a name, "all", or "auto" to follow the active pane (default: config)`)
		format  = fs.String("format", "compact", "output format: compact, plain or json")
		colour  statusline.ColourMode
		labels  = fs.Bool("labels", false, "prefix each group with its provider name")
		prefix  = fs.String("prefix", "", "text emitted before the output, only when there is output")
		reset   = fs.Bool("reset", false, "append the time left in the first window")
		refresh = fs.Bool("refresh", false, "ignore the cache and fetch now")
		ttl     = fs.Duration("ttl", statusline.DefaultTTL, "how long a cached snapshot is reused")
		timeout = fs.Duration("timeout", 3*time.Second, "budget for a live fetch")
		warnAt  = fs.Float64("warn-at", 60, "utilisation percentage that turns the segment amber")
		critAt  = fs.Float64("crit-at", 85, "utilisation percentage that turns the segment red")
	)
	fs.Var(statusline.ColourFlagValue(&colour), "color",
		"colourise output: none, auto, tmux or ansi (bare --color means auto)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// A disabled status line prints nothing and exits zero, so a bar can keep
	// calling it and simply show an empty segment. Checked before any work:
	// disabled means no network, no cache read, nothing.
	if !settings.Statusline.IsEnabled() {
		return nil
	}

	// Precedence: flag, then config, then "auto". An empty flag value is what
	// tmux passes when its user option is unset, so it must mean "unset" and
	// not "show nothing".
	want := strings.TrimSpace(*prov)
	if want == "" {
		want = strings.TrimSpace(settings.Statusline.Provider)
	}
	if want == "" {
		want = statusline.ProviderAuto
	}
	if strings.EqualFold(want, statusline.ProviderAuto) {
		want = resolveAuto(settings)
	}

	opts := statusline.Options{
		Format:     statusline.Format(*format),
		Provider:   want,
		ShowLabels: *labels,
		Prefix:     *prefix,
		Colour:     colour,
		Reset:      *reset,
		WarnAt:     *warnAt,
		CritAt:     *critAt,
	}
	now := time.Now()

	// Serve the cache when it is fresh. This is the common path and the whole
	// point: a bar redrawing every two seconds must not touch the network.
	cached, cacheErr := statusline.ReadCache(p.UsageCachePath)
	haveCache := cacheErr == nil
	if haveCache && !*refresh && cached.Fresh(now, *ttl) {
		return emit(out, cached.Snapshot, cached.Providers, opts)
	}

	if fetch == nil {
		fetch = usage.New(p.ClaudeCreds)
	}
	if registry == nil {
		registry = defaultRegistry(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	snap, snapErr := fetch.Get(ctx)

	// Registry providers are independent of Anthropic and of each other; one
	// failing must not hide the rest. Errors are dropped rather than cached, so
	// a provider that is merely logged-out disappears instead of sticking.
	var usages []provider.Usage
	for _, r := range registry.FetchAll(ctx) {
		if r.Err == nil && len(r.Usage.Windows) > 0 {
			usages = append(usages, r.Usage)
		}
	}

	if snapErr != nil && len(usages) == 0 {
		// Stale beats blank. A quota from a minute ago still tells you roughly
		// where you stand; an empty bar tells you nothing and looks like a bug.
		if haveCache {
			return emit(out, cached.Snapshot, cached.Providers, opts)
		}
		return nil
	}
	if snapErr != nil && haveCache {
		// Keep the last good Anthropic reading rather than dropping it because
		// this one refresh failed.
		snap = cached.Snapshot
	}

	// A cache write failure is not worth failing the render over — the numbers
	// are already in hand, and the only cost is fetching again next time.
	_ = statusline.WriteCache(p.UsageCachePath, snap, usages, now)
	return emit(out, snap, usages, opts)
}

// resolveAuto picks a provider from the agent in the active tmux pane, falling
// back to Anthropic when there is nothing to go on — outside tmux, or in a pane
// running something we do not recognise.
// setStatuslineEnabled flips the config key and persists it.
//
// It writes the whole settings file, which is how config.Save works elsewhere
// in this command set: the file is rewritten from the merged defaults, so keys
// the user never set appear with their default rather than vanishing.
func setStatuslineEnabled(p config.Paths, out io.Writer, on bool) error {
	settings, err := config.Load(p.ConfigPath)
	if err != nil {
		return err
	}
	settings.Statusline.Enabled = &on
	if err := config.Save(p.ConfigPath, settings); err != nil {
		return err
	}
	state := "disabled"
	if on {
		state = "enabled"
	}
	_, _ = fmt.Fprintf(out, "statusline %s (%s)\n", state, p.ConfigPath)
	if on {
		_, _ = fmt.Fprintln(out, "add this to your status bar if you have not already:")
		_, _ = fmt.Fprintln(out, `  #(command -v claudeops >/dev/null && claudeops statusline --color)`)
	}
	return nil
}

func providerOrDefault(s config.Settings) string {
	if v := strings.TrimSpace(s.Statusline.Provider); v != "" {
		return v
	}
	return statusline.ProviderAuto
}

func resolveAuto(s config.Settings) string {
	if name := statusline.DetectAgent(s.Statusline.Agents); name != "" {
		return name
	}
	return statusline.ClaudeProvider
}

func defaultRegistry(p config.Paths) *provider.Registry {
	r := provider.NewRegistry(
		provider.NewCodex(),
		provider.NewCopilot(),
		provider.NewGemini(),
	)
	if gens, err := provider.LoadGeneric(filepath.Join(p.DataDir, "providers.toml")); err == nil {
		for _, g := range gens {
			r.Register(g)
		}
	}
	return r
}

func emit(out io.Writer, snap usage.Snapshot, providers []provider.Usage, opts statusline.Options) error {
	s, err := statusline.Render(snap, providers, opts)
	if err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	// plain already ends in a newline; compact and json do not.
	if opts.Format == statusline.FormatPlain {
		_, _ = fmt.Fprint(out, s)
		return nil
	}
	_, _ = fmt.Fprintln(out, s)
	return nil
}
