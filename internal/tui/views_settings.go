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
//
//   - section:   non-selectable header row
//   - skip:      non-selectable display row (e.g. readonly value)
//   - toggle:    bool field — space/enter flips it
//   - isString:  editable string — enter opens the text-input modal
//   - actionKey: action identifier dispatched by Update (push_now, apply_otel)
//   - readValue: function that returns a display string for a readonly row
type settingsItem struct {
	section bool
	skip    bool
	label   string
	desc    string

	// bool toggle
	get    func(config.Settings) bool
	toggle func(*config.Settings)

	// editable string
	isString  bool
	getString func(config.Settings) string
	setString func(*config.Settings, string)

	// action (dispatched by Update)
	actionKey string

	// readonly display (skip=true, renders value but not selectable)
	readValue func(config.Settings) string
}

// settingsItems returns the flat ordered list of entries for the Settings tab.
func settingsItems() []settingsItem {
	return []settingsItem{
		// ── Dashboard Widgets ────────────────────────────────────────────────
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

		// ── Thresholds ───────────────────────────────────────────────────────
		{section: true, label: "Thresholds"},
		// Thresholds are display-only; edit via config.toml.

		// ── Visible Tabs ─────────────────────────────────────────────────────
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

		// ── Insights Cards ───────────────────────────────────────────────────
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

		// ── MCP Server ───────────────────────────────────────────────────────
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

		// ── Export Metrics ───────────────────────────────────────────────────
		{section: true, label: "Export Metrics"},
		{label: "Enabled", desc: "push metrics to OTLP endpoint",
			get:    func(s config.Settings) bool { return s.Export.Enabled },
			toggle: func(s *config.Settings) { s.Export.Enabled = !s.Export.Enabled }},
		{label: "User name", desc: "your display name in dashboards",
			isString:  true,
			getString: func(s config.Settings) string { return s.Export.UserName },
			setString: func(s *config.Settings, v string) { s.Export.UserName = v }},
		{label: "Team name", desc: "team label for grouping",
			isString:  true,
			getString: func(s config.Settings) string { return s.Export.TeamName },
			setString: func(s *config.Settings, v string) { s.Export.TeamName = v }},
		{label: "Endpoint", desc: "OTLP HTTP endpoint URL",
			isString:  true,
			getString: func(s config.Settings) string { return s.Export.Endpoint },
			setString: func(s *config.Settings, v string) { s.Export.Endpoint = v }},
		{skip: true, label: "Headers", desc: "edit in ~/.claudeops/config.toml",
			readValue: func(s config.Settings) string {
				if len(s.Export.Headers) == 0 {
					return "none"
				}
				return fmt.Sprintf("%d set", len(s.Export.Headers))
			}},

		// ── Claude Code OTel ─────────────────────────────────────────────────
		{section: true, label: "Claude Code OTel"},
		{label: "Enabled", desc: "forward native Claude Code OTel",
			get:    func(s config.Settings) bool { return s.Export.ClaudeOTel.Enabled },
			toggle: func(s *config.Settings) { s.Export.ClaudeOTel.Enabled = !s.Export.ClaudeOTel.Enabled }},
		{label: "Include prompts", desc: "log user prompt content",
			get:    func(s config.Settings) bool { return s.Export.ClaudeOTel.IncludeUserPrompts },
			toggle: func(s *config.Settings) { s.Export.ClaudeOTel.IncludeUserPrompts = !s.Export.ClaudeOTel.IncludeUserPrompts }},
		{label: "Include tool details", desc: "log Bash commands, file paths",
			get:    func(s config.Settings) bool { return s.Export.ClaudeOTel.IncludeToolDetails },
			toggle: func(s *config.Settings) { s.Export.ClaudeOTel.IncludeToolDetails = !s.Export.ClaudeOTel.IncludeToolDetails }},
		{label: "Push now", desc: "send metrics to endpoint immediately",
			actionKey: "push_now"},
		{label: "Apply OTel config", desc: "write env vars to ~/.claude/settings.json",
			actionKey: "apply_otel"},
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func mcpConfigPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "mcp", name+".json")
}

func mcpConfigExists(name string) bool {
	_, err := os.Stat(mcpConfigPath(name))
	return err == nil
}

func toggleMCPConfig(name string, content []byte) {
	path := mcpConfigPath(name)
	if mcpConfigExists(name) {
		os.Remove(path)
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, content, 0o600)
}

// ── Styles ───────────────────────────────────────────────────────────────────

var (
	settingsSectionStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginTop(1)
	settingsCursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	settingsOnStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	settingsOffStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	settingsDescStyle     = lipgloss.NewStyle().Faint(true)
	settingsStringStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // cyan
	settingsActionStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	settingsReadonlyStyle = lipgloss.NewStyle().Faint(true)
)

// renderSettingsTab draws the interactive settings list with cursor highlight.
func renderSettingsTab(m Model) string {
	var sb strings.Builder
	items := settingsItems()

	sb.WriteString(headerStyle.Render("Settings") + "\n")
	sb.WriteString(dimStyle.Render("  j/k navigate   space/enter toggle · edit · run   auto-saved") + "\n\n")

	for i, item := range items {
		// ── Section header ────────────────────────────────────────────────
		if item.section {
			sb.WriteString("\n")
			sb.WriteString("  " + settingsSectionStyle.Render(item.label) + "\n")
			sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 52)) + "\n")

			if item.label == "Thresholds" {
				th := m.Settings.Dashboard.Thresholds
				sb.WriteString(fmt.Sprintf("  warning   %s    alert   %s\n",
					warnStyle.Render(fmt.Sprintf("€%.0f", th.DailyWarnEUR)),
					errStyle.Render(fmt.Sprintf("€%.0f", th.DailyAlertEUR))))
				sb.WriteString("  " + settingsDescStyle.Render("edit thresholds in ~/.claudeops/config.toml") + "\n")
			}
			continue
		}

		// ── Skip (readonly display, non-selectable) ───────────────────────
		if item.skip && item.readValue != nil {
			val := item.readValue(m.Settings)
			sb.WriteString(fmt.Sprintf("   %-20s %-14s  %s\n",
				item.label,
				settingsReadonlyStyle.Render(val),
				settingsDescStyle.Render(item.desc)))
			continue
		}

		selected := i == m.settingsCursor

		switch {
		// ── Bool toggle ───────────────────────────────────────────────────
		case item.toggle != nil:
			on := item.get(m.Settings)
			toggle := settingsOffStyle.Render("[ ]")
			if on {
				toggle = settingsOnStyle.Render("[*]")
			}
			desc := settingsDescStyle.Render(item.desc)
			if selected {
				line := fmt.Sprintf(" > %s  %-24s  %s", toggle, item.label, item.desc)
				sb.WriteString(cursorLineMarker + settingsCursorStyle.Render(line) + "\n")
			} else {
				sb.WriteString(fmt.Sprintf("   %s  %-24s  %s\n", toggle, item.label, desc))
			}

		// ── Editable string ───────────────────────────────────────────────
		case item.isString:
			val := item.getString(m.Settings)
			if val == "" {
				val = "(not set)"
			} else {
				val = truncate(val, 28)
			}
			desc := settingsDescStyle.Render(item.desc)
			if selected {
				line := fmt.Sprintf(" > %-20s %-28s  %s", item.label, val, item.desc)
				sb.WriteString(cursorLineMarker + settingsCursorStyle.Render(line) + "\n")
			} else {
				sb.WriteString(fmt.Sprintf("   %-20s %-28s  %s\n",
					item.label,
					settingsStringStyle.Render(val),
					desc))
			}

		// ── Action ────────────────────────────────────────────────────────
		case item.actionKey != "":
			desc := settingsDescStyle.Render(item.desc)
			if selected {
				line := fmt.Sprintf(" > ► %-28s  %s", item.label, item.desc)
				sb.WriteString(cursorLineMarker + settingsCursorStyle.Render(line) + "\n")
			} else {
				sb.WriteString(fmt.Sprintf("   %s %-28s  %s\n",
					settingsActionStyle.Render("►"),
					item.label,
					desc))
			}
		}
	}

	sb.WriteString("\n")
	return sb.String()
}
