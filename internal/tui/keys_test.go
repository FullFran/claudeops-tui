package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSettingsKeysDoNotScrollViewport(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
	}{
		{name: "space toggles without paging", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}},
		{name: "enter toggles without paging", key: tea.KeyMsg{Type: tea.KeyEnter}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedTestModel(t, 100, 20)
			mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'8'}})
			before := mm.(Model).viewport.YOffset
			mm, _ = mm.Update(tc.key)
			if got := mm.(Model).viewport.YOffset; got != before {
				t.Errorf("YOffset moved from %d to %d on %s", before, got, tc.name)
			}
		})
	}
}
