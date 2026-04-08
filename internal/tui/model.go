// Package tui implements the Bubbletea dashboard with multiple tabs.
package tui

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/fullfran/claudeops-tui/internal/tasks"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// Model is the Bubbletea model for the multi-tab dashboard.
type Model struct {
	Store          *store.Store
	Usage          *usage.Client
	Tasks          *tasks.Tracker
	PricingUpdated string
	Version        string

	// snapshot
	Today        store.Aggregates
	Last7d       store.Aggregates
	TopSess      []store.SessionAgg
	AllSess      []store.SessionAgg
	TopProj      []store.ProjectAgg
	AllProj      []store.ProjectAgg
	PerModel     []store.ModelAgg
	AllTasks     []store.TaskAgg
	Snap         *usage.Snapshot
	UsageErr     string
	ActiveTask   *tasks.Task

	// ui state
	activeTab Tab
	viewport  viewport.Model
	width     int
	height    int
	ready     bool
}

// New constructs a Model.
func New(s *store.Store, u *usage.Client, tr *tasks.Tracker, pricingUpdated, version string) Model {
	return Model{
		Store:          s,
		Usage:          u,
		Tasks:          tr,
		PricingUpdated: pricingUpdated,
		Version:        version,
		activeTab:      TabDashboard,
	}
}

// Init kicks off the first refresh and tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(refreshCmd(m), tickCmd())
}

type tickMsg struct{}

type refreshMsg struct {
	today      store.Aggregates
	last7d     store.Aggregates
	topSess    []store.SessionAgg
	allSess    []store.SessionAgg
	topProj    []store.ProjectAgg
	allProj    []store.ProjectAgg
	perModel   []store.ModelAgg
	allTasks   []store.TaskAgg
	snap       *usage.Snapshot
	usageErr   string
	activeTask *tasks.Task
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func refreshCmd(m Model) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var msg refreshMsg
		since7d := time.Now().Add(-7 * 24 * time.Hour)
		if m.Store != nil {
			msg.today, _ = m.Store.AggregatesForToday(ctx)
			msg.last7d, _ = m.Store.AggregatesSince(ctx, since7d)
			msg.topSess, _ = m.Store.TopSessionsByCost(ctx, 5, since7d)
			msg.allSess, _ = m.Store.TopSessionsByCost(ctx, 500, time.Time{})
			msg.topProj, _ = m.Store.TopProjectsByCost(ctx, 5, since7d)
			msg.allProj, _ = m.Store.TopProjectsByCost(ctx, 500, time.Time{})
			msg.perModel, _ = m.Store.PerModelAggregates(ctx, time.Time{})
			msg.allTasks, _ = m.Store.TaskAggregates(ctx)
		}
		if m.Usage != nil {
			snap, err := m.Usage.Get(ctx)
			if err != nil {
				if errors.Is(err, usage.ErrUsageUnavailable) {
					msg.usageErr = "subscription % unavailable (API key mode)"
				} else if errors.Is(err, usage.ErrAuthExpired) {
					msg.usageErr = "run `claude /login` to re-auth"
				} else {
					msg.usageErr = "usage: " + err.Error()
				}
			} else {
				msg.snap = &snap
			}
		}
		if m.Tasks != nil {
			if t, ok := m.Tasks.Current(); ok {
				msg.activeTask = t
			}
		}
		return msg
	}
}

// Update handles incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			cmds = append(cmds, refreshCmd(m))
		case "tab", "right", "l":
			m.activeTab = (m.activeTab + 1) % tabCount
			m.refreshViewport()
		case "shift+tab", "left", "h":
			m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
			m.refreshViewport()
		case "1":
			m.activeTab = TabDashboard
			m.refreshViewport()
		case "2":
			m.activeTab = TabSessions
			m.refreshViewport()
		case "3":
			m.activeTab = TabProjects
			m.refreshViewport()
		case "4":
			m.activeTab = TabModels
			m.refreshViewport()
		case "5":
			m.activeTab = TabTasks
			m.refreshViewport()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Reserve 4 lines for tab bar + footer.
		vpH := msg.Height - 4
		if vpH < 5 {
			vpH = 5
		}
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpH)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpH
		}
		m.refreshViewport()
	case tickMsg:
		cmds = append(cmds, refreshCmd(m), tickCmd())
	case refreshMsg:
		m.Today = msg.today
		m.Last7d = msg.last7d
		m.TopSess = msg.topSess
		m.AllSess = msg.allSess
		m.TopProj = msg.topProj
		m.AllProj = msg.allProj
		m.PerModel = msg.perModel
		m.AllTasks = msg.allTasks
		m.Snap = msg.snap
		m.UsageErr = msg.usageErr
		m.ActiveTask = msg.activeTask
		m.refreshViewport()
	}
	// Forward unknown keys / scroll keys to the viewport for the scrollable tabs.
	if m.activeTab != TabDashboard && m.ready {
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		if vpCmd != nil {
			cmds = append(cmds, vpCmd)
		}
	}
	return m, tea.Batch(cmds...)
}

// refreshViewport regenerates the viewport content for the active tab.
// Called whenever data or tab changes.
func (m *Model) refreshViewport() {
	if !m.ready || m.activeTab == TabDashboard {
		return
	}
	var content string
	switch m.activeTab {
	case TabSessions:
		content = renderSessionsTab(*m)
	case TabProjects:
		content = renderProjectsTab(*m)
	case TabModels:
		content = renderModelsTab(*m)
	case TabTasks:
		content = renderTasksTab(*m)
	}
	m.viewport.SetContent(content)
}
