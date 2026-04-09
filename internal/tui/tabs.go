package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tab is one screen of the dashboard.
type Tab int

const (
	TabDashboard Tab = iota
	TabSessions
	TabProjects
	TabModels
	TabTasks
	TabInsights
	TabSettings
)

func (t Tab) String() string {
	switch t {
	case TabDashboard:
		return "Dashboard"
	case TabSessions:
		return "Sessions"
	case TabProjects:
		return "Projects"
	case TabModels:
		return "Models"
	case TabTasks:
		return "Tasks"
	case TabInsights:
		return "Insights"
	case TabSettings:
		return "Settings"
	}
	return "?"
}

const tabCount = 7

var (
	tabActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12")).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Faint(true).Padding(0, 1)
)

// renderTabBar draws "1·Dashboard  2·Sessions  3·Projects  ..." with the
// active tab highlighted.
func renderTabBar(active Tab) string {
	var parts []string
	for i := 0; i < tabCount; i++ {
		t := Tab(i)
		label := tabLabelString(i+1, t.String())
		if t == active {
			parts = append(parts, tabActive.Render(label))
		} else {
			parts = append(parts, tabInactive.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

func tabLabelString(num int, name string) string {
	return string(rune('0'+num)) + "·" + name
}
