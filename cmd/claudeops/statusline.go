package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/statusline"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// snapshotFetcher is the slice of usage.Client this command needs, so tests can
// substitute it without a network.
type snapshotFetcher interface {
	Get(ctx context.Context) (usage.Snapshot, error)
}

func cmdStatusline(args []string) error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	return cmdStatuslineWith(p, os.Stdout, args, nil)
}

// cmdStatuslineWith renders the status line.
//
// Contract: it prints something useful or nothing at all, and it exits zero
// either way. A status bar is not a place to learn that a token expired, and a
// non-zero exit there just leaves a gap in the bar with no explanation.
//
// fetch is injected by tests; nil means build a real client from p.
func cmdStatuslineWith(p config.Paths, out io.Writer, args []string, fetch snapshotFetcher) error {
	fs := flag.NewFlagSet("statusline", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		format  = fs.String("format", "compact", "output format: compact, plain or json")
		color   = fs.Bool("color", false, "wrap compact output in tmux colour escapes")
		reset   = fs.Bool("reset", false, "append the time left in the 5h window")
		refresh = fs.Bool("refresh", false, "ignore the cache and fetch now")
		ttl     = fs.Duration("ttl", statusline.DefaultTTL, "how long a cached snapshot is reused")
		timeout = fs.Duration("timeout", 3*time.Second, "budget for a live fetch")
		warnAt  = fs.Float64("warn-at", 60, "utilisation percentage that turns the segment amber")
		critAt  = fs.Float64("crit-at", 85, "utilisation percentage that turns the segment red")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	opts := statusline.Options{
		Format: statusline.Format(*format),
		Color:  *color,
		Reset:  *reset,
		WarnAt: *warnAt,
		CritAt: *critAt,
	}
	now := time.Now()

	// Serve the cache when it is fresh. This is the common path and the whole
	// point: a bar redrawing every two seconds must not touch the network.
	cached, cacheErr := statusline.ReadCache(p.UsageCachePath)
	haveCache := cacheErr == nil
	if haveCache && !*refresh && cached.Fresh(now, *ttl) {
		return emit(out, cached.Snapshot, opts)
	}

	if fetch == nil {
		fetch = usage.New(p.ClaudeCreds)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	snap, err := fetch.Get(ctx)
	if err != nil {
		// Stale beats blank. A quota from a minute ago still tells you roughly
		// where you stand; an empty bar tells you nothing and looks like a bug.
		if haveCache {
			return emit(out, cached.Snapshot, opts)
		}
		return nil
	}

	// A cache write failure is not worth failing the render over — the number
	// is already in hand, and the only cost is fetching again next time.
	_ = statusline.WriteCache(p.UsageCachePath, snap, now)
	return emit(out, snap, opts)
}

func emit(out io.Writer, snap usage.Snapshot, opts statusline.Options) error {
	s, err := statusline.Render(snap, opts)
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
