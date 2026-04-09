package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/fullfran/claudeops-tui/internal/insights"
)

var (
	insightInfoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // green
	insightTipStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))  // cyan
	insightWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))  // yellow
	insightCardStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).MarginBottom(1)
)

// severityPrefix returns an ASCII prefix and a lipgloss style for the given severity.
func severityPrefix(sev insights.Severity) (string, lipgloss.Style) {
	switch sev {
	case insights.Warn:
		return "[WARN]", insightWarnStyle
	case insights.Tip:
		return "[TIP] ", insightTipStyle
	default:
		return "[INFO]", insightInfoStyle
	}
}

// renderInsightsTab renders the Insights tab with filtered insight cards.
func renderInsightsTab(m Model) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Insights") + "\n")
	sb.WriteString(dimStyle.Render("  Actionable observations from your usage patterns") + "\n\n")

	// Filter insights based on settings.
	cfg := m.Settings.Insights
	var filtered []insights.Insight
	for _, ins := range m.Insights {
		switch ins.ID {
		case "cache-efficiency":
			if cfg.ShowCacheEfficiency {
				filtered = append(filtered, ins)
			}
		case "model-mix":
			if cfg.ShowModelMix {
				filtered = append(filtered, ins)
			}
		case "cost-trend":
			if cfg.ShowCostTrend {
				filtered = append(filtered, ins)
			}
		case "session-efficiency":
			if cfg.ShowSessionEfficiency {
				filtered = append(filtered, ins)
			}
		case "peak-hours":
			if cfg.ShowPeakHours {
				filtered = append(filtered, ins)
			}
		default:
			filtered = append(filtered, ins)
		}
	}

	if len(filtered) == 0 {
		sb.WriteString(dimStyle.Render("  No insights yet — keep using Claude Code to generate data.") + "\n")
		return sb.String()
	}

	for _, ins := range filtered {
		prefix, style := severityPrefix(ins.Severity)
		header := style.Render(prefix) + "  " + style.Bold(true).Render(ins.Title)
		sb.WriteString("  " + header + "\n")
		if ins.Detail != "" {
			sb.WriteString("  " + dimStyle.Render(ins.Detail) + "\n")
		}
		if ins.Recommendation != "" {
			sb.WriteString("  " + ins.Recommendation + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
