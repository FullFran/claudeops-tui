package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/fullfran/claudeops-tui/internal/config"
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
	TabClassroom
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
	case TabClassroom:
		return "Classroom"
	case TabSettings:
		return "Settings"
	}
	return "?"
}

var (
	tabActive   = lipgloss.NewStyle().Bold(true).Foreground(colOnAccent).Background(colAccent).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)
)

// allTabs is the canonical tab order. A tab's position here is its number key.
var allTabs = []Tab{
	TabDashboard, TabSessions, TabProjects, TabModels,
	TabTasks, TabInsights, TabClassroom, TabSettings,
}

// tabVisible reports whether a tab is enabled by the [tabs] settings.
// Dashboard, Classroom and Settings are always visible: Dashboard is the
// landing tab and Settings is the only place to turn the others back on.
func tabVisible(t Tab, s config.Settings) bool {
	switch t {
	case TabSessions:
		return s.Tabs.Sessions
	case TabProjects:
		return s.Tabs.Projects
	case TabModels:
		return s.Tabs.Models
	case TabTasks:
		return s.Tabs.Tasks
	case TabInsights:
		return s.Tabs.Insights
	}
	return true
}

// visibleTabs returns the enabled tabs in canonical order.
func visibleTabs(s config.Settings) []Tab {
	out := make([]Tab, 0, len(allTabs))
	for _, t := range allTabs {
		if tabVisible(t, s) {
			out = append(out, t)
		}
	}
	return out
}

// nextVisibleTab returns the visible tab delta steps away from cur, wrapping
// around. A hidden cur is treated as the first visible tab.
func nextVisibleTab(cur Tab, s config.Settings, delta int) Tab {
	tabs := visibleTabs(s)
	idx := 0
	for i, t := range tabs {
		if t == cur {
			idx = i
			break
		}
	}
	n := len(tabs)
	return tabs[((idx+delta)%n+n)%n]
}

// renderTabBar draws "1·Dashboard  2·Sessions  3·Projects  ..." with the
// active tab highlighted. Hidden tabs are omitted; the numbers stay tied to
// the canonical order so a tab's key never changes when another is hidden.
func renderTabBar(active Tab, tabs []Tab) string {
	var parts []string
	for _, t := range tabs {
		label := tabLabelString(int(t)+1, t.String())
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
