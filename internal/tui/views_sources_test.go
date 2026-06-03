package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// TestDashboardShowsSourceBreakdown covers REQ-4.2: TUI displays source attribution.
// Drives Model.Update() directly (go-testing decision gate: TUI state transition).
func TestDashboardShowsSourceBreakdown(t *testing.T) {
	m := newTestModel(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Insert one claude event and one codex event with distinct sources.
	claudeCost := 2.0
	codexCost := 0.5

	claudeEv := store.Event{
		UUID: "src-claude-1", SessionID: "src-sess-claude", CWD: "/p/alpha",
		Type: "assistant", Model: "claude-sonnet-4-6", TS: now,
		InTokens: 100, OutTokens: 50,
		Source: "claude",
	}
	if err := m.Store.Insert(ctx, claudeEv, &claudeCost, nil); err != nil {
		t.Fatal(err)
	}

	codexEv := store.Event{
		UUID: "src-codex-1", SessionID: "src-sess-codex", CWD: "/p/beta",
		Type: "assistant", Model: "codex-model", TS: now,
		InTokens: 50, OutTokens: 20,
		Source: "codex",
	}
	if err := m.Store.Insert(ctx, codexEv, &codexCost, nil); err != nil {
		t.Fatal(err)
	}

	// Initialize the viewport and refresh.
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	msg := refreshCmd(mm.(Model))().(refreshMsg)
	mm, _ = mm.Update(msg)

	// REQ-4.2 acceptance: given DB rows with mixed sources, the TUI renders
	// source identifiers without panicking.
	out := mm.(Model).View()
	if strings.Contains(out, "panic") {
		t.Errorf("panic in view with mixed sources:\n%s", out)
	}

	// Source breakdown section must appear on the Dashboard.
	if !strings.Contains(out, "By source") {
		t.Errorf("expected 'By source' section in dashboard view:\n%s", out)
	}

	// Both source names must be visible.
	if !strings.Contains(out, "claude") {
		t.Errorf("expected 'claude' source in dashboard:\n%s", out)
	}
	if !strings.Contains(out, "codex") {
		t.Errorf("expected 'codex' source in dashboard:\n%s", out)
	}
}

// TestDashboardSourceBreakdownEmpty verifies no panic or broken layout
// when there are no events (empty source aggs).
func TestDashboardSourceBreakdownEmpty(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	msg := refreshCmd(mm.(Model))().(refreshMsg)
	mm, _ = mm.Update(msg)

	out := mm.(Model).View()
	if strings.Contains(out, "panic") {
		t.Errorf("panic in empty dashboard: %s", out)
	}
	// With no data the "By source" section should not appear (empty means no rows).
}

// TestSourceAggsLoadedOnRefresh verifies SourceAggs is populated by refreshCmd.
func TestSourceAggsLoadedOnRefresh(t *testing.T) {
	m := newTestModel(t)
	ctx := context.Background()
	now := time.Now().UTC()

	cost := 1.0
	ev := store.Event{
		UUID: "sagtest-1", SessionID: "sagtest-sess", CWD: "/p/x",
		Type: "assistant", Model: "claude-sonnet-4-6", TS: now,
		InTokens: 10, Source: "claude",
	}
	if err := m.Store.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatal(err)
	}

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	msg := refreshCmd(mm.(Model))().(refreshMsg)
	mm, _ = mm.Update(msg)

	srcAggs := mm.(Model).SourceAggs
	if len(srcAggs) == 0 {
		t.Fatal("expected SourceAggs to be populated after refresh with source='claude' event")
	}
	found := false
	for _, ag := range srcAggs {
		if ag.Source == "claude" {
			found = true
			if ag.Events != 1 {
				t.Errorf("claude: want 1 event, got %d", ag.Events)
			}
		}
	}
	if !found {
		t.Errorf("claude source not found in SourceAggs: %+v", srcAggs)
	}
}
