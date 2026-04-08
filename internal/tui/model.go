// Package tui implements the Bubbletea dashboard. MVP is a single view.
package tui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/fullfran/claudeops-tui/internal/tasks"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

// Model is the Bubbletea model for the dashboard.
type Model struct {
	Store          *store.Store
	Usage          *usage.Client
	Tasks          *tasks.Tracker
	PricingUpdated string
	Version        string

	// snapshot
	Today      store.Aggregates
	TopSess    []store.SessionAgg
	TopProj    []store.ProjectAgg
	Snap       *usage.Snapshot
	UsageErr   string
	ActiveTask *tasks.Task
	Warnings   []string

	width  int
	height int
}

// New constructs a Model.
func New(s *store.Store, u *usage.Client, tr *tasks.Tracker, pricingUpdated, version string) Model {
	return Model{
		Store:          s,
		Usage:          u,
		Tasks:          tr,
		PricingUpdated: pricingUpdated,
		Version:        version,
	}
}

// Init kicks off the first refresh.
func (m Model) Init() tea.Cmd {
	return tea.Batch(refreshCmd(m), tickCmd())
}

type tickMsg struct{}
type refreshMsg struct {
	today      store.Aggregates
	topSess    []store.SessionAgg
	topProj    []store.ProjectAgg
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
		if m.Store != nil {
			msg.today, _ = m.Store.AggregatesForToday(ctx)
			since := time.Now().Add(-7 * 24 * time.Hour)
			msg.topSess, _ = m.Store.TopSessionsByCost(ctx, 5, since)
			msg.topProj, _ = m.Store.TopProjectsByCost(ctx, 5, since)
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, refreshCmd(m)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		return m, tea.Batch(refreshCmd(m), tickCmd())
	case refreshMsg:
		m.Today = msg.today
		m.TopSess = msg.topSess
		m.TopProj = msg.topProj
		m.Snap = msg.snap
		m.UsageErr = msg.usageErr
		m.ActiveTask = msg.activeTask
	}
	return m, nil
}
