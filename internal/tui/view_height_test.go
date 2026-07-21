package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// sizedTestModel returns a model that has been resized and refreshed once.
func sizedTestModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	mm, _ = mm.Update(refreshCmd(mm.(Model))().(refreshMsg))
	return mm.(Model)
}

func TestViewFitsTerminalHeight(t *testing.T) {
	cases := []struct {
		name   string
		width  int
		height int
		mutate func(*Model)
	}{
		{name: "small", width: 80, height: 20},
		{name: "tall", width: 120, height: 40},
		{name: "tiny", width: 60, height: 10},
		{name: "with status", width: 80, height: 20, mutate: func(m *Model) {
			m.statusMsg = "saved config.toml"
		}},
		{name: "with task modal", width: 80, height: 20, mutate: func(m *Model) {
			m.taskInputOpen = true
		}},
		{name: "with settings modal and status", width: 120, height: 24, mutate: func(m *Model) {
			m.settingsEditOpen = true
			m.statusMsg = "pushing…"
		}},
		{name: "settings tab with status", width: 100, height: 30, mutate: func(m *Model) {
			m.activeTab = TabSettings
			m.statusMsg = "saved config.toml"
			m.refreshViewport()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedTestModel(t, tc.width, tc.height)
			if tc.mutate != nil {
				tc.mutate(&m)
			}
			out := m.View()
			if got := strings.Count(out, "\n") + 1; got > tc.height {
				t.Errorf("View() rendered %d lines, terminal height is %d", got, tc.height)
			}
		})
	}
}

func TestViewKeepsTitleAndTabBar(t *testing.T) {
	m := sizedTestModel(t, 80, 20)
	m.statusMsg = "saved config.toml"
	lines := strings.Split(m.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("view too short: %d lines", len(lines))
	}
	if !strings.Contains(lines[0], "claudeops") {
		t.Errorf("first line should be the brand, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "Dashboard") {
		t.Errorf("second line should be the tab bar, got %q", lines[1])
	}
}
