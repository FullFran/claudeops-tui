package tui

import (
	"fmt"
	"strings"
	"time"
)

// renderSessionsTab — all sessions, sorted by cost desc.
func renderSessionsTab(m Model) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("All sessions") + "  ")
	sb.WriteString(dimStyle.Render(fmt.Sprintf("(%d)", len(m.AllSess))) + "\n\n")
	if len(m.AllSess) == 0 {
		sb.WriteString(dimStyle.Render("  no data") + "\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("  %-10s  %-30s  %12s\n", "SESSION", "PROJECT", "€"))
	sb.WriteString("  " + strings.Repeat("─", 58) + "\n")
	for _, s := range m.AllSess {
		id := truncate(s.SessionID, 10)
		sb.WriteString(fmt.Sprintf("  %-10s  %-30s  %12.4f\n", id, truncate(s.ProjectName, 30), s.CostEUR))
	}
	return sb.String()
}

// renderProjectsTab — all projects with rolling totals.
func renderProjectsTab(m Model) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("All projects") + "  ")
	sb.WriteString(dimStyle.Render(fmt.Sprintf("(%d)", len(m.AllProj))) + "\n\n")
	if len(m.AllProj) == 0 {
		sb.WriteString(dimStyle.Render("  no data") + "\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("  %-40s  %12s\n", "PROJECT", "€ all-time"))
	sb.WriteString("  " + strings.Repeat("─", 58) + "\n")
	for _, p := range m.AllProj {
		sb.WriteString(fmt.Sprintf("  %-40s  %12.4f\n", truncate(p.ProjectName, 40), p.CostEUR))
	}
	return sb.String()
}

// renderModelsTab — per-model breakdown.
func renderModelsTab(m Model) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Per-model usage (all time)") + "\n\n")
	if len(m.PerModel) == 0 {
		sb.WriteString(dimStyle.Render("  no data") + "\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("  %-32s  %8s  %12s  %12s  %14s  %14s  %12s\n",
		"MODEL", "EVENTS", "IN", "OUT", "CACHE_READ", "CACHE_CREATE", "€"))
	sb.WriteString("  " + strings.Repeat("─", 116) + "\n")
	for _, p := range m.PerModel {
		sb.WriteString(fmt.Sprintf("  %-32s  %8d  %12d  %12d  %14d  %14d  %12.4f\n",
			truncate(p.Model, 32), p.Events, p.InTokens, p.OutTokens,
			p.CacheReadTokens, p.CacheCreateTokens, p.CostEUR))
	}
	return sb.String()
}

// renderTasksTab — task history.
func renderTasksTab(m Model) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Tasks") + "  ")
	sb.WriteString(dimStyle.Render(fmt.Sprintf("(%d)", len(m.AllTasks))) + "\n\n")
	if len(m.AllTasks) == 0 {
		sb.WriteString(dimStyle.Render("  no tasks yet — press `n` to start one (or use `claudeops task start \"name\"`)") + "\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("  %-30s  %-19s  %-12s  %8s  %12s\n",
		"NAME", "STARTED", "DURATION", "EVENTS", "€"))
	sb.WriteString("  " + strings.Repeat("─", 95) + "\n")
	for _, t := range m.AllTasks {
		dur := "—"
		end := time.Now()
		if t.EndedAt != nil {
			end = *t.EndedAt
		}
		if !t.StartedAt.IsZero() {
			dur = end.Sub(t.StartedAt).Truncate(time.Second).String()
		}
		sb.WriteString(fmt.Sprintf("  %-30s  %-19s  %-12s  %8d  %12.4f\n",
			truncate(t.Name, 30), t.StartedAt.Format("2006-01-02 15:04:05"), dur, t.Events, t.CostEUR))
	}
	return sb.String()
}
