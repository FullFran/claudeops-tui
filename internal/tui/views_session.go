package tui

import (
	"fmt"
	"strings"
	"time"
)

// renderSessionBrowse draws the session browser: a scrollable list of all sessions
// with cost, events, and a preview card for the selected session.
func renderSessionBrowse(m Model) string {
	var sb strings.Builder
	th := m.Settings.Dashboard.Thresholds

	// Breadcrumb.
	sb.WriteString(breadcrumbStyle.Render("Sessions") + dimStyle.Render(" > ") + breadcrumbActive.Render("Session list") + "\n\n")

	// Preview card for the selected session.
	if m.sessCursor >= 0 && m.sessCursor < len(m.AllSess) {
		sel := m.AllSess[m.sessCursor]
		costStr := colorForSpend(sel.CostEUR, th).Render(fmt.Sprintf("€%.4f", sel.CostEUR))

		var dur string
		if !sel.FirstSeen.IsZero() && !sel.LastSeen.IsZero() {
			dur = sel.LastSeen.Sub(sel.FirstSeen).Truncate(time.Second).String()
		}

		preview := headerStyle.Render(truncate(sel.SessionID, 32)) + "\n"
		preview += fmt.Sprintf("project: %s\n", sel.ProjectName)
		preview += fmt.Sprintf("cost: %s   events: %d", costStr, sel.Events)
		if dur != "" {
			preview += fmt.Sprintf("   duration: %s", dur)
		}
		totalTokens := sel.InTokens + sel.OutTokens + sel.CacheReadTokens + sel.CacheCreateTokens
		if totalTokens > 0 {
			preview += fmt.Sprintf("\ntokens: %d total (in: %d, out: %d, cache_r: %d, cache_c: %d)",
				totalTokens, sel.InTokens, sel.OutTokens, sel.CacheReadTokens, sel.CacheCreateTokens)
		}
		preview += "\n" + dimStyle.Render("press enter for detail")
		sb.WriteString(previewBox.Render(preview) + "\n\n")
	}

	// Column headers.
	header := fmt.Sprintf("  %-18s  %-26s  %8s  %12s", "SESSION", "PROJECT", "EVENTS", "COST")
	sb.WriteString(dimStyle.Render(header) + "\n")
	sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 70)) + "\n")

	// List sessions.
	for i, s := range m.AllSess {
		id := truncate(s.SessionID, 18)
		proj := truncate(s.ProjectName, 26)
		costStr := fmt.Sprintf("€%.4f", s.CostEUR)

		if i == m.sessCursor {
			line := fmt.Sprintf(" > %-18s  %-26s  %8d  %12s", id, proj, s.Events, costStr)
			sb.WriteString(daySelectedStyle.Render(line) + "\n")
		} else if s.CostEUR == 0 {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("   %-18s  %-26s  %8d  %12s",
				id, proj, s.Events, costStr)) + "\n")
		} else {
			costColored := colorForSpend(s.CostEUR, th).Render(costStr)
			sb.WriteString(fmt.Sprintf("   %-18s  %-26s  %8d  %12s\n",
				id, proj, s.Events, costColored))
		}
	}

	sb.WriteString("\n")
	return sb.String()
}

// renderSessionDetail draws the full breakdown for a single session.
func renderSessionDetail(m Model) string {
	if m.sessDetail == nil {
		return dimStyle.Render("loading…")
	}
	var sb strings.Builder
	d := m.sessDetail
	th := m.Settings.Dashboard.Thresholds

	// Breadcrumb.
	shortID := d.SessionID
	if len(shortID) > 16 {
		shortID = shortID[:16]
	}
	sb.WriteString(breadcrumbStyle.Render("Sessions") +
		dimStyle.Render(" > ") + breadcrumbStyle.Render("Session list") +
		dimStyle.Render(" > ") + breadcrumbActive.Render(shortID) + "\n\n")

	// Summary card.
	costStyled := colorForSpend(d.Agg.CostEUR, th).Render(fmt.Sprintf("€%.4f", d.Agg.CostEUR))
	summary := headerStyle.Render(d.SessionID) + "\n"
	summary += fmt.Sprintf("project: %s\n", d.Agg.ProjectName)
	summary += fmt.Sprintf("cost: %s   events: %d", costStyled, d.Agg.Events)

	totalTokens := d.Agg.InTokens + d.Agg.OutTokens + d.Agg.CacheReadTokens + d.Agg.CacheCreateTokens
	summary += fmt.Sprintf("   tokens: %d", totalTokens)

	if !d.Agg.FirstSeen.IsZero() && !d.Agg.LastSeen.IsZero() {
		dur := d.Agg.LastSeen.Sub(d.Agg.FirstSeen).Truncate(time.Second)
		summary += fmt.Sprintf("   duration: %s", dur)

		// Cache hit ratio: cache_read / (cache_read + in_tokens + out_tokens)
		denominator := d.Agg.CacheReadTokens + d.Agg.InTokens + d.Agg.OutTokens
		if denominator > 0 {
			ratio := float64(d.Agg.CacheReadTokens) / float64(denominator) * 100
			summary += fmt.Sprintf("\ncache hit ratio: %.1f%%", ratio)
		}
		summary += fmt.Sprintf("\nfirst seen: %s   last seen: %s",
			d.Agg.FirstSeen.Local().Format("2006-01-02 15:04:05"),
			d.Agg.LastSeen.Local().Format("2006-01-02 15:04:05"))
	}
	sb.WriteString(previewBox.Render(summary) + "\n\n")

	// Hourly activity.
	sb.WriteString("  " + headerStyle.Render("Hourly activity") + "\n")
	sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 50)) + "\n")
	if len(d.Hourly) > 0 {
		maxH := 0.0
		for _, h := range d.Hourly {
			if h.CostEUR > maxH {
				maxH = h.CostEUR
			}
		}
		hourMap := make(map[int]struct {
			cost   float64
			events int64
		})
		for _, h := range d.Hourly {
			hourMap[h.Hour] = struct {
				cost   float64
				events int64
			}{h.CostEUR, h.Events}
		}
		for h := 0; h < 24; h++ {
			data := hourMap[h]
			hourLabel := fmt.Sprintf("  %02d:00", h)
			if data.events == 0 {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("%s  %s", hourLabel, strings.Repeat("·", 20))) + "\n")
			} else {
				bar := hourlyBar(data.cost, maxH, 20)
				costStr := colorForSpend(data.cost, th).Render(fmt.Sprintf("€%.4f", data.cost))
				sb.WriteString(fmt.Sprintf("%s  %s  %s  %d ev\n", hourLabel, bar, costStr, data.events))
			}
		}
	} else {
		sb.WriteString(dimStyle.Render("  no activity") + "\n")
	}
	sb.WriteString("\n")

	// Per-model breakdown.
	sb.WriteString("  " + headerStyle.Render("Models") + "\n")
	sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 50)) + "\n")
	if len(d.Models) > 0 {
		for _, pm := range d.Models {
			costStr := colorForSpend(pm.CostEUR, th).Render(fmt.Sprintf("€%.4f", pm.CostEUR))
			pct := ""
			if d.Agg.CostEUR > 0 {
				pct = dimStyle.Render(fmt.Sprintf("  (%.0f%%)", pm.CostEUR/d.Agg.CostEUR*100))
			}
			sb.WriteString(fmt.Sprintf("  %-32s  %s  %d ev%s\n", truncate(pm.Model, 32), costStr, pm.Events, pct))
		}
	} else {
		sb.WriteString(dimStyle.Render("  no data") + "\n")
	}
	sb.WriteString("\n")

	// Token breakdown.
	sb.WriteString("  " + headerStyle.Render("Token breakdown") + "\n")
	sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 50)) + "\n")
	sb.WriteString(fmt.Sprintf("  %-20s  %d\n", "Input tokens:", d.Agg.InTokens))
	sb.WriteString(fmt.Sprintf("  %-20s  %d\n", "Output tokens:", d.Agg.OutTokens))
	sb.WriteString(fmt.Sprintf("  %-20s  %d\n", "Cache read:", d.Agg.CacheReadTokens))
	sb.WriteString(fmt.Sprintf("  %-20s  %d\n", "Cache create:", d.Agg.CacheCreateTokens))
	sb.WriteString(fmt.Sprintf("  %-20s  %d\n", "Total:", totalTokens))
	sb.WriteString("\n")

	return sb.String()
}
