package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// seedTwoSubs returns a model with Claude + Codex subscriptions present.
func seedTwoSubs(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	m = mm.(Model)
	m.Snap = &usage.Snapshot{FiveHour: &usage.Bucket{Utilization: 10}}
	m.ProviderUsages = []provider.Result{
		{Name: "Codex", Usage: provider.Usage{Provider: "Codex", Windows: []provider.Window{
			{Label: "5h", Utilization: 22},
		}}},
	}
	return m
}

func TestSubscriptionNamesOrder(t *testing.T) {
	m := seedTwoSubs(t)
	names := subscriptionNames(m)
	if len(names) != 2 || names[0] != "Claude" || names[1] != "Codex" {
		t.Fatalf("names = %v, want [Claude Codex]", names)
	}
}

func TestSubSelectorShowsChipsAndAll(t *testing.T) {
	m := seedTwoSubs(t)
	m.refreshViewport()
	out := m.View()
	// Chip row + both entries visible when focus = All.
	for _, want := range []string{"All", "Claude", "Codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in view\n%s", want, out)
		}
	}
}

func TestPKeyCyclesFocus(t *testing.T) {
	m := seedTwoSubs(t)
	m.refreshViewport()

	// All(0) -> Claude(1) -> Codex(2) -> All(0)
	press := func(mod Model) Model {
		nm, _ := mod.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
		return nm.(Model)
	}

	if m.subFocus != 0 {
		t.Fatalf("initial subFocus = %d, want 0", m.subFocus)
	}
	m = press(m)
	if m.subFocus != 1 {
		t.Fatalf("after 1 press subFocus = %d, want 1", m.subFocus)
	}
	// Focused on Claude: Codex meters hidden.
	out := m.View()
	if strings.Contains(out, "Codex") && strings.Contains(out, "\n  Codex\n") {
		t.Errorf("Codex block should be hidden when focused on Claude")
	}
	m = press(m)
	if m.subFocus != 2 {
		t.Fatalf("after 2 presses subFocus = %d, want 2", m.subFocus)
	}
	m = press(m)
	if m.subFocus != 0 {
		t.Fatalf("after 3 presses subFocus = %d, want 0 (wrap)", m.subFocus)
	}
}

func TestPKeyNoopWithSingleSubscription(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	m = mm.(Model)
	m.Snap = &usage.Snapshot{FiveHour: &usage.Bucket{Utilization: 10}} // only Claude
	m.refreshViewport()

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if nm.(Model).subFocus != 0 {
		t.Errorf("subFocus changed with a single subscription; want 0")
	}
}
