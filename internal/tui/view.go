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

	// Body
	switch m.activeTab {
	case TabDashboard:
		sb.WriteString(renderDashboardTab(m))
	default:
		if m.ready {
			sb.WriteString(m.viewport.View() + "\n")
		} else {
			sb.WriteString(dimStyle.Render("loading…") + "\n")
		}
	}

	// Modal: task input
	if m.taskInputOpen {
		sb.WriteString("\n" + headerStyle.Render("New task") + "\n")
		sb.WriteString("  " + m.taskInput.View() + "\n")
		sb.WriteString(dimStyle.Render("  enter: start · esc: cancel") + "\n")
	}

	// Transient status line (last action result)
	if m.statusMsg != "" {
		sb.WriteString("\n" + warnStyle.Render(m.statusMsg))
	}

	// Footer
	hints := "1-5 tabs · tab/⇄ · ↑↓ scroll · n new task · S stop task · r refresh · ? help · q quit"
	footer := dimStyle.Render(fmt.Sprintf("pricing updated: %s   %s", m.PricingUpdated, hints))
	sb.WriteString("\n" + footer)

	return sb.String()
}

// renderHelp draws the full-screen help overlay.
func renderHelp(m Model) string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("claudeops — keybindings") + "\n\n")
	rows := [][2]string{
		{"1-5", "switch to tab N"},
		{"tab / shift+tab", "cycle tabs forward / back"},
		{"← → h l", "cycle tabs"},
		{"↑ ↓", "scroll active tab"},
		{"r", "force refresh"},
		{"n", "new task (opens input)"},
		{"S", "stop active task"},
		{"?", "toggle this help"},
		{"q / ctrl+c", "quit"},
	}
	for _, r := range rows {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}
	sb.WriteString("\n" + dimStyle.Render("press any key to dismiss"))
	return sb.String()
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
