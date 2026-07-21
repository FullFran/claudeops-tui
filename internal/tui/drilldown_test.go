package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/store"
)

// seedEvent inserts one priced event so the drill-down lists are non-empty.
func seedEvent(t *testing.T, m Model, uuid, sessionID string) {
	t.Helper()
	cost := 1.25
	ev := store.Event{
		UUID: uuid, SessionID: sessionID, CWD: "/p/alpha", Type: "assistant",
		Model: "claude-opus-4-6", TS: time.Now().UTC(),
		InTokens: 10, OutTokens: 20, CacheReadTokens: 30, CacheCreateTokens: 40,
	}
	if err := m.Store.Insert(context.Background(), ev, &cost, nil); err != nil {
		t.Fatal(err)
	}
}

func TestStaleDrillDownResultIsDropped(t *testing.T) {
	cases := []struct {
		name      string
		enter     []tea.KeyMsg // keys that open the browse list
		switchTab bool         // also move to another tab before the result lands
	}{
		{
			name:  "day detail after esc",
			enter: []tea.KeyMsg{{Type: tea.KeyEnter}},
		},
		{
			name:      "day detail after esc and tab switch",
			enter:     []tea.KeyMsg{{Type: tea.KeyEnter}},
			switchTab: true,
		},
		{
			name: "session detail after esc",
			enter: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune{'2'}},
				{Type: tea.KeyEnter},
			},
		},
		{
			name: "session detail after esc and tab switch",
			enter: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune{'2'}},
				{Type: tea.KeyEnter},
			},
			switchTab: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			seedEvent(t, m, "u1", "sess-stale-01")
			mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
			mm, _ = mm.Update(refreshCmd(mm.(Model))().(refreshMsg))
			for _, k := range tc.enter {
				mm, _ = mm.Update(k)
			}

			// Kick off the async load, then leave before it lands.
			mm, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("enter should start a detail load")
			}
			stale := cmd()
			mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
			wantTab := mm.(Model).activeTab
			if tc.switchTab {
				mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
				wantTab = TabProjects
			}

			mm, _ = mm.Update(stale)
			if got := mm.(Model).viewMode; got != viewNormal {
				t.Errorf("stale result changed viewMode to %d", got)
			}
			if got := mm.(Model).activeTab; got != wantTab {
				t.Errorf("stale result changed activeTab to %v, want %v", got, wantTab)
			}
		})
	}
}

func TestTimelyDrillDownResultIsApplied(t *testing.T) {
	m := newTestModel(t)
	seedEvent(t, m, "u1", "sess-fresh-01")
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	mm, _ = mm.Update(refreshCmd(mm.(Model))().(refreshMsg))
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	mm, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should start a detail load")
	}
	mm, _ = mm.Update(cmd())
	if got := mm.(Model).viewMode; got != viewDayDetail {
		t.Fatalf("timely result should open the day detail, got %d", got)
	}
}
