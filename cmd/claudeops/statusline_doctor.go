package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/statusline"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// Doctor outcomes, worst last. The ordering is the exit-code rule: anything at
// stateBroken means something needs fixing, everything below it does not.
type doctorState int

const (
	stateOK     doctorState = iota // answered, has data
	stateAbsent                    // no credentials; you do not use this service
	stateEmpty                     // authenticated, but the plan reports no quota
	stateStale                     // live fetch failed, cached data still serving
	stateBroken                    // nothing usable
)

func (s doctorState) label() string {
	switch s {
	case stateOK:
		return "ok"
	case stateAbsent:
		return "absent"
	case stateEmpty:
		return "empty"
	case stateStale:
		return "stale"
	default:
		return "error"
	}
}

type doctorRow struct {
	name   string
	state  doctorState
	detail string
	remedy string
}

// cmdStatuslineDoctor explains what the status line can and cannot see.
//
// The status line is deliberately silent: a provider you are logged out of is
// dropped rather than reported, because a bar is no place to learn that a token
// expired. That is right for the bar and useless when you are asking why a
// provider is missing. This is where that question gets answered.
//
// It exits non-zero only when something is actually broken. A service you do
// not use is not a problem, and neither is a failed refresh while the cache
// still has an answer — the bar is fine in both cases.
func cmdStatuslineDoctor(p config.Paths, out io.Writer, settings config.Settings,
	fetch snapshotFetcher, registry registryFetcher, timeout time.Duration) error {

	cached, cacheErr := statusline.ReadCache(p.UsageCachePath)
	haveCache := cacheErr == nil

	_, _ = fmt.Fprintf(out, "config      %s\n", p.ConfigPath)
	_, _ = fmt.Fprintf(out, "statusline  %s\n", enabledLabel(settings))
	_, _ = fmt.Fprintf(out, "provider    %s%s\n", providerOrDefault(settings), autoNote(settings))
	_, _ = fmt.Fprintf(out, "cache       %s\n", cacheLine(cached, haveCache))
	_, _ = fmt.Fprintln(out)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if fetch == nil {
		fetch = usage.New(p.ClaudeCreds)
	}
	if registry == nil {
		registry = defaultRegistry(p)
	}

	rows := []doctorRow{claudeRow(ctx, fetch, p, cached, haveCache)}
	seen := map[string]bool{statusline.ClaudeProvider: true}

	for _, r := range registry.FetchAll(ctx) {
		name := strings.ToLower(r.Name)
		seen[name] = true
		rows = append(rows, registryRow(name, r, cached, haveCache))
	}

	// Providers with no credentials never reach FetchAll, so name them anyway.
	// "absent" is the answer to "why is Copilot missing"; silence is not.
	for _, known := range []string{"codex", "copilot", "gemini"} {
		if !seen[known] {
			rows = append(rows, doctorRow{
				name:   known,
				state:  stateAbsent,
				detail: "no credentials found",
				remedy: remedyFor(known),
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	worst := stateOK
	for _, r := range rows {
		_, _ = fmt.Fprintf(out, "%-9s %-7s %s\n", r.name, r.state.label(), r.detail)
		if r.remedy != "" {
			_, _ = fmt.Fprintf(out, "%-17s → %s\n", "", r.remedy)
		}
		if r.state > worst {
			worst = r.state
		}
	}

	if agents := settings.Statusline.Agents; len(agents) > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "agent mapping")
		keys := make([]string, 0, len(agents))
		for k := range agents {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = fmt.Fprintf(out, "  %-10s → %s\n", k, agents[k])
		}
	}

	if worst >= stateBroken {
		return errors.New("statusline: at least one provider is not usable")
	}
	return nil
}

func claudeRow(ctx context.Context, fetch snapshotFetcher, p config.Paths,
	cached statusline.Cached, haveCache bool) doctorRow {

	r := doctorRow{name: statusline.ClaudeProvider}
	snap, err := fetch.Get(ctx)
	if err != nil {
		// A failed refresh is not the same as being broken. With a usable cache
		// the bar keeps working, so this is worth knowing and not worth acting
		// on — rate limiting in particular clears itself.
		if haveCache && len(statusline.ClaudeWindows(cached.Snapshot)) > 0 {
			r.state = stateStale
			r.detail = fmt.Sprintf("%s; serving cache from %s ago",
				err, cached.Age(time.Now()).Round(time.Second))
			return r
		}
		r.state = stateBroken
		r.detail = err.Error()
		r.remedy = "check " + p.ClaudeCreds + ", or sign in to Claude Code again"
		return r
	}

	windows := statusline.ClaudeWindows(snap)
	if len(windows) == 0 {
		r.state = stateEmpty
		r.detail = "authenticated, but this plan reports no quota windows"
		return r
	}
	r.state = stateOK
	r.detail = summariseWindows(windows, "", "")
	return r
}

func registryRow(name string, res provider.Result, cached statusline.Cached, haveCache bool) doctorRow {
	r := doctorRow{name: name}

	if res.Err != nil {
		if haveCache {
			for _, u := range cached.Providers {
				if strings.EqualFold(u.Provider, name) && len(u.Windows) > 0 {
					r.state = stateStale
					r.detail = fmt.Sprintf("%s; serving cache from %s ago",
						res.Err, cached.Age(time.Now()).Round(time.Second))
					return r
				}
			}
		}
		r.state = stateBroken
		r.detail = res.Err.Error()
		r.remedy = remedyFor(name)
		return r
	}

	if len(res.Usage.Windows) == 0 {
		// Answered, but with nothing in it. Distinct from an error: the
		// credentials work, the plan simply has no tracked quota.
		r.state = stateEmpty
		r.detail = "authenticated, but this plan reports no quota windows"
		return r
	}

	r.state = stateOK
	r.detail = summariseWindows(res.Usage.Windows, res.Usage.Note, res.Usage.Source)
	return r
}

func summariseWindows(windows []provider.Window, note, source string) string {
	parts := make([]string, 0, len(windows))
	for _, w := range windows {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", w.Label, w.Utilization))
	}
	s := strings.Join(parts, " · ")

	var extra []string
	if note != "" {
		extra = append(extra, note)
	}
	// Which credential answered. This is the line that turns "run codex login"
	// into "you are already signed in, through opencode".
	if source != "" {
		extra = append(extra, "via "+source)
	}
	if len(extra) > 0 {
		s += "  (" + strings.Join(extra, ", ") + ")"
	}
	return s
}

func remedyFor(name string) string {
	switch name {
	case "codex":
		return "run `codex login`, or sign in to openai through opencode"
	case "copilot":
		return "sign in with the GitHub Copilot CLI, or your editor's Copilot extension"
	case "gemini":
		return "run `gemini auth`, or sign in to google through opencode"
	default:
		return ""
	}
}

func enabledLabel(s config.Settings) string {
	if s.Statusline.IsEnabled() {
		return "enabled"
	}
	return "disabled  (claudeops statusline enable)"
}

// autoNote spells out what auto resolved to, since that is the part nobody can
// see for themselves.
func autoNote(s config.Settings) string {
	if !strings.EqualFold(providerOrDefault(s), statusline.ProviderAuto) {
		return ""
	}
	if name := statusline.DetectAgent(s.Statusline.Agents); name != "" {
		return fmt.Sprintf("  (active pane → %s)", name)
	}
	return fmt.Sprintf("  (nothing recognised in the active pane, falling back to %s)", statusline.ClaudeProvider)
}

func cacheLine(c statusline.Cached, ok bool) string {
	if !ok {
		return "empty  (populated on the next run)"
	}
	return fmt.Sprintf("%s old", c.Age(time.Now()).Round(time.Second))
}
