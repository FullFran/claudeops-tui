// Package tui implements the Bubbletea dashboard with multiple tabs.
package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/fullfran/claudeops-tui/internal/tasks"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// Model is the Bubbletea model for the multi-tab dashboard.
type Model struct {
	Store          *store.Store
	Usage          *usage.Client
	Tasks          *tasks.Tracker
	Settings       config.Settings
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
	Daily        []store.DailyAgg     // last 30 days, local TZ
	PerModelToday []store.ModelAgg    // per-model breakdown for today only
	BurnRate4h   float64              // €/hour over the last 4 hours
	Snap         *usage.Snapshot
	UsageErr     string
	ActiveTask   *tasks.Task

	// ui state
	activeTab Tab
	viewport  viewport.Model
	width     int
	height    int
	ready     bool

	// modal state
	taskInput     textinput.Model
	taskInputOpen bool
	showHelp      bool
	statusMsg     string // transient feedback (e.g. "task started: foo")
}

// New constructs a Model. Settings defaults to DefaultSettings() so callers
// (and tests) that don't care about config can omit it via NewWithSettings.
func New(s *store.Store, u *usage.Client, tr *tasks.Tracker, pricingUpdated, version string) Model {
	return NewWithSettings(s, u, tr, config.DefaultSettings(), pricingUpdated, version)
}

// NewWithSettings is the explicit constructor used by main.go.
func NewWithSettings(s *store.Store, u *usage.Client, tr *tasks.Tracker, settings config.Settings, pricingUpdated, version string) Model {
	ti := textinput.New()
	ti.Placeholder = "task name…"
	ti.CharLimit = 80
	ti.Width = 40
	return Model{
		Store:          s,
		Usage:          u,
		Tasks:          tr,
		Settings:       settings,
		PricingUpdated: pricingUpdated,
		Version:        version,
		activeTab:      TabDashboard,
		taskInput:      ti,
	}
}

// Init kicks off the first refresh and tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(refreshCmd(m), tickCmd())
}

type tickMsg struct{}

// taskActionMsg is the result of a Start/Stop call from the input modal.
type taskActionMsg struct {
	status string
	err    error
}

func startTaskCmd(tr *tasks.Tracker, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		t, err := tr.Start(ctx, name)
		if err != nil {
			return taskActionMsg{err: err}
		}
		return taskActionMsg{status: "task started: " + t.Name}
	}
}

func stopTaskCmd(tr *tasks.Tracker) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := tr.Stop(ctx); err != nil {
			return taskActionMsg{err: err}
		}
		return taskActionMsg{status: "task stopped"}
	}
}

type refreshMsg struct {
	today         store.Aggregates
	last7d        store.Aggregates
	topSess       []store.SessionAgg
	allSess       []store.SessionAgg
	topProj       []store.ProjectAgg
	allProj       []store.ProjectAgg
	perModel      []store.ModelAgg
	allTasks      []store.TaskAgg
	daily         []store.DailyAgg
	perModelToday []store.ModelAgg
	burnRate4h    float64
	snap          *usage.Snapshot
	usageErr      string
	activeTask    *tasks.Task
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func refreshCmd(m Model) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var msg refreshMsg
		now := time.Now()
		since7d := now.Add(-7 * 24 * time.Hour)
		since4h := now.Add(-4 * time.Hour)
		startOfTodayLocal := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if m.Store != nil {
			msg.today, _ = m.Store.AggregatesForToday(ctx)
			msg.last7d, _ = m.Store.AggregatesSince(ctx, since7d)
			msg.topSess, _ = m.Store.TopSessionsByCost(ctx, 5, since7d)
			msg.allSess, _ = m.Store.TopSessionsByCost(ctx, 500, time.Time{})
			msg.topProj, _ = m.Store.TopProjectsByCost(ctx, 5, since7d)
			msg.allProj, _ = m.Store.TopProjectsByCost(ctx, 500, time.Time{})
			msg.perModel, _ = m.Store.PerModelAggregates(ctx, time.Time{})
			msg.allTasks, _ = m.Store.TaskAggregates(ctx)
			msg.daily, _ = m.Store.DailyAggregatesLocal(ctx, 30)
			msg.perModelToday, _ = m.Store.PerModelAggregates(ctx, startOfTodayLocal)
			if a, err := m.Store.AggregatesSince(ctx, since4h); err == nil {
				msg.burnRate4h = a.CostEUR / 4.0
			}
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
		// Modal input has priority: capture all keys except submit/cancel.
		if m.taskInputOpen {
			switch msg.String() {
			case "esc", "ctrl+c":
				m.taskInputOpen = false
				m.taskInput.Blur()
				m.taskInput.SetValue("")
				return m, nil
			case "enter":
				name := strings.TrimSpace(m.taskInput.Value())
				m.taskInputOpen = false
				m.taskInput.Blur()
				m.taskInput.SetValue("")
				if name == "" || m.Tasks == nil {
					return m, nil
				}
				return m, startTaskCmd(m.Tasks, name)
			}
			var ic tea.Cmd
			m.taskInput, ic = m.taskInput.Update(msg)
			return m, ic
		}
		// Help overlay: any key dismisses it.
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = true
			return m, nil
		case "n":
			if m.Tasks != nil {
				m.taskInputOpen = true
				m.taskInput.Focus()
				return m, textinput.Blink
			}
		case "S":
			if m.Tasks != nil && m.ActiveTask != nil {
				return m, stopTaskCmd(m.Tasks)
			}
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
	case taskActionMsg:
		if msg.err != nil {
			m.statusMsg = "task error: " + msg.err.Error()
		} else {
			m.statusMsg = msg.status
		}
		cmds = append(cmds, refreshCmd(m))
	case refreshMsg:
		m.Today = msg.today
		m.Last7d = msg.last7d
		m.TopSess = msg.topSess
		m.AllSess = msg.allSess
		m.TopProj = msg.topProj
		m.AllProj = msg.allProj
		m.PerModel = msg.perModel
		m.AllTasks = msg.allTasks
		m.Daily = msg.daily
		m.PerModelToday = msg.perModelToday
		m.BurnRate4h = msg.burnRate4h
		m.Snap = msg.snap
		m.UsageErr = msg.usageErr
		m.ActiveTask = msg.activeTask
		m.refreshViewport()
	}
	// Forward unknown keys / scroll keys to the viewport for every tab.
	if m.ready {
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		if vpCmd != nil {
			cmds = append(cmds, vpCmd)
		}
	}
	return m, tea.Batch(cmds...)
}

// refreshViewport regenerates the viewport content for the active tab.
// Called whenever data or tab changes. Every tab — including Dashboard —
// renders into the viewport so content longer than the terminal scrolls.
func (m *Model) refreshViewport() {
	if !m.ready {
		return
	}
	var content string
	switch m.activeTab {
	case TabDashboard:
		content = renderDashboardTab(*m)
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
