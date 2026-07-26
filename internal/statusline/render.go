package statusline

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// ProviderAll and ProviderAuto are the two non-literal values accepted wherever
// a provider name is expected.
const (
	ProviderAll  = "all"
	ProviderAuto = "auto"
)

// ClaudeProvider is the name of the Anthropic source. It does not come from the
// provider registry — it has its own client — but it is selectable by the same
// name so users do not have to care about that split.
const ClaudeProvider = "claude"

// Format selects the output shape.
type Format string

const (
	// FormatCompact is one line for a status bar: "5h 42% · 7d 18%".
	FormatCompact Format = "compact"
	// FormatPlain is one bucket per line, for scripts and eyeballs.
	FormatPlain Format = "plain"
	// FormatJSON is the whole snapshot, for anything that wants the numbers.
	FormatJSON Format = "json"
)

// Options controls rendering.
type Options struct {
	Format Format
	// Provider selects the source to show: a name, ProviderAll, or "" which
	// behaves like ProviderAll. Resolution of ProviderAuto happens before this
	// point, in the command, because it needs to inspect tmux.
	Provider string
	// ShowLabels prefixes each group with its provider name. Left off for a
	// single source, where the prefix is noise.
	ShowLabels bool
	// Prefix is emitted before the first segment, and only when there is
	// something to show. A status bar with a label but no value reads as
	// broken, so an empty render stays empty.
	Prefix string
	// Colour selects the escape syntax. Off by default so the output stays
	// usable anywhere; see ColourMode for why tmux and ANSI cannot be the same.
	Colour ColourMode
	// Threshold percentages at which compact output switches colour.
	WarnAt float64 // default 60
	CritAt float64 // default 85
	// Reset shows the time remaining in the 5h window.
	Reset bool
	// Now is injected by tests. Zero means time.Now().
	Now time.Time
}

func (o Options) now() time.Time {
	if o.Now.IsZero() {
		return time.Now()
	}
	return o.Now
}

func (o Options) warnAt() float64 {
	if o.WarnAt == 0 {
		return 60
	}
	return o.WarnAt
}

func (o Options) critAt() float64 {
	if o.CritAt == 0 {
		return 85
	}
	return o.CritAt
}

// tmux colours, matching the palette the TUI already uses for spend.
const (
	colourOK   = "#9ece6a"
	colourWarn = "#e0af68"
	colourCrit = "#f7768e"
	colourOff  = "#[default]"
)

// Render turns a snapshot into a status line.
//
// Buckets the plan does not have come back nil from the API, so every one is
// optional and a plan with no quota at all renders as an empty string rather
// than a row of zeroes.
func Render(snap usage.Snapshot, providers []provider.Usage, o Options) (string, error) {
	groups := selectGroups(snap, providers, o.Provider)
	switch o.Format {
	case FormatJSON:
		b, err := json.Marshal(groups)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case FormatPlain:
		return renderPlain(groups, o), nil
	default:
		return renderCompact(groups, o), nil
	}
}

// Group is one source's windows, ready to render.
//
// The JSON shape is declared here rather than reusing provider.Window so the
// output stays snake_case and stable: this is a public interface for scripts,
// and it should not shift because an internal struct gained a field.
type Group struct {
	Provider string   `json:"provider"`
	Windows  []Window `json:"windows"`
	Note     string   `json:"note,omitempty"`
}

// Window is one quota window in a Group.
type Window struct {
	Label       string    `json:"label"`
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at,omitzero"`
}

func toWindows(in []provider.Window) []Window {
	out := make([]Window, 0, len(in))
	for _, w := range in {
		out = append(out, Window{Label: w.Label, Utilization: w.Utilization, ResetsAt: w.ResetsAt})
	}
	return out
}

// selectGroups flattens both sources into a common shape and keeps only what
// was asked for. An unknown name yields nothing, which renders as an empty
// segment — the same outcome as a provider you have no credentials for, and
// the right one for a status bar.
func selectGroups(snap usage.Snapshot, providers []provider.Usage, want string) []Group {
	want = strings.ToLower(strings.TrimSpace(want))
	all := want == "" || want == ProviderAll

	var groups []Group
	if all || want == ClaudeProvider {
		if w := ClaudeWindows(snap); len(w) > 0 {
			groups = append(groups, Group{
				Provider: ClaudeProvider,
				Windows:  toWindows(w),
				Note:     claudeNote(snap),
			})
		}
	}
	for _, p := range providers {
		name := strings.ToLower(p.Provider)
		if !all && name != want {
			continue
		}
		if len(p.Windows) == 0 {
			continue
		}
		groups = append(groups, Group{Provider: name, Windows: toWindows(p.Windows), Note: p.Note})
	}
	return groups
}

// ClaudeWindows converts the Anthropic snapshot into the shared window shape.
// Buckets the plan does not have come back nil, so each is optional and a plan
// with none yields an empty slice rather than a row of zeroes.
func ClaudeWindows(snap usage.Snapshot) []provider.Window {
	out := make([]provider.Window, 0, 4)
	add := func(label string, b *usage.Bucket) {
		if b != nil {
			out = append(out, provider.Window{Label: label, Utilization: b.Utilization, ResetsAt: b.ResetsAt})
		}
	}
	add("5h", snap.FiveHour)
	add("7d", snap.SevenDay)
	for _, nb := range snap.PerModelBuckets() {
		out = append(out, provider.Window{Label: nb.Label, Utilization: nb.Bucket.Utilization, ResetsAt: nb.Bucket.ResetsAt})
	}
	if x := snap.ExtraUsage; x != nil && x.IsEnabled && x.Utilization != nil {
		out = append(out, provider.Window{Label: "extra", Utilization: *x.Utilization})
	}
	return out
}

// claudeNote surfaces the pay-as-you-go balance, which is a currency amount and
// so has no place among the percentage windows. Providers use Note the same way
// for their plan name.
func claudeNote(snap usage.Snapshot) string {
	x := snap.ExtraUsage
	if x == nil || !x.IsEnabled || x.UsedCredits == nil || x.MonthlyLimit == nil {
		return ""
	}
	return fmt.Sprintf("$%.2f of $%.2f", *x.UsedCredits, *x.MonthlyLimit)
}

func renderCompact(groups []Group, o Options) string {
	var parts []string
	for _, g := range groups {
		var segs []string
		if o.ShowLabels {
			segs = append(segs, g.Provider)
		}
		for _, w := range g.Windows {
			seg := o.Colour.wrap(fmt.Sprintf("%s %.0f%%", w.Label, w.Utilization), o.level(w.Utilization))
			segs = append(segs, seg)
		}
		if o.Reset && len(g.Windows) > 0 {
			if left := g.Windows[0].ResetsAt.Sub(o.now()); left > 0 {
				segs = append(segs, "↻"+shortDuration(left))
			}
		}
		// Windows within a source read as one reading, so they keep the light
		// separator. Sources are different readings and get a heavier one, which
		// also stops "claude 5h 6% codex 5h 12%" from looking like four windows
		// of the same thing.
		if o.ShowLabels && len(segs) > 1 {
			parts = append(parts, segs[0]+" "+strings.Join(segs[1:], " · "))
		} else {
			parts = append(parts, strings.Join(segs, " · "))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := strings.Join(parts, " │ ")
	if o.Prefix != "" {
		out = o.Prefix + " " + out
	}
	return out
}

func renderPlain(groups []Group, o Options) string {
	var sb strings.Builder
	if o.Prefix != "" && len(groups) > 0 {
		fmt.Fprintf(&sb, "%s\n", o.Prefix)
	}
	for _, g := range groups {
		if o.ShowLabels {
			fmt.Fprintf(&sb, "%s\n", g.Provider)
		}
		for _, w := range g.Windows {
			fmt.Fprintf(&sb, "%-12s %5.1f%%", w.Label, w.Utilization)
			if left := w.ResetsAt.Sub(o.now()); left > 0 {
				fmt.Fprintf(&sb, "  resets in %s", shortDuration(left))
			}
			sb.WriteString("\n")
		}
		if g.Note != "" {
			fmt.Fprintf(&sb, "%s\n", g.Note)
		}
	}
	return sb.String()
}

// level classifies a utilisation against the configured thresholds.
func (o Options) level(util float64) colourLevel {
	switch {
	case util >= o.critAt():
		return levelCrit
	case util >= o.warnAt():
		return levelWarn
	default:
		return levelOK
	}
}

// shortDuration renders a duration the way a status bar wants it: "4d12h",
// "2h12m", "45m". Two units at most, largest first.
//
// The seven day window makes the day unit worth having: without it a fresh
// weekly quota reads as "167h58m", which is accurate and useless.
func shortDuration(d time.Duration) string {
	// Check before rounding: 30s rounds up to a minute, and reporting "1m" for
	// something about to expire is the wrong way to be wrong.
	if d < time.Minute {
		return "<1m"
	}
	d = d.Round(time.Minute)
	if days := int(d / (24 * time.Hour)); days > 0 {
		return fmt.Sprintf("%dd%02dh", days, int((d%(24*time.Hour))/time.Hour))
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
