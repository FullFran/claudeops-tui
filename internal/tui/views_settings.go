package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/fullfran/claudeops-tui/internal/config"
)

// settingsItem represents a single row in the Settings tab.
// If section is true, it renders as a non-selectable section header.
type settingsItem struct {
	section bool
	label   string
	desc    string
	get     func(config.Settings) bool
	toggle  func(*config.Settings)
}

// settingsItems returns the flat list of entries for the Settings tab.
// Section headers have section=true and nil get/toggle.
func settingsItems() []settingsItem {
	return []settingsItem{
		{section: true, label: "Dashboard Widgets"},
		{label: "Subscription usage", desc: "API subscription % bars",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowSubscription },
			toggle: func(s *config.Settings) { s.Dashboard.ShowSubscription = !s.Dashboard.ShowSubscription }},
		{label: "Today summary", desc: "events, cost, tokens",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowToday },
			toggle: func(s *config.Settings) { s.Dashboard.ShowToday = !s.Dashboard.ShowToday }},
		{label: "Sparkline (14d)", desc: "daily cost bar chart",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowSparkline14d },
			toggle: func(s *config.Settings) { s.Dashboard.ShowSparkline14d = !s.Dashboard.ShowSparkline14d }},
		{label: "Burn rate", desc: "cost/hour from last 4h",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowBurnRate },
			toggle: func(s *config.Settings) { s.Dashboard.ShowBurnRate = !s.Dashboard.ShowBurnRate }},
		{label: "Streak", desc: "consecutive active days",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowStreak },
			toggle: func(s *config.Settings) { s.Dashboard.ShowStreak = !s.Dashboard.ShowStreak }},
		{label: "Max day (30d)", desc: "most expensive day",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowMaxDay30d },
			toggle: func(s *config.Settings) { s.Dashboard.ShowMaxDay30d = !s.Dashboard.ShowMaxDay30d }},
		{label: "Avg cost/session", desc: "today's average",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowAvgPerSession },
			toggle: func(s *config.Settings) { s.Dashboard.ShowAvgPerSession = !s.Dashboard.ShowAvgPerSession }},
		{label: "Cache hit ratio", desc: "inline on Today card",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowCacheHitRatio },
			toggle: func(s *config.Settings) { s.Dashboard.ShowCacheHitRatio = !s.Dashboard.ShowCacheHitRatio }},
		{label: "Tokens per euro", desc: "inline on Today card",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowTokensPerEuro },
			toggle: func(s *config.Settings) { s.Dashboard.ShowTokensPerEuro = !s.Dashboard.ShowTokensPerEuro }},
		{label: "vs 7d average", desc: "daily spend delta",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowVsAvg7d },
			toggle: func(s *config.Settings) { s.Dashboard.ShowVsAvg7d = !s.Dashboard.ShowVsAvg7d }},
		{label: "Per-model today", desc: "cost breakdown by model",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowPerModelToday },
			toggle: func(s *config.Settings) { s.Dashboard.ShowPerModelToday = !s.Dashboard.ShowPerModelToday }},
		{label: "Top sessions (7d)", desc: "highest-cost sessions",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowTopSessions },
			toggle: func(s *config.Settings) { s.Dashboard.ShowTopSessions = !s.Dashboard.ShowTopSessions }},
		{label: "Top projects (7d)", desc: "highest-cost projects",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowTopProjects },
			toggle: func(s *config.Settings) { s.Dashboard.ShowTopProjects = !s.Dashboard.ShowTopProjects }},
		{label: "Active task", desc: "current task name + elapsed",
			get:    func(s config.Settings) bool { return s.Dashboard.ShowActiveTask },
			toggle: func(s *config.Settings) { s.Dashboard.ShowActiveTask = !s.Dashboard.ShowActiveTask }},

		{section: true, label: "Thresholds"},
		// Thresholds are display-only in the TUI; edit via config.toml.

		{section: true, label: "Visible Tabs"},
		{label: "Sessions", desc: "all sessions by cost",
			get:    func(s config.Settings) bool { return s.Tabs.Sessions },
			toggle: func(s *config.Settings) { s.Tabs.Sessions = !s.Tabs.Sessions }},
		{label: "Projects", desc: "all projects by cost",
			get:    func(s config.Settings) bool { return s.Tabs.Projects },
			toggle: func(s *config.Settings) { s.Tabs.Projects = !s.Tabs.Projects }},
		{label: "Models", desc: "per-model token breakdown",
			get:    func(s config.Settings) bool { return s.Tabs.Models },
			toggle: func(s *config.Settings) { s.Tabs.Models = !s.Tabs.Models }},
		{label: "Tasks", desc: "task history with costs",
			get:    func(s config.Settings) bool { return s.Tabs.Tasks },
			toggle: func(s *config.Settings) { s.Tabs.Tasks = !s.Tabs.Tasks }},
		{label: "Insights", desc: "actionable usage observations",
			get:    func(s config.Settings) bool { return s.Tabs.Insights },
			toggle: func(s *config.Settings) { s.Tabs.Insights = !s.Tabs.Insights }},

		{section: true, label: "Insights Cards"},
		{label: "Cache efficiency", desc: "cache reuse ratio insight",
			get:    func(s config.Settings) bool { return s.Insights.ShowCacheEfficiency },
			toggle: func(s *config.Settings) { s.Insights.ShowCacheEfficiency = !s.Insights.ShowCacheEfficiency }},
		{label: "Model mix", desc: "spend concentration by model",
			get:    func(s config.Settings) bool { return s.Insights.ShowModelMix },
			toggle: func(s *config.Settings) { s.Insights.ShowModelMix = !s.Insights.ShowModelMix }},
		{label: "Cost trend", desc: "week-over-week cost delta",
			get:    func(s config.Settings) bool { return s.Insights.ShowCostTrend },
			toggle: func(s *config.Settings) { s.Insights.ShowCostTrend = !s.Insights.ShowCostTrend }},
		{label: "Session efficiency", desc: "short vs long session cost/token",
			get:    func(s config.Settings) bool { return s.Insights.ShowSessionEfficiency },
			toggle: func(s *config.Settings) { s.Insights.ShowSessionEfficiency = !s.Insights.ShowSessionEfficiency }},
		{label: "Peak hours", desc: "top 3 hours by spend",
			get:    func(s config.Settings) bool { return s.Insights.ShowPeakHours },
			toggle: func(s *config.Settings) { s.Insights.ShowPeakHours = !s.Insights.ShowPeakHours }},

		{section: true, label: "MCP Server"},
		{label: "Claude Code", desc: "expose usage data via MCP",
			get: func(_ config.Settings) bool {
				return mcpConfigExists("claudeops")
			},
			toggle: func(_ *config.Settings) {
				toggleMCPConfig("claudeops", []byte(`{
  "command": "claudeops",
  "args": ["mcp"]
}
`))
			}},
	}
}

// mcpConfigPath returns ~/.claude/mcp/<name>.json.
func mcpConfigPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "mcp", name+".json")
}

// mcpConfigExists checks whether the MCP server config file exists.
func mcpConfigExists(name string) bool {
	_, err := os.Stat(mcpConfigPath(name))
	return err == nil
}

// toggleMCPConfig creates or removes the MCP server config file.
func toggleMCPConfig(name string, content []byte) {
	path := mcpConfigPath(name)
	if mcpConfigExists(name) {
		os.Remove(path)
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, content, 0o600)
}

var (
	settingsSectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginTop(1)
	settingsCursorStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	settingsOnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	settingsOffStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	settingsDescStyle    = lipgloss.NewStyle().Faint(true)
)

// renderSettingsTab draws the interactive settings list with cursor highlight.
func renderSettingsTab(m Model) string {
	var sb strings.Builder
	items := settingsItems()

	sb.WriteString(headerStyle.Render("Settings") + "\n")
	sb.WriteString(dimStyle.Render("  j/k navigate   space/enter toggle   changes auto-saved") + "\n\n")

	for i, item := range items {
		if item.section {
			sb.WriteString("\n")
			sb.WriteString("  " + settingsSectionStyle.Render(item.label) + "\n")
			sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 52)) + "\n")

			// Show threshold values inline for the Thresholds section.
			if item.label == "Thresholds" {
				th := m.Settings.Dashboard.Thresholds
				sb.WriteString(fmt.Sprintf("  warning   %s    alert   %s\n",
					warnStyle.Render(fmt.Sprintf("€%.0f", th.DailyWarnEUR)),
					errStyle.Render(fmt.Sprintf("€%.0f", th.DailyAlertEUR))))
				sb.WriteString("  " + settingsDescStyle.Render("edit thresholds in ~/.claudeops/config.toml") + "\n")
			}
			continue
		}

		on := item.get(m.Settings)
		toggle := settingsOffStyle.Render("[ ]")
		if on {
			toggle = settingsOnStyle.Render("[*]")
		}

		label := item.label
		desc := settingsDescStyle.Render(item.desc)

		if i == m.settingsCursor {
			// Highlighted row.
			indicator := ">"
			line := fmt.Sprintf(" %s %s  %-24s  %s", indicator, toggle, label, desc)
			sb.WriteString(cursorLineMarker + settingsCursorStyle.Render(line) + "\n")
		} else {
			sb.WriteString(fmt.Sprintf("   %s  %-24s  %s\n", toggle, label, desc))
		}
	}

	sb.WriteString("\n")
	return sb.String()
}
