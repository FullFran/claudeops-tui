package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	dimStyle    = lipgloss.NewStyle().Faint(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// View is the entrypoint for Bubbletea.
func (m Model) View() string {
	if m.showHelp {
		return renderHelp(m)
	}

	var sb strings.Builder

	// Title + tab bar
	sb.WriteString(titleStyle.Render("claudeops") + " " + dimStyle.Render(m.Version) + "\n")
	sb.WriteString(renderTabBar(m.activeTab) + "\n\n")

	// Body — every tab renders through the viewport so content scrolls
	// when the terminal is shorter than the rendered content.
	if m.ready {
		sb.WriteString(m.viewport.View() + "\n")
	} else {
		sb.WriteString(dimStyle.Render("loading…") + "\n")
	}

	// Modal: task input
	if m.taskInputOpen {
		sb.WriteString("\n" + headerStyle.Render("New task") + "\n")
		sb.WriteString("  " + m.taskInput.View() + "\n")
		sb.WriteString(dimStyle.Render("  enter: start · esc: cancel") + "\n")
	}
	// Modal: settings string edit
	if m.settingsEditOpen {
		sb.WriteString("\n" + headerStyle.Render("Edit value") + "\n")
		sb.WriteString("  " + m.settingsInput.View() + "\n")
		sb.WriteString(dimStyle.Render("  enter: save · esc: cancel") + "\n")
	}

	// Transient status line (last action result)
	if m.statusMsg != "" {
		sb.WriteString("\n" + warnStyle.Render(m.statusMsg))
	}

	// Footer — context-sensitive hints per view mode.
	hints := contextHints(m)
	footer := dimStyle.Render(fmt.Sprintf("pricing updated: %s   %s", m.PricingUpdated, hints))
	sb.WriteString("\n" + footer)

	return sb.String()
}

// renderHelp draws the full-screen help overlay.
func renderHelp(m Model) string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("claudeops — keybindings") + "\n\n")
	sb.WriteString(dimStyle.Render("  Navigation") + "\n")
	for _, r := range [][2]string{
		{"1-8", "switch to tab N"},
		{"tab / shift+tab", "cycle tabs forward / back"},
		{"← → h l", "cycle tabs"},
		{"↑ ↓ j k", "scroll content / navigate lists"},
		{"q / ctrl+c", "quit"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}

	sb.WriteString("\n" + dimStyle.Render("  Dashboard") + "\n")
	for _, r := range [][2]string{
		{"enter", "open daily breakdown (browse 30 days)"},
		{"r", "force refresh data"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}

	sb.WriteString("\n" + dimStyle.Render("  Daily breakdown") + "\n")
	for _, r := range [][2]string{
		{"j / k", "select day (newer / older)"},
		{"enter", "drill into day detail"},
		{"esc", "go back"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}

	sb.WriteString("\n" + dimStyle.Render("  Sessions") + "\n")
	for _, r := range [][2]string{
		{"enter", "open session browser"},
		{"j / k", "select session (down / up)"},
		{"enter (in list)", "drill into session detail"},
		{"esc", "go back"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}

	sb.WriteString("\n" + dimStyle.Render("  Settings tab") + "\n")
	for _, r := range [][2]string{
		{"j / k", "move cursor"},
		{"space / enter", "toggle bool (auto-saved)"},
		{"enter (on string)", "edit value inline"},
		{"enter (on ► action)", "run action (push, apply OTel)"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}

	sb.WriteString("\n" + dimStyle.Render("  Tasks") + "\n")
	for _, r := range [][2]string{
		{"n", "new task (opens input)"},
		{"S", "stop active task"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}
	sb.WriteString("\n" + dimStyle.Render("  Classroom") + "\n")
	for _, r := range [][2]string{
		{"(auto-refresh)", "live grid of Claude Code sessions (2s tick)"},
		{"✨ / 💤", "working / waiting for your input"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}
	sb.WriteString("\n" + dimStyle.Render("press any key to dismiss"))
	return sb.String()
}

// contextHints returns the footer hint string appropriate to the current view.
func contextHints(m Model) string {
	switch m.viewMode {
	case viewDayBrowse:
		return "j/k move · enter detail · esc back · ? help"
	case viewDayDetail:
		return "esc back to list · ? help"
	case viewSessionBrowse:
		return "j/k move · enter detail · esc back · ? help"
	case viewSessionDetail:
		return "esc back to list · ? help"
	}
	switch m.activeTab {
	case TabSettings:
		items := settingsItems()
		if m.settingsCursor >= 0 && m.settingsCursor < len(items) {
			item := items[m.settingsCursor]
			if item.isString {
				return "j/k move · enter edit · esc cancel · ? help"
			}
			if item.actionKey != "" {
				return "j/k move · enter run · ? help"
			}
		}
		return "j/k move · space/enter toggle · ? help"
	case TabDashboard:
		if len(m.Daily) > 0 {
			return "1-8 tabs · enter browse days · n task · r refresh · ? help · q quit"
		}
	case TabSessions:
		if len(m.AllSess) > 0 {
			return "1-8 tabs · enter browse · n task · r refresh · ? help · q quit"
		}
	}
	return "1-8 tabs · n task · S stop · r refresh · ? help · q quit"
}

// padRight pads a string with spaces on the right to width n.
func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// truncate cuts s to length n with an ellipsis when too long.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
