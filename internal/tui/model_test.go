package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/fullfran/claudeops-tui/internal/tasks"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	tbl, _ := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
	_ = tbl
	tr := tasks.New(filepath.Join(dir, "current-task.json"), s)
	return New(s, nil, tr, "2026-04-08", "v0.1")
}

func TestEmptyDashboardRendersPlaceholders(t *testing.T) {
	m := newTestModel(t)
	// Apply one refresh synchronously.
	cmd := refreshCmd(m)
	msg := cmd().(refreshMsg)
	mm, _ := m.Update(msg)
	out := mm.(Model).View()

	for _, want := range []string{"claudeops", "Subscription usage", "Today", "Top sessions", "Top projects", "Active task", "no active task"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in view\n--\n%s", want, out)
		}
	}
	if strings.Contains(out, "error") || strings.Contains(out, "panic") {
		t.Errorf("unexpected error in view: %s", out)
	}
}

func TestDashboardWithDataShowsNumbers(t *testing.T) {
	m := newTestModel(t)
	ctx := context.Background()
	cost := 2.5
	ev := store.Event{
		UUID: "u1", SessionID: "sess-abcdef12", CWD: "/p/alpha",
		Type: "assistant", Model: "claude-opus-4-6", TS: time.Now().UTC(),
		InTokens: 5, OutTokens: 1101, CacheReadTokens: 15718, CacheCreateTokens: 20780,
	}
	if err := m.Store.Insert(ctx, ev, &cost, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Tasks.Start(ctx, "demo task"); err != nil {
		t.Fatal(err)
	}

	msg := refreshCmd(m)().(refreshMsg)
	mm, _ := m.Update(msg)
	out := mm.(Model).View()

	for _, want := range []string{"events: 1", "demo task", "alpha", "€2.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in view\n--\n%s", want, out)
		}
	}
}

func TestQuitOnQ(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
}

func TestTabSwitchingByNumberKeys(t *testing.T) {
	m := newTestModel(t)
	// Apply window size so the viewport is ready.
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	for key, want := range map[string]Tab{"2": TabSessions, "3": TabProjects, "4": TabModels, "5": TabTasks, "1": TabDashboard} {
		mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if got := mm.(Model).activeTab; got != want {
			t.Errorf("key %q → tab %v, want %v", key, got, want)
		}
	}
}

func TestHelpOverlayToggle(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !mm.(Model).showHelp {
		t.Fatal("? should open help")
	}
	out := mm.(Model).View()
	if !strings.Contains(out, "keybindings") || !strings.Contains(out, "new task") {
		t.Errorf("help overlay missing content:\n%s", out)
	}
	// Any key dismisses.
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if mm.(Model).showHelp {
		t.Fatal("any key should close help")
	}
}

func TestNewTaskFromTUI(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Press `n` → opens input modal.
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !mm.(Model).taskInputOpen {
		t.Fatal("n should open task input")
	}

	// Type a name. textinput consumes runes one at a time.
	for _, r := range "refactor parser" {
		mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Submit with enter → returns a startTaskCmd.
	mm2, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should return a start cmd")
	}
	if mm2.(Model).taskInputOpen {
		t.Fatal("input should be closed after submit")
	}
	// Execute the cmd → produces a taskActionMsg, then apply it.
	resMsg := cmd()
	if _, ok := resMsg.(taskActionMsg); !ok {
		t.Fatalf("expected taskActionMsg, got %T", resMsg)
	}
	mm3, _ := mm2.Update(resMsg)
	if !strings.Contains(mm3.(Model).statusMsg, "task started") {
		t.Errorf("status: %q", mm3.(Model).statusMsg)
	}
	// Tracker actually has the task.
	if cur, ok := mm3.(Model).Tasks.Current(); !ok || cur.Name != "refactor parser" {
		t.Errorf("tracker state wrong: %+v ok=%v", cur, ok)
	}
}

func TestTaskInputEscCancels(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if mm.(Model).taskInputOpen {
		t.Fatal("esc should cancel input")
	}
	if _, ok := mm.(Model).Tasks.Current(); ok {
		t.Fatal("no task should have been started")
	}
}

func TestAllTabsRenderWithoutPanic(t *testing.T) {
	m := newTestModel(t)
	ctx := context.Background()
	cost := 1.5
	ev := store.Event{
		UUID: "u1", SessionID: "s1", CWD: "/p/alpha", Type: "assistant",
		Model: "claude-sonnet-4-6", TS: time.Now().UTC(),
		InTokens: 10, OutTokens: 20, CacheReadTokens: 30, CacheCreateTokens: 40,
	}
	_ = m.Store.Insert(ctx, ev, &cost, nil)
	_, _ = m.Tasks.Start(ctx, "demo")

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	mm, _ = mm.Update(refreshCmd(mm.(Model))().(refreshMsg))

	for _, tab := range []Tab{TabDashboard, TabSessions, TabProjects, TabModels, TabTasks} {
		mm2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune('1' + int(tab))}})
		out := mm2.(Model).View()
		if !strings.Contains(out, tab.String()) {
			t.Errorf("tab %s: expected its name in view\n--\n%s", tab, out)
		}
		if strings.Contains(out, "panic") {
			t.Errorf("tab %s: panic in view", tab)
		}
	}
}
