package statusline

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fullfran/claudeops-tui/internal/usage"
)

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
	// Color wraps compact output in tmux style escapes. Off by default so the
	// output stays usable in bars that do not speak tmux formatting.
	Color bool
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
func Render(snap usage.Snapshot, o Options) (string, error) {
	switch o.Format {
	case FormatJSON:
		b, err := json.Marshal(snap)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case FormatPlain:
		return renderPlain(snap, o), nil
	default:
		return renderCompact(snap, o), nil
	}
}

func renderCompact(snap usage.Snapshot, o Options) string {
	var parts []string
	for _, b := range buckets(snap) {
		seg := fmt.Sprintf("%s %.0f%%", b.Label, b.Bucket.Utilization)
		if o.Color {
			seg = fmt.Sprintf("#[fg=%s]%s%s", colourFor(b.Bucket.Utilization, o), seg, colourOff)
		}
		parts = append(parts, seg)
	}
	if o.Reset && snap.FiveHour != nil {
		if left := snap.FiveHour.ResetsAt.Sub(o.now()); left > 0 {
			parts = append(parts, "↻"+shortDuration(left))
		}
	}
	if x := snap.ExtraUsage; x != nil && x.IsEnabled && x.Utilization != nil {
		seg := fmt.Sprintf("extra %.0f%%", *x.Utilization)
		if o.Color {
			seg = fmt.Sprintf("#[fg=%s]%s%s", colourFor(*x.Utilization, o), seg, colourOff)
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, " · ")
}

func renderPlain(snap usage.Snapshot, o Options) string {
	var sb strings.Builder
	for _, b := range buckets(snap) {
		fmt.Fprintf(&sb, "%-12s %5.1f%%", b.Label, b.Bucket.Utilization)
		if left := b.Bucket.ResetsAt.Sub(o.now()); left > 0 {
			fmt.Fprintf(&sb, "  resets in %s", shortDuration(left))
		}
		sb.WriteString("\n")
	}
	if x := snap.ExtraUsage; x != nil && x.IsEnabled {
		sb.WriteString("extra        ")
		if x.Utilization != nil {
			fmt.Fprintf(&sb, "%5.1f%%", *x.Utilization)
		} else {
			sb.WriteString("      ")
		}
		if x.UsedCredits != nil && x.MonthlyLimit != nil {
			fmt.Fprintf(&sb, "  $%.2f of $%.2f", *x.UsedCredits, *x.MonthlyLimit)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// buckets flattens the snapshot into display order, skipping the nil ones.
// The per-model buckets come from the snapshot's own helper so this stays in
// step with the TUI if new models are added there.
func buckets(snap usage.Snapshot) []usage.NamedBucket {
	out := make([]usage.NamedBucket, 0, 4)
	if snap.FiveHour != nil {
		out = append(out, usage.NamedBucket{Label: "5h", Bucket: *snap.FiveHour})
	}
	if snap.SevenDay != nil {
		out = append(out, usage.NamedBucket{Label: "7d", Bucket: *snap.SevenDay})
	}
	out = append(out, snap.PerModelBuckets()...)
	return out
}

func colourFor(util float64, o Options) string {
	switch {
	case util >= o.critAt():
		return colourCrit
	case util >= o.warnAt():
		return colourWarn
	default:
		return colourOK
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
