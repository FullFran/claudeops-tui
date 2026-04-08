package tui

import (
	"fmt"
	"strings"
	"time"
)

// renderDashboardTab renders the overview screen.
func renderDashboardTab(m Model) string {
	var sb strings.Builder

	sb.WriteString(headerStyle.Render("Subscription usage") + "\n")
	if m.UsageErr != "" {
		sb.WriteString(warnStyle.Render("  "+m.UsageErr) + "\n")
	} else if m.Snap != nil {
		if m.Snap.FiveHour != nil {
			sb.WriteString(renderBucket("  5h         ", m.Snap.FiveHour.Utilization, m.Snap.FiveHour.ResetsAt))
		}
		if m.Snap.SevenDay != nil {
			sb.WriteString(renderBucket("  7d         ", m.Snap.SevenDay.Utilization, m.Snap.SevenDay.ResetsAt))
		}
		for _, nb := range m.Snap.PerModelBuckets() {
			label := "  " + padRight(nb.Label, 11)
			sb.WriteString(renderBucket(label, nb.Bucket.Utilization, nb.Bucket.ResetsAt))
		}
	} else {
		sb.WriteString(dimStyle.Render("  —") + "\n")
	}
	sb.WriteString("\n")

	sb.WriteString(headerStyle.Render("Today") + "\n")
	if m.Today.Events == 0 {
		sb.WriteString(dimStyle.Render("  —") + "\n")
	} else {
		sb.WriteString(fmt.Sprintf("  events: %d   €: %.4f\n", m.Today.Events, m.Today.CostEUR))
		sb.WriteString(fmt.Sprintf("  in: %d   out: %d   cache_read: %d   cache_create: %d\n",
			m.Today.InTokens, m.Today.OutTokens, m.Today.CacheReadTokens, m.Today.CacheCreateTokens))
	}
	sb.WriteString("\n")

	sb.WriteString(headerStyle.Render("Top sessions (7d)") + "\n")
	if len(m.TopSess) == 0 {
		sb.WriteString(dimStyle.Render("  —") + "\n")
	} else {
		for _, s := range m.TopSess {
			id := truncate(s.SessionID, 8)
			sb.WriteString(fmt.Sprintf("  %s  %-22s  €%.4f\n", id, truncate(s.ProjectName, 22), s.CostEUR))
		}
	}
	sb.WriteString("\n")

	sb.WriteString(headerStyle.Render("Top projects (7d)") + "\n")
	if len(m.TopProj) == 0 {
		sb.WriteString(dimStyle.Render("  —") + "\n")
	} else {
		for _, p := range m.TopProj {
			sb.WriteString(fmt.Sprintf("  %-30s  €%.4f\n", truncate(p.ProjectName, 30), p.CostEUR))
		}
	}
	sb.WriteString("\n")

	sb.WriteString(headerStyle.Render("Active task") + "\n")
	if m.ActiveTask == nil {
		sb.WriteString(dimStyle.Render("  no active task") + "\n")
	} else {
		elapsed := time.Since(m.ActiveTask.StartedAt).Truncate(time.Second)
		sb.WriteString(fmt.Sprintf("  %s   elapsed: %s\n", m.ActiveTask.Name, elapsed))
	}

	return sb.String()
}

func renderBucket(label string, util float64, resets time.Time) string {
	bar := bar(util, 24)
	when := ""
	if !resets.IsZero() {
		d := time.Until(resets).Truncate(time.Minute)
		when = fmt.Sprintf("  resets in %s", d)
	}
	return fmt.Sprintf("%s %s %5.1f%%%s\n", label, bar, util, when)
}

func bar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(width))
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}
