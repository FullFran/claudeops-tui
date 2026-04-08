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

	// Footer
	hints := "1-5 tabs · tab/⇄ cycle · ↑↓ scroll · r refresh · q quit"
	footer := dimStyle.Render(fmt.Sprintf("pricing updated: %s   %s", m.PricingUpdated, hints))
	sb.WriteString("\n" + footer)

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
