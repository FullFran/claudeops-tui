package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// enterDrillDown drives the model into the requested drill-down view mode.
func enterDrillDown(t *testing.T, mode viewMode) tea.Model {
	t.Helper()
	m := newTestModel(t)
	seedEvent(t, m, "u1", "sess-help-01")
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	mm, _ = mm.Update(refreshCmd(mm.(Model))().(refreshMsg))

	switch mode {
	case viewSessionBrowse, viewSessionDetail:
		mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	}
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if mode == viewDayDetail || mode == viewSessionDetail {
		var cmd tea.Cmd
		mm, cmd = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("enter should start a detail load")
		}
		mm, _ = mm.Update(cmd())
	}
	if got := mm.(Model).viewMode; got != mode {
		t.Fatalf("setup landed in viewMode %d, want %d", got, mode)
	}
	return mm
}

func TestHelpOpensInEveryDrillDownMode(t *testing.T) {
	cases := []struct {
		name string
		mode viewMode
	}{
		{name: "day browse", mode: viewDayBrowse},
		{name: "day detail", mode: viewDayDetail},
		{name: "session browse", mode: viewSessionBrowse},
		{name: "session detail", mode: viewSessionDetail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mm := enterDrillDown(t, tc.mode)
			mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
			if !mm.(Model).showHelp {
				t.Fatal("? should open help, the footer advertises it")
			}
			if !strings.Contains(mm.(Model).View(), "keybindings") {
				t.Error("help overlay should be rendered")
			}
			// Dismissing keeps the drill-down the user was in.
			mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			if mm.(Model).showHelp {
				t.Error("any key should dismiss help")
			}
			if got := mm.(Model).viewMode; got != tc.mode {
				t.Errorf("help should not change viewMode, got %d want %d", got, tc.mode)
			}
		})
	}
}

func TestHelpDescribesDailyBreakdownKeysCorrectly(t *testing.T) {
	// j selects an older day (lower index in the newest-first list).
	mm := enterDrillDown(t, viewDayBrowse)
	before := mm.(Model).dayCursor
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if mm.(Model).dayCursor >= before {
		t.Fatal("j should select an older day")
	}
	help := renderHelp(mm.(Model))
	if !strings.Contains(help, "select day (older / newer)") {
		t.Errorf("help must describe j/k as older/newer:\n%s", help)
	}
}

func TestHelpDescribesQuitAndBack(t *testing.T) {
	m := sizedTestModel(t, 120, 40)
	help := renderHelp(m)
	if !strings.Contains(help, "back inside drill-downs") {
		t.Errorf("help must note that q goes back inside drill-downs:\n%s", help)
	}
}
