package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestResizeUpdatesViewport(t *testing.T) {
	cases := []struct {
		name       string
		width      int
		height     int
		wantWidth  int
		wantHeight int
	}{
		{name: "initial", width: 100, height: 40, wantWidth: 100, wantHeight: 35},
		{name: "grow", width: 160, height: 60, wantWidth: 160, wantHeight: 55},
		{name: "shrink", width: 70, height: 20, wantWidth: 70, wantHeight: 15},
		{name: "clamped to minimum", width: 60, height: 6, wantWidth: 60, wantHeight: 5},
	}
	m := newTestModel(t)
	var mm tea.Model = m
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mm, _ = mm.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			vp := mm.(Model).viewport
			if vp.Width != tc.wantWidth || vp.Height != tc.wantHeight {
				t.Errorf("viewport %dx%d, want %dx%d", vp.Width, vp.Height, tc.wantWidth, tc.wantHeight)
			}
		})
	}
}

func TestResizeDuringDrillDownKeepsCursorVisible(t *testing.T) {
	mm := enterDrillDown(t, viewDayBrowse)
	// Walk to the oldest day so the cursor sits far down the list.
	for i := 0; i < 40; i++ {
		mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	for _, h := range []int{50, 14, 30} {
		mm, _ = mm.Update(tea.WindowSizeMsg{Width: 120, Height: h})
		m := mm.(Model)
		if m.viewMode != viewDayBrowse {
			t.Fatalf("resize left drill-down mode: %d", m.viewMode)
		}
		line := cursorLineIndex(renderDayBrowse(m))
		if line < 0 {
			t.Fatal("no cursor line in day browse content")
		}
		if line < m.viewport.YOffset || line >= m.viewport.YOffset+m.viewport.Height {
			t.Errorf("height %d: cursor line %d outside viewport [%d,%d)",
				h, line, m.viewport.YOffset, m.viewport.YOffset+m.viewport.Height)
		}
	}
}

// cursorLineIndex returns the index of the marked cursor line, or -1.
func cursorLineIndex(content string) int {
	for i, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, cursorLineMarker) {
			return i
		}
	}
	return -1
}

func TestSettingsStringModalOpensAndCancels(t *testing.T) {
	m := sizedTestModel(t, 120, 40)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'8'}})

	idx := -1
	for i, item := range settingsItems() {
		if item.isString {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no editable string row in settings")
	}
	model := mm.(Model)
	model.settingsCursor = idx
	model.refreshViewport()

	mm, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !mm.(Model).settingsEditOpen {
		t.Fatal("enter on a string row should open the edit modal")
	}
	if !strings.Contains(mm.(Model).View(), "Edit value") {
		t.Error("edit modal should be visible")
	}
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if mm.(Model).settingsEditOpen {
		t.Error("esc should close the edit modal")
	}
	if mm.(Model).settingsInput.Value() != "" {
		t.Error("esc should clear the edit buffer")
	}
}

func TestNonASCIIProjectNameRenders(t *testing.T) {
	m := newTestModel(t)
	seedEvent(t, m, "u1", "sesión-ñandú")
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	mm, _ = mm.Update(refreshCmd(mm.(Model))().(refreshMsg))
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if out := mm.(Model).View(); !strings.Contains(out, "sesión") {
		t.Errorf("non-ASCII session id should render:\n%s", out)
	}
}
