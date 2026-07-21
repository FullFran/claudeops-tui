package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Semantic palette. Every color is adaptive so the UI stays legible on both
// light and dark terminals (the old fixed ANSI 12/14 were near-invisible on
// light backgrounds — an accessibility bug).
var (
	colAccent   = lipgloss.AdaptiveColor{Light: "#0057d8", Dark: "#7aa2f7"} // primary
	colAccent2  = lipgloss.AdaptiveColor{Light: "#0f766e", Dark: "#5fd7d7"} // secondary
	colMuted    = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#8a8f98"} // de-emphasized
	colOk       = lipgloss.AdaptiveColor{Light: "#0f8a3c", Dark: "#7bd88f"} // healthy
	colWarn     = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#e5c07b"} // caution
	colErr      = lipgloss.AdaptiveColor{Light: "#c0392b", Dark: "#f7768e"} // danger
	colOnAccent = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#11121a"} // text on accent fill

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent2)
	dimStyle    = lipgloss.NewStyle().Foreground(colMuted)
	warnStyle   = lipgloss.NewStyle().Foreground(colWarn)
	errStyle    = lipgloss.NewStyle().Foreground(colErr)
)

// chromeLines is the number of lines View() always draws around the viewport:
// title, tab bar, rule, spacer and footer. Modals and the status line add more
// on top and are absorbed by clipping the body.
const chromeLines = 5

// View is the entrypoint for Bubbletea.
func (m Model) View() string {
	if m.showHelp {
		return renderHelp(m)
	}

	// Title + tab bar, separated from the body by a full-width rule so the
	// chrome reads as one header block.
	brand := titleStyle.Render("◆ claudeops")
	head := brand + " " + dimStyle.Render(m.Version) + "\n" +
		renderTabBar(m.activeTab, visibleTabs(m.Settings)) + "\n" +
		dimStyle.Render(ruleLine(m.width))

	// Tail — modals, status line and footer. Every block is prefixed with a
	// newline and none ends with one, so the tail height is exact.
	var tb strings.Builder
	// Modal: task input
	if m.taskInputOpen {
		tb.WriteString("\n" + headerStyle.Render("New task") + "\n")
		tb.WriteString("  " + m.taskInput.View() + "\n")
		tb.WriteString(dimStyle.Render("  enter: start · esc: cancel") + "\n")
	}
	// Modal: settings string edit
	if m.settingsEditOpen {
		tb.WriteString("\n" + headerStyle.Render("Edit value") + "\n")
		tb.WriteString("  " + m.settingsInput.View() + "\n")
		tb.WriteString(dimStyle.Render("  enter: save · esc: cancel") + "\n")
	}
	// Transient status line (last action result)
	if m.statusMsg != "" {
		tb.WriteString("\n" + warnStyle.Render(m.statusMsg))
	}
	// Footer — context-sensitive hints per view mode.
	tb.WriteString("\n" + dimStyle.Render(fmt.Sprintf("pricing updated: %s   %s",
		m.PricingUpdated, contextHints(m))))
	tail := tb.String()

	// Body — every tab renders through the viewport so content scrolls
	// when the terminal is shorter than the rendered content.
	body := dimStyle.Render("loading…")
	if m.ready {
		body = m.viewport.View()
	}
	// The viewport height is set on resize, but modals and the status line
	// appear without one. Clip the body to what is actually left so the
	// renderer never drops the title row off the top of the altscreen.
	if m.height > 0 {
		body = clipHeight(body, m.height-lipgloss.Height(head)-lipgloss.Height(tail))
	}

	return head + "\n" + body + "\n" + tail
}

// clipHeight truncates s to at most n lines (always keeping at least one).
func clipHeight(s string, n int) string {
	if n < 1 {
		n = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
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
		{"q / ctrl+c", "quit (q goes back inside drill-downs)"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}

	sb.WriteString("\n" + dimStyle.Render("  Dashboard") + "\n")
	for _, r := range [][2]string{
		{"enter", "open daily breakdown (browse 30 days)"},
		{"p", "switch subscription (All / Claude / Codex / …)"},
		{"r", "force refresh data"},
	} {
		sb.WriteString("  " + headerStyle.Render(padRight(r[0], 18)) + r[1] + "\n")
	}

	sb.WriteString("\n" + dimStyle.Render("  Daily breakdown") + "\n")
	for _, r := range [][2]string{
		{"j / k", "select day (older / newer)"},
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
		sub := ""
		if len(subscriptionNames(m)) > 1 {
			sub = "p switch sub · "
		}
		if len(m.Daily) > 0 {
			return "1-8 tabs · " + sub + "enter browse days · n task · r refresh · ? help · q quit"
		}
		if sub != "" {
			return "1-8 tabs · " + sub + "n task · r refresh · ? help · q quit"
		}
	case TabSessions:
		if len(m.AllSess) > 0 {
			return "1-8 tabs · enter browse · n task · r refresh · ? help · q quit"
		}
	}
	return "1-8 tabs · n task · S stop · r refresh · ? help · q quit"
}

// ruleLine returns a horizontal rule sized to the terminal width (capped so it
// never dominates very wide terminals). Falls back to a sensible default when
// the width is not yet known.
func ruleLine(width int) string {
	w := width
	if w <= 0 {
		w = 72
	}
	return strings.Repeat("─", min(w, 100))
}

// padRight pads a string to a display width of n, measuring rune/display width
// (not bytes) so emoji and accented text stay column-aligned.
func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// truncate cuts s to a display width of n with an ellipsis, never slicing a
// multi-byte rune in half.
func truncate(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	if n <= 1 {
		r := []rune(s)
		if len(r) == 0 {
			return s
		}
		return string(r[:1])
	}
	out := make([]rune, 0, n)
	width := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if width+rw > n-1 {
			break
		}
		out = append(out, r)
		width += rw
	}
	return string(out) + "…"
}
