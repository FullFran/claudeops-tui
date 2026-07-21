package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/config"
)

func hiddenTabsSettings() config.Settings {
	s := config.DefaultSettings()
	s.Tabs.Sessions = false
	s.Tabs.Models = false
	return s
}

func TestVisibleTabsHonorsSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings config.Settings
		want     []Tab
	}{
		{
			name:     "all enabled",
			settings: config.DefaultSettings(),
			want: []Tab{TabDashboard, TabSessions, TabProjects, TabModels,
				TabTasks, TabInsights, TabClassroom, TabSettings},
		},
		{
			name:     "sessions and models hidden",
			settings: hiddenTabsSettings(),
			want: []Tab{TabDashboard, TabProjects, TabTasks, TabInsights,
				TabClassroom, TabSettings},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := visibleTabs(tc.settings)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestClassroomTabHonorsItsToggle(t *testing.T) {
	s := config.DefaultSettings()
	s.Tabs.Classroom = false
	if tabVisible(TabClassroom, s) {
		t.Error("classroom must be hidden when its toggle is off")
	}
	for _, tab := range visibleTabs(s) {
		if tab == TabClassroom {
			t.Error("hidden classroom must be absent from visibleTabs")
		}
	}
}

func TestSettingsTabIsNeverHidden(t *testing.T) {
	s := config.DefaultSettings()
	s.Tabs = config.TabSettings{}
	for _, want := range []Tab{TabDashboard, TabSettings} {
		if !tabVisible(want, s) {
			t.Errorf("%v must stay visible with every [tabs] toggle off", want)
		}
	}
}

func TestHiddenTabIsAbsentFromTabBar(t *testing.T) {
	s := hiddenTabsSettings()
	bar := renderTabBar(TabDashboard, visibleTabs(s))
	for _, hidden := range []string{"Sessions", "Models"} {
		if strings.Contains(bar, hidden) {
			t.Errorf("hidden tab %q still rendered in tab bar: %s", hidden, bar)
		}
	}
	for _, shown := range []string{"Dashboard", "Projects", "Settings"} {
		if !strings.Contains(bar, shown) {
			t.Errorf("visible tab %q missing from tab bar: %s", shown, bar)
		}
	}
}

func TestHiddenTabIsUnreachable(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want Tab
	}{
		{name: "number key for hidden tab is ignored", keys: []string{"2"}, want: TabDashboard},
		{name: "number key for visible tab works", keys: []string{"3"}, want: TabProjects},
		{name: "next skips hidden tab", keys: []string{"tab"}, want: TabProjects},
		{name: "prev skips hidden tab", keys: []string{"3", "shift+tab"}, want: TabDashboard},
		{name: "next wraps from last tab", keys: []string{"8", "tab"}, want: TabDashboard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedTestModel(t, 120, 40)
			m.Settings = hiddenTabsSettings()
			m.refreshViewport()
			var mm tea.Model = m
			for _, k := range tc.keys {
				mm, _ = mm.Update(keyMsgFor(k))
			}
			if got := mm.(Model).activeTab; got != tc.want {
				t.Errorf("keys %v → tab %v, want %v", tc.keys, got, tc.want)
			}
		})
	}
}

// keyMsgFor builds the KeyMsg matching a key name as produced by KeyMsg.String().
func keyMsgFor(k string) tea.KeyMsg {
	switch k {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

func TestEnteringSettingsByCyclingSelectsFirstRow(t *testing.T) {
	m := sizedTestModel(t, 120, 40)
	// Cycle backwards from Dashboard — Settings is the last tab.
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	got := mm.(Model).activeTab
	if got != TabSettings {
		t.Fatalf("shift+tab from Dashboard → %v, want Settings", got)
	}
	cursor := mm.(Model).settingsCursor
	items := settingsItems()
	if cursor < 0 || cursor >= len(items) || isSettingsNonNav(items[cursor]) {
		t.Fatalf("settings cursor %d is not on a selectable row", cursor)
	}
}
