package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// renderDashboardTab renders the overview screen. Each widget is gated by a
// flag in m.Settings.Dashboard so users can hide anything they don't want.
func renderDashboardTab(m Model) string {
	var sb strings.Builder
	d := m.Settings.Dashboard

	if d.ShowSubscription {
		sb.WriteString(renderSubscriptionSection(m))
	}

	if d.ShowToday {
		sb.WriteString(headerStyle.Render("Today") + "\n")
		if m.Today.Events == 0 {
			sb.WriteString(dimStyle.Render("  —") + "\n")
		} else {
			costStyled := colorForSpend(m.Today.CostEUR, d.Thresholds).Render(fmt.Sprintf("€%.4f", m.Today.CostEUR))
			fmt.Fprintf(&sb, "  events: %d   %s\n", m.Today.Events, costStyled)
			fmt.Fprintf(&sb, "  in: %d   out: %d   cache_read: %d   cache_create: %d\n",
				m.Today.InTokens, m.Today.OutTokens, m.Today.CacheReadTokens, m.Today.CacheCreateTokens)
			// Inline derived stats: cache hit ratio and tokens-per-€.
			extras := []string{}
			if d.ShowCacheHitRatio {
				if r, ok := cacheHitRatio(m.Today); ok {
					extras = append(extras, fmt.Sprintf("cache hit: %.0f%%", r*100))
				}
			}
			if d.ShowTokensPerEuro && m.Today.CostEUR > 0 {
				tot := float64(m.Today.InTokens + m.Today.OutTokens + m.Today.CacheReadTokens)
				extras = append(extras, fmt.Sprintf("%.0f tok/€", tot/m.Today.CostEUR))
			}
			if d.ShowVsAvg7d {
				if delta, ok := todayVsAvg7d(m.Today.CostEUR, m.Last7d.CostEUR); ok {
					extras = append(extras, fmt.Sprintf("vs 7d avg: %s", formatDelta(delta)))
				}
			}
			if len(extras) > 0 {
				sb.WriteString("  " + dimStyle.Render(strings.Join(extras, "   ")) + "\n")
			}
		}
		sb.WriteString("\n")
	}

	if d.ShowSparkline14d && len(m.Daily) > 0 {
		sb.WriteString(headerStyle.Render("Last 14 days (€/day)") + "\n")
		series := lastN(m.Daily, 14)
		sb.WriteString("  " + sparkline(series, d.Thresholds) + "\n")
		first, last := series[0].Date.Format("01-02"), series[len(series)-1].Date.Format("01-02")
		total := 0.0
		for _, x := range series {
			total += x.CostEUR
		}
		sb.WriteString("  " + dimStyle.Render(fmt.Sprintf("%s … %s   total: €%.2f   avg: €%.2f/d   ",
			first, last, total, total/float64(len(series)))) +
			dimStyle.Render("enter: browse days") + "\n\n")
	}

	if d.ShowBurnRate && m.BurnRate4h > 0 {
		sb.WriteString(headerStyle.Render("Burn rate") + "\n")
		fmt.Fprintf(&sb, "  %s   %s\n",
			colorForSpend(m.BurnRate4h*24, d.Thresholds).Render(fmt.Sprintf("€%.3f/h", m.BurnRate4h)),
			dimStyle.Render(fmt.Sprintf("(last 4h · projected: €%.2f/day)", m.BurnRate4h*24)))
		sb.WriteString("\n")
	}

	if d.ShowStreak && len(m.Daily) > 0 {
		streak := currentStreak(m.Daily)
		if streak > 0 {
			sb.WriteString(headerStyle.Render("Streak") + "\n")
			fmt.Fprintf(&sb, "  %d days with activity\n\n", streak)
		}
	}

	if d.ShowMaxDay30d && len(m.Daily) > 0 {
		if maxD, ok := maxDay(m.Daily); ok && maxD.CostEUR > 0 {
			sb.WriteString(headerStyle.Render("Most expensive day (30d)") + "\n")
			fmt.Fprintf(&sb, "  %s   %s   %d events\n\n",
				maxD.Date.Format("2006-01-02 Mon"),
				colorForSpend(maxD.CostEUR, d.Thresholds).Render(fmt.Sprintf("€%.2f", maxD.CostEUR)),
				maxD.Events)
		}
	}

	if d.ShowAvgPerSession && len(m.Daily) > 0 {
		// Use today first, then fall back to a 7-day rolling avg.
		todayRow := m.Daily[len(m.Daily)-1]
		if todayRow.Sessions > 0 {
			avg := todayRow.CostEUR / float64(todayRow.Sessions)
			sb.WriteString(headerStyle.Render("Avg cost per session (today)") + "\n")
			fmt.Fprintf(&sb, "  €%.4f   over %d sessions\n\n", avg, todayRow.Sessions)
		}
	}

	if d.ShowPerModelToday && len(m.PerModelToday) > 0 {
		sb.WriteString(headerStyle.Render("Per-model today") + "\n")
		for _, p := range m.PerModelToday {
			fmt.Fprintf(&sb, "  %-30s  €%.4f\n", truncate(p.Model, 30), p.CostEUR)
		}
		sb.WriteString("\n")
	}

	if d.ShowTopSessions {
		sb.WriteString(headerStyle.Render("Top sessions (7d)") + "\n")
		if len(m.TopSess) == 0 {
			sb.WriteString(dimStyle.Render("  —") + "\n")
		} else {
			for _, s := range m.TopSess {
				id := truncate(s.SessionID, 8)
				fmt.Fprintf(&sb, "  %s  %-22s  €%.4f\n", id, truncate(s.ProjectName, 22), s.CostEUR)
			}
		}
		sb.WriteString("\n")
	}

	if d.ShowTopProjects {
		sb.WriteString(headerStyle.Render("Top projects (7d)") + "\n")
		if len(m.TopProj) == 0 {
			sb.WriteString(dimStyle.Render("  —") + "\n")
		} else {
			for _, p := range m.TopProj {
				fmt.Fprintf(&sb, "  %-30s  €%.4f\n", truncate(p.ProjectName, 30), p.CostEUR)
			}
		}
		sb.WriteString("\n")
	}

	if d.ShowActiveTask {
		sb.WriteString(headerStyle.Render("Active task") + "\n")
		if m.ActiveTask == nil {
			sb.WriteString(dimStyle.Render("  no active task") + "\n")
		} else {
			elapsed := time.Since(m.ActiveTask.StartedAt).Truncate(time.Second)
			fmt.Fprintf(&sb, "  %s   elapsed: %s\n", m.ActiveTask.Name, elapsed)
		}
	}

	if len(m.SourceAggs) > 0 {
		sb.WriteString("\n")
		sb.WriteString(headerStyle.Render("By source") + "\n")
		for _, ag := range m.SourceAggs {
			costStyled := colorForSpend(ag.CostEUR, d.Thresholds).Render(fmt.Sprintf("€%.4f", ag.CostEUR))
			fmt.Fprintf(&sb, "  %-12s  events: %d   %s\n",
				ag.Source, ag.Events, costStyled)
		}
	}

	return sb.String()
}

// subscriptionNames lists the detected subscriptions in display order: the
// bespoke Claude entry first (when configured), then each extra provider.
// It backs both the selector chips and the `p` focus-cycling key.
func subscriptionNames(m Model) []string {
	names := []string{}
	if m.Snap != nil || m.UsageErr != "" {
		names = append(names, "Claude")
	}
	for _, r := range m.ProviderUsages {
		names = append(names, r.Name)
	}
	return names
}

// renderSubscriptionSection renders the "Subscription usage" block. When more
// than one subscription is present it shows a chip selector; `p` cycles the
// focus (All → each provider → All) so a long stack collapses to one at a time.
func renderSubscriptionSection(m Model) string {
	var sb strings.Builder
	names := subscriptionNames(m)

	header := headerStyle.Render("Subscription usage")
	if len(names) > 1 {
		header += "   " + renderSubChips(names, m.subFocus)
	}
	sb.WriteString(header + "\n")

	if len(names) == 0 {
		sb.WriteString(dimStyle.Render("  —") + "\n\n")
		return sb.String()
	}

	focus := m.subFocus
	if focus < 0 || focus > len(names) {
		focus = 0
	}
	shown := func(i int) bool { return focus == 0 || focus == i+1 }

	first := true
	emit := func(block string) {
		if !first {
			sb.WriteString("\n")
		}
		sb.WriteString(block)
		first = false
	}

	idx := 0
	if m.Snap != nil || m.UsageErr != "" {
		if shown(idx) {
			emit(renderClaudeBlock(m))
		}
		idx++
	}
	for _, r := range m.ProviderUsages {
		if shown(idx) {
			emit(renderProviderResult(r))
		}
		idx++
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderSubChips renders the "All Claude Codex …" selector row, highlighting
// the focused entry (0 = All).
func renderSubChips(names []string, focus int) string {
	chip := func(text string, active bool) string {
		if active {
			return tabActive.Render(text)
		}
		return tabInactive.Render(text)
	}
	parts := []string{chip("All", focus == 0)}
	for i, n := range names {
		parts = append(parts, chip(n, focus == i+1))
	}
	return strings.Join(parts, " ")
}

// renderClaudeBlock renders the bespoke Anthropic subscription entry: the OAuth
// quota buckets plus the local-store weekly-cycle correlation.
func renderClaudeBlock(m Model) string {
	var sb strings.Builder
	d := m.Settings.Dashboard
	sb.WriteString(dimStyle.Render("  Claude") + "\n")

	if m.UsageErr != "" {
		sb.WriteString(warnStyle.Render("    "+m.UsageErr) + "\n")
		return sb.String()
	}
	if m.Snap == nil {
		sb.WriteString(dimStyle.Render("    —") + "\n")
		return sb.String()
	}
	if m.Snap.FiveHour != nil {
		sb.WriteString(renderBucket("  "+padRight("5h", 11), m.Snap.FiveHour.Utilization, m.Snap.FiveHour.ResetsAt))
	}
	if m.Snap.SevenDay != nil {
		sb.WriteString(renderBucket("  "+padRight("7d", 11), m.Snap.SevenDay.Utilization, m.Snap.SevenDay.ResetsAt))
	}
	for _, nb := range m.Snap.PerModelBuckets() {
		sb.WriteString(renderBucket("  "+padRight(nb.Label, 11), nb.Bucket.Utilization, nb.Bucket.ResetsAt))
	}
	sb.WriteString(dimStyle.Render("  This device · current weekly cycle") + "\n")
	if m.HasWeeklyCycleWindow {
		costStyled := colorForSpend(m.WeeklyCycleLocal.CostEUR, d.Thresholds).Render(fmt.Sprintf("€%.4f", m.WeeklyCycleLocal.CostEUR))
		fmt.Fprintf(&sb, "  events: %d   %s\n", m.WeeklyCycleLocal.Events, costStyled)
		sb.WriteString("  " + dimStyle.Render(fmt.Sprintf("window: %s -> %s",
			m.WeeklyCycleStart.Local().Format("2006-01-02 15:04"),
			m.WeeklyCycleEnd.Local().Format("2006-01-02 15:04"))) + "\n")
	} else {
		sb.WriteString(dimStyle.Render("  unavailable (weekly cycle window not present in usage snapshot)") + "\n")
	}
	return sb.String()
}

// renderProviderResult renders one extra provider's entry (name, meters, note),
// showing a per-provider error inline instead of hiding it.
func renderProviderResult(r provider.Result) string {
	var sb strings.Builder
	sb.WriteString(dimStyle.Render("  "+r.Name) + "\n")
	if r.Err != nil {
		sb.WriteString(warnStyle.Render("    "+r.Err.Error()) + "\n")
		return sb.String()
	}
	if len(r.Usage.Windows) == 0 {
		sb.WriteString(dimStyle.Render("    —") + "\n")
		return sb.String()
	}
	for _, w := range r.Usage.Windows {
		sb.WriteString(renderBucket("  "+padRight(w.Label, 11), w.Utilization, w.ResetsAt))
	}
	if r.Usage.Note != "" {
		sb.WriteString(dimStyle.Render("    "+r.Usage.Note) + "\n")
	}
	return sb.String()
}

// renderProviders renders every extra provider stacked (used by tests and as a
// helper); the dashboard itself goes through renderSubscriptionSection.
func renderProviders(results []provider.Result) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, r := range results {
		sb.WriteString("\n")
		sb.WriteString(renderProviderResult(r))
	}
	return sb.String()
}

func renderBucket(label string, util float64, resets time.Time) string {
	meter := bar(util, 24)
	pct := levelStyle(util).Render(fmt.Sprintf("%5.1f%%", util))
	when := ""
	if !resets.IsZero() {
		when = dimStyle.Render("  resets in " + humanDur(time.Until(resets)))
	}
	return fmt.Sprintf("%s %s %s%s\n", label, meter, pct, when)
}

// humanDur renders a duration compactly ("5d 2h", "3h 20m", "45m") instead of
// Go's default "119h59m0s", which is unreadable for multi-day quota windows.
func humanDur(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// levelStyle colors a utilization value: healthy < 60%, caution < 85%, else
// danger. Gives an at-a-glance read of how close a quota is to its limit.
func levelStyle(pct float64) lipgloss.Style {
	switch {
	case pct >= 85:
		return lipgloss.NewStyle().Foreground(colErr)
	case pct >= 60:
		return lipgloss.NewStyle().Foreground(colWarn)
	default:
		return lipgloss.NewStyle().Foreground(colOk)
	}
}

// bar renders a colored progress meter: the filled portion is tinted by level
// and the empty track is de-emphasized, so the eye lands on the fill.
func bar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(width))
	fill := levelStyle(pct).Render(strings.Repeat("█", filled))
	track := dimStyle.Render(strings.Repeat("░", width-filled))
	return fill + track
}

// --- helpers for the new stats ---------------------------------------------

// sparkChars are the standard 8-level Unicode block elements.
var sparkChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// sparkline renders a series of daily costs as Unicode block characters.
// Each cell is colored by the threshold it falls into.
func sparkline(days []store.DailyAgg, th config.ThresholdsSettings) string {
	if len(days) == 0 {
		return ""
	}
	max := 0.0
	for _, d := range days {
		if d.CostEUR > max {
			max = d.CostEUR
		}
	}
	if max == 0 {
		return strings.Repeat("·", len(days))
	}
	var sb strings.Builder
	for _, d := range days {
		if d.CostEUR == 0 {
			sb.WriteString(dimStyle.Render("·"))
			continue
		}
		idx := int((d.CostEUR / max) * float64(len(sparkChars)-1))
		if idx >= len(sparkChars) {
			idx = len(sparkChars) - 1
		}
		cell := string(sparkChars[idx])
		sb.WriteString(colorForSpend(d.CostEUR, th).Render(cell))
	}
	return sb.String()
}

// colorForSpend picks a lipgloss style based on cost vs thresholds.
// Below warn → green, between warn and alert → yellow, above alert → red.
func colorForSpend(cost float64, th config.ThresholdsSettings) lipgloss.Style {
	switch {
	case cost >= th.DailyAlertEUR:
		return lipgloss.NewStyle().Foreground(colErr)
	case cost >= th.DailyWarnEUR:
		return lipgloss.NewStyle().Foreground(colWarn)
	default:
		return lipgloss.NewStyle().Foreground(colOk)
	}
}

// cacheHitRatio = cache_read / (cache_read + in). Returns false if both
// numerators are zero (avoids 0/0 and a meaningless "0%" display).
func cacheHitRatio(a store.Aggregates) (float64, bool) {
	denom := float64(a.CacheReadTokens + a.InTokens)
	if denom == 0 {
		return 0, false
	}
	return float64(a.CacheReadTokens) / denom, true
}

// todayVsAvg7d returns the percentage delta between today's cost and the
// 7-day average. Returns false when there's no baseline.
func todayVsAvg7d(today, last7dTotal float64) (float64, bool) {
	if last7dTotal <= 0 {
		return 0, false
	}
	avg := last7dTotal / 7
	if avg == 0 {
		return 0, false
	}
	return (today - avg) / avg * 100, true
}

func formatDelta(pct float64) string {
	sign := "+"
	if pct < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.0f%%", sign, pct)
}

// currentStreak counts the trailing days with non-zero events. Today doesn't
// have to have activity yet — we look at the longest run ending at today OR
// yesterday so an early-morning glance at the dashboard still shows the streak.
func currentStreak(days []store.DailyAgg) int {
	if len(days) == 0 {
		return 0
	}
	streak := 0
	// Walk newest → oldest. Allow today to be empty (grace period).
	start := len(days) - 1
	if days[start].Events == 0 && start > 0 {
		start--
	}
	for i := start; i >= 0; i-- {
		if days[i].Events == 0 {
			break
		}
		streak++
	}
	return streak
}

// maxDay returns the day with the highest cost in the series.
func maxDay(days []store.DailyAgg) (store.DailyAgg, bool) {
	if len(days) == 0 {
		return store.DailyAgg{}, false
	}
	best := days[0]
	for _, d := range days[1:] {
		if d.CostEUR > best.CostEUR {
			best = d
		}
	}
	return best, true
}

// lastN returns the trailing N elements of a slice (or all if shorter).
func lastN(days []store.DailyAgg, n int) []store.DailyAgg {
	if len(days) <= n {
		return days
	}
	return days[len(days)-n:]
}
