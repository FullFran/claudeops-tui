package tui

import (
	"context"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// TestUIPreview renders the dashboard with sample data and prints it with real
// colors so the layout can be eyeballed. Skipped unless CLAUDEOPS_UI_PREVIEW=1.
//
//	CLAUDEOPS_UI_PREVIEW=1 go test -run TestUIPreview -v ./internal/tui/
func TestUIPreview(t *testing.T) {
	if os.Getenv("CLAUDEOPS_UI_PREVIEW") == "" {
		t.Skip("set CLAUDEOPS_UI_PREVIEW=1 to render a colored UI preview")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := newTestModel(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// Seed a few days of cost so the sparkline and Today block render.
	for i := 0; i < 6; i++ {
		cost := 1.5 + float64(i)
		ev := store.Event{
			UUID: "prev-" + time.Duration(i).String(), SessionID: "sess-preview", CWD: "/p/demo",
			Type: "assistant", Model: "claude-opus-4-8", TS: now.Add(-time.Duration(i) * 24 * time.Hour),
			InTokens: 1000 * int64(i+1), OutTokens: 400, CacheReadTokens: 2000, Source: "claude",
		}
		_ = m.Store.Insert(ctx, ev, &cost, nil)
	}

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 44})
	msg := refreshCmd(mm.(Model))().(refreshMsg)
	mm, _ = mm.Update(msg)

	model := mm.(Model)
	// Inject provider meters at healthy / caution / danger levels.
	reset := now.Add(3 * time.Hour)
	model.ProviderUsages = []provider.Result{
		{Name: "Codex", Usage: provider.Usage{Provider: "Codex", Note: "plan: plus", Windows: []provider.Window{
			{Label: "5h", Utilization: 22.5, ResetsAt: reset},
			{Label: "7d", Utilization: 71.0, ResetsAt: now.Add(5 * 24 * time.Hour)},
		}}},
		{Name: "Copilot", Err: nil, Usage: provider.Usage{Provider: "Copilot", Windows: []provider.Window{
			{Label: "premium", Utilization: 93.0},
		}}},
	}
	model.Snap = &usage.Snapshot{FiveHour: &usage.Bucket{Utilization: 15, ResetsAt: reset}}
	model.refreshViewport()

	t.Log("\n=== All subscriptions (default) ===\n" + model.View())

	// Focus a single subscription with the `p` selector.
	focused, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	focused, _ = focused.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	t.Log("\n=== Focused (p pressed twice → Codex) ===\n" + focused.(Model).View())
}
