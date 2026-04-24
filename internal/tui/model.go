// Package tui implements the Bubbletea dashboard with multiple tabs.
package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/export"
	"github.com/fullfran/claudeops-tui/internal/insights"
	"github.com/fullfran/claudeops-tui/internal/live"
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
	ConfigPath     string // path to config.toml for live saves
	PricingUpdated string
	Version        string
	ProjectsRoot   string // ~/.claude/projects — scanned for live sessions
	LiveDir        string // ~/.claudeops/live — hook-written session sidecars

	// snapshot
	Today                store.Aggregates
	Last7d               store.Aggregates
	TopSess              []store.SessionAgg
	AllSess              []store.SessionAgg
	TopProj              []store.ProjectAgg
	AllProj              []store.ProjectAgg
	PerModel             []store.ModelAgg
	AllTasks             []store.TaskAgg
	Daily                []store.DailyAgg // last 30 days, local TZ
	PerModelToday        []store.ModelAgg // per-model breakdown for today only
	BurnRate4h           float64          // €/hour over the last 4 hours
	Snap                 *usage.Snapshot
	UsageErr             string
	WeeklyCycleLocal     store.Aggregates
	WeeklyCycleStart     time.Time
	WeeklyCycleEnd       time.Time
	HasWeeklyCycleWindow bool
	ActiveTask           *tasks.Task
	HourlyGlobal         []store.HourlyAgg  // global per-hour aggregates for insights
	Insights             []insights.Insight // computed insights
	LiveSessions         []live.Session     // active Claude Code sessions for the Classroom tab

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

	// settings tab state
	settingsCursor   int // selected row in the settings list
	settingsInput    textinput.Model
	settingsEditOpen bool
	settingsSetStr   func(*config.Settings, string) // active setter for string edit

	// day drill-down state
	viewMode  viewMode
	dayCursor int        // index into m.Daily
	dayDetail *DayDetail // loaded on drill-down

	// session drill-down state
	sessCursor int            // index into m.AllSess
	sessDetail *SessionDetail // loaded on drill-down
}

// viewMode tracks the drill-down level within a tab.
type viewMode int

const (
	viewNormal        viewMode = iota
	viewDayBrowse              // list of days with cursor
	viewDayDetail              // single day breakdown
	viewSessionBrowse          // list of sessions with cursor
	viewSessionDetail          // single session breakdown
)

// DayDetail holds the loaded data for a single-day drill-down.
type DayDetail struct {
	Date     time.Time
	Agg      store.DailyAgg
	Sessions []store.SessionAgg
	Models   []store.ModelAgg
	Hourly   []store.HourlyAgg
}

// SessionDetail holds the loaded data for a single-session drill-down.
type SessionDetail struct {
	SessionID string
	Agg       store.SessionAgg
	Models    []store.ModelAgg
	Hourly    []store.HourlyAgg
}

// sessionDetailMsg is the result of loading drill-down data for a single session.
type sessionDetailMsg struct {
	detail *SessionDetail
	err    error
}

func loadSessionDetailCmd(s *store.Store, sess store.SessionAgg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		d := &SessionDetail{SessionID: sess.SessionID, Agg: sess}
		var err error
		d.Models, err = s.ModelsForSession(ctx, sess.SessionID)
		if err != nil {
			return sessionDetailMsg{err: err}
		}
		d.Hourly, err = s.HourlyForSession(ctx, sess.SessionID)
		if err != nil {
			return sessionDetailMsg{err: err}
		}
		return sessionDetailMsg{detail: d}
	}
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

	si := textinput.New()
	si.CharLimit = 200
	si.Width = 50

	return Model{
		Store:          s,
		Usage:          u,
		Tasks:          tr,
		Settings:       settings,
		PricingUpdated: pricingUpdated,
		Version:        version,
		activeTab:      TabDashboard,
		taskInput:      ti,
		settingsInput:  si,
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

// exportPushMsg is the result of a claudeops push triggered from Settings.
type exportPushMsg struct {
	result export.PushResult
	err    error
}

// otelApplyMsg is the result of otel-config apply triggered from Settings.
type otelApplyMsg struct {
	err error
}

func (m Model) pushNowCmd() tea.Cmd {
	return func() tea.Msg {
		if m.Store == nil {
			return exportPushMsg{err: fmt.Errorf("store not available")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		home, _ := os.UserHomeDir()
		credPath := filepath.Join(home, ".claude", ".credentials.json")
		pusher := export.New(m.Store, m.Settings.Export,
			export.NewFileCredReader(credPath),
			&http.Client{Timeout: 30 * time.Second},
			nil)
		result, err := pusher.Push(ctx, export.PushOptions{})
		return exportPushMsg{result: result, err: err}
	}
}

func (m Model) applyOTelCmd() tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		settingsPath := filepath.Join(home, ".claude", "settings.json")
		err := export.ApplyOTelConfig(settingsPath, m.Settings.Export)
		return otelApplyMsg{err: err}
	}
}

// dayDetailMsg is the result of loading drill-down data for a single day.
type dayDetailMsg struct {
	detail *DayDetail
	err    error
}

func loadDayDetailCmd(s *store.Store, day time.Time, agg store.DailyAgg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		d := &DayDetail{Date: day, Agg: agg}
		var err error
		d.Sessions, err = s.SessionsForDay(ctx, day)
		if err != nil {
			return dayDetailMsg{err: err}
		}
		d.Models, err = s.ModelsForDay(ctx, day)
		if err != nil {
			return dayDetailMsg{err: err}
		}
		d.Hourly, err = s.HourlyForDay(ctx, day)
		if err != nil {
			return dayDetailMsg{err: err}
		}
		return dayDetailMsg{detail: d}
	}
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
	today                store.Aggregates
	last7d               store.Aggregates
	topSess              []store.SessionAgg
	allSess              []store.SessionAgg
	topProj              []store.ProjectAgg
	allProj              []store.ProjectAgg
	perModel             []store.ModelAgg
	allTasks             []store.TaskAgg
	daily                []store.DailyAgg
	perModelToday        []store.ModelAgg
	burnRate4h           float64
	snap                 *usage.Snapshot
	usageErr             string
	weeklyCycleLocal     store.Aggregates
	weeklyCycleStart     time.Time
	weeklyCycleEnd       time.Time
	hasWeeklyCycleWindow bool
	activeTask           *tasks.Task
	hourlyGlobal         []store.HourlyAgg
	computedInsights     []insights.Insight
	liveSessions         []live.Session
}

func currentSevenDayCycleWindow(snap *usage.Snapshot) (time.Time, time.Time, bool) {
	if snap == nil || snap.SevenDay == nil || snap.SevenDay.ResetsAt.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	end := snap.SevenDay.ResetsAt.UTC()
	start := end.Add(-7 * 24 * time.Hour)
	if snap.FetchedAt.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	asOf := snap.FetchedAt.UTC()
	if !asOf.After(start) {
		return time.Time{}, time.Time{}, false
	}
	if asOf.After(end) {
		asOf = end
	}
	return start, asOf, true
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
			msg.hourlyGlobal, _ = m.Store.GlobalHourlyAggregates(ctx, since7d)
			msg.computedInsights = insights.Compute(insights.Input{
				Last7d:       msg.last7d,
				PerModel:     msg.perModel,
				Daily:        msg.daily,
				Sessions:     msg.allSess,
				HourlyGlobal: msg.hourlyGlobal,
			})
		}
		if m.Usage != nil && m.Settings.Dashboard.ShowSubscription {
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
				if m.Store != nil {
					if from, to, ok := currentSevenDayCycleWindow(&snap); ok {
						if agg, err := m.Store.AggregatesBetween(ctx, from, to); err == nil {
							msg.weeklyCycleLocal = agg
							msg.weeklyCycleStart = from
							msg.weeklyCycleEnd = snap.SevenDay.ResetsAt.UTC()
							msg.hasWeeklyCycleWindow = true
						}
					}
				}
			}
		}
		if m.Tasks != nil {
			if t, ok := m.Tasks.Current(); ok {
				msg.activeTask = t
			}
		}
		if m.ProjectsRoot != "" || m.LiveDir != "" {
			if sessions, err := live.Scan(m.ProjectsRoot, live.Config{LiveDir: m.LiveDir}); err == nil {
				msg.liveSessions = sessions
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
		// Settings string edit modal.
		if m.settingsEditOpen {
			switch msg.String() {
			case "esc", "ctrl+c":
				m.settingsEditOpen = false
				m.settingsInput.Blur()
				m.settingsInput.SetValue("")
				m.settingsSetStr = nil
				return m, nil
			case "enter":
				val := strings.TrimSpace(m.settingsInput.Value())
				m.settingsEditOpen = false
				m.settingsInput.Blur()
				m.settingsInput.SetValue("")
				if m.settingsSetStr != nil {
					m.settingsSetStr(&m.Settings, val)
					m.settingsSetStr = nil
					if m.ConfigPath != "" {
						if err := config.Save(m.ConfigPath, m.Settings); err != nil {
							m.statusMsg = "config save: " + err.Error()
						} else {
							m.statusMsg = "saved config.toml"
						}
					}
				}
				m.refreshViewport()
				return m, nil
			}
			var ic tea.Cmd
			m.settingsInput, ic = m.settingsInput.Update(msg)
			return m, ic
		}
		// Help overlay: any key dismisses it.
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		// Drill-down views intercept navigation keys before normal handling.
		if m.viewMode != viewNormal {
			return m.updateDrillDown(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = true
			return m, nil
		case "esc":
			// No-op at top level.
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
		case "j", "down":
			if m.activeTab == TabSettings {
				m.moveSettingsCursor(1)
				m.refreshViewport()
				return m, nil
			}
		case "k", "up":
			if m.activeTab == TabSettings {
				m.moveSettingsCursor(-1)
				m.refreshViewport()
				return m, nil
			}
		case "r":
			cmds = append(cmds, refreshCmd(m))
		case "tab", "right", "l":
			m.activeTab = (m.activeTab + 1) % tabCount
			m.viewMode = viewNormal
			m.refreshViewport()
		case "shift+tab", "left", "h":
			m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
			m.viewMode = viewNormal
			m.refreshViewport()
		case "1":
			m.activeTab = TabDashboard
			m.viewMode = viewNormal
			m.refreshViewport()
		case "2":
			m.activeTab = TabSessions
			m.viewMode = viewNormal
			m.refreshViewport()
		case "3":
			m.activeTab = TabProjects
			m.viewMode = viewNormal
			m.refreshViewport()
		case "4":
			m.activeTab = TabModels
			m.viewMode = viewNormal
			m.refreshViewport()
		case "5":
			m.activeTab = TabTasks
			m.viewMode = viewNormal
			m.refreshViewport()
		case "6":
			m.activeTab = TabInsights
			m.viewMode = viewNormal
			m.refreshViewport()
		case "7":
			m.activeTab = TabClassroom
			m.viewMode = viewNormal
			m.refreshViewport()
		case "8":
			m.activeTab = TabSettings
			m.viewMode = viewNormal
			m.settingsCursor = 1 // skip first section header
			m.refreshViewport()
		case " ":
			if m.activeTab == TabSettings {
				items := settingsItems()
				if m.settingsCursor >= 0 && m.settingsCursor < len(items) &&
					items[m.settingsCursor].toggle != nil {
					m.toggleSettingsItem()
					m.refreshViewport()
				}
			}
		case "enter":
			if m.activeTab == TabSettings {
				items := settingsItems()
				if m.settingsCursor >= 0 && m.settingsCursor < len(items) {
					item := items[m.settingsCursor]
					switch {
					case item.toggle != nil:
						m.toggleSettingsItem()
						m.refreshViewport()
					case item.isString && item.setString != nil:
						m.settingsInput.Placeholder = item.label + "…"
						m.settingsInput.SetValue(item.getString(m.Settings))
						m.settingsInput.CursorEnd()
						m.settingsSetStr = item.setString
						m.settingsEditOpen = true
						m.settingsInput.Focus()
						return m, textinput.Blink
					case item.actionKey == "push_now":
						if !m.Settings.Export.Enabled {
							m.statusMsg = "export disabled — toggle Enabled in the Export Metrics section first"
							m.refreshViewport()
							return m, nil
						}
						if m.Settings.Export.Endpoint == "" {
							m.statusMsg = "no endpoint set — edit Endpoint in the Export Metrics section first"
							m.refreshViewport()
							return m, nil
						}
						m.statusMsg = "pushing…"
						return m, m.pushNowCmd()
					case item.actionKey == "apply_otel":
						if !m.Settings.Export.ClaudeOTel.Enabled {
							m.statusMsg = "claude_otel disabled — toggle Enabled in the Claude Code OTel section first"
							m.refreshViewport()
							return m, nil
						}
						if m.Settings.Export.Endpoint == "" {
							m.statusMsg = "no endpoint set — edit Endpoint in the Export Metrics section first"
							m.refreshViewport()
							return m, nil
						}
						m.statusMsg = "applying OTel config…"
						return m, m.applyOTelCmd()
					}
				}
			} else if m.activeTab == TabDashboard && len(m.Daily) > 0 {
				m.viewMode = viewDayBrowse
				m.dayCursor = len(m.Daily) - 1 // start on today
				m.refreshViewport()
			} else if m.activeTab == TabSessions && len(m.AllSess) > 0 {
				m.viewMode = viewSessionBrowse
				m.sessCursor = 0
				m.refreshViewport()
			}
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
	case dayDetailMsg:
		if msg.err != nil {
			m.statusMsg = "day detail: " + msg.err.Error()
			m.viewMode = viewDayBrowse
		} else {
			m.dayDetail = msg.detail
			m.viewMode = viewDayDetail
		}
		m.refreshViewport()
	case sessionDetailMsg:
		if msg.err != nil {
			m.statusMsg = "session detail: " + msg.err.Error()
			m.viewMode = viewSessionBrowse
		} else {
			m.sessDetail = msg.detail
			m.viewMode = viewSessionDetail
		}
		m.refreshViewport()
	case taskActionMsg:
		if msg.err != nil {
			m.statusMsg = "task error: " + msg.err.Error()
		} else {
			m.statusMsg = msg.status
		}
		cmds = append(cmds, refreshCmd(m))
	case exportPushMsg:
		if msg.err != nil {
			m.statusMsg = "push failed: " + msg.err.Error()
		} else {
			m.statusMsg = fmt.Sprintf("pushed %d data points  %s → %s",
				msg.result.DataPoints,
				msg.result.PeriodFrom.Format("Jan 02 15:04"),
				msg.result.PeriodTo.Format("Jan 02 15:04"))
		}
		m.refreshViewport()
	case otelApplyMsg:
		if msg.err != nil {
			m.statusMsg = "otel-config failed: " + msg.err.Error()
		} else {
			m.statusMsg = "OTel config written to ~/.claude/settings.json"
		}
		m.refreshViewport()
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
		m.WeeklyCycleLocal = msg.weeklyCycleLocal
		m.WeeklyCycleStart = msg.weeklyCycleStart
		m.WeeklyCycleEnd = msg.weeklyCycleEnd
		m.HasWeeklyCycleWindow = msg.hasWeeklyCycleWindow
		m.ActiveTask = msg.activeTask
		m.HourlyGlobal = msg.hourlyGlobal
		m.Insights = msg.computedInsights
		m.LiveSessions = msg.liveSessions
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

// isSettingsNonNav reports whether an item should be skipped during cursor navigation.
func isSettingsNonNav(item settingsItem) bool {
	return item.section || item.skip
}

// moveSettingsCursor moves the cursor by delta, skipping non-navigable rows.
func (m *Model) moveSettingsCursor(delta int) {
	items := settingsItems()
	m.settingsCursor += delta
	for m.settingsCursor >= 0 && m.settingsCursor < len(items) && isSettingsNonNav(items[m.settingsCursor]) {
		m.settingsCursor += delta
	}
	if m.settingsCursor < 0 {
		m.settingsCursor = 0
		for m.settingsCursor < len(items) && isSettingsNonNav(items[m.settingsCursor]) {
			m.settingsCursor++
		}
	}
	if m.settingsCursor >= len(items) {
		m.settingsCursor = len(items) - 1
		for m.settingsCursor >= 0 && isSettingsNonNav(items[m.settingsCursor]) {
			m.settingsCursor--
		}
	}
}

// updateDrillDown handles keys when in day browse or day detail mode.
func (m Model) updateDrillDown(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.viewMode {
	case viewDayBrowse:
		switch msg.String() {
		case "esc", "q":
			m.viewMode = viewNormal
			m.refreshViewport()
			return m, nil
		case "j", "down":
			// List renders newest-first, so down = older = lower index.
			if m.dayCursor > 0 {
				m.dayCursor--
			}
			m.refreshViewport()
			return m, nil
		case "k", "up":
			// Up = newer = higher index.
			if m.dayCursor < len(m.Daily)-1 {
				m.dayCursor++
			}
			m.refreshViewport()
			return m, nil
		case "enter":
			if m.dayCursor >= 0 && m.dayCursor < len(m.Daily) && m.Store != nil {
				day := m.Daily[m.dayCursor]
				return m, loadDayDetailCmd(m.Store, day.Date, day)
			}
		}
	case viewDayDetail:
		switch msg.String() {
		case "esc", "q":
			m.viewMode = viewDayBrowse
			m.dayDetail = nil
			m.refreshViewport()
			return m, nil
		}
	case viewSessionBrowse:
		switch msg.String() {
		case "esc", "q":
			m.viewMode = viewNormal
			m.refreshViewport()
			return m, nil
		case "j", "down":
			if m.sessCursor < len(m.AllSess)-1 {
				m.sessCursor++
			}
			m.refreshViewport()
			return m, nil
		case "k", "up":
			if m.sessCursor > 0 {
				m.sessCursor--
			}
			m.refreshViewport()
			return m, nil
		case "enter":
			if m.sessCursor >= 0 && m.sessCursor < len(m.AllSess) && m.Store != nil {
				return m, loadSessionDetailCmd(m.Store, m.AllSess[m.sessCursor])
			}
		}
	case viewSessionDetail:
		switch msg.String() {
		case "esc", "q":
			m.viewMode = viewSessionBrowse
			m.sessDetail = nil
			m.refreshViewport()
			return m, nil
		}
	}
	// Forward scroll keys to viewport.
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, vpCmd
}

// refreshViewport regenerates the viewport content for the active tab.
// Called whenever data or tab changes. Every tab — including Dashboard —
// renders into the viewport so content longer than the terminal scrolls.
func (m *Model) refreshViewport() {
	if !m.ready {
		return
	}
	prevOffset := m.viewport.YOffset
	var content string
	// Drill-down views override normal tab rendering.
	if m.viewMode == viewDayBrowse {
		content = renderDayBrowse(*m)
		m.viewport.SetContent(content)
		m.scrollCursorIntoView(content, prevOffset)
		return
	}
	if m.viewMode == viewDayDetail && m.dayDetail != nil {
		content = renderDayDetail(*m)
		m.viewport.SetContent(content)
		return
	}
	if m.viewMode == viewSessionBrowse {
		content = renderSessionBrowse(*m)
		m.viewport.SetContent(content)
		m.scrollCursorIntoView(content, prevOffset)
		return
	}
	if m.viewMode == viewSessionDetail {
		content = renderSessionDetail(*m)
		m.viewport.SetContent(content)
		return
	}
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
	case TabInsights:
		content = renderInsightsTab(*m)
	case TabClassroom:
		content = renderClassroomTab(*m)
	case TabSettings:
		content = renderSettingsTab(*m)
	}
	m.viewport.SetContent(content)
	if m.activeTab == TabSettings {
		m.scrollCursorIntoView(content, prevOffset)
	}
}

// cursorLineMarker is an invisible zero-width space injected at the start of
// cursor lines by render functions. Used to find the cursor line reliably
// without false matches on breadcrumbs or other content.
const cursorLineMarker = "\u200B"

// scrollCursorIntoView finds the cursor line marker in the rendered content
// and adjusts the viewport offset to keep that line visible. Works for all
// views that inject cursorLineMarker (Settings, Day Browse, Session Browse).
// prevOffset is the YOffset before SetContent was called.
func (m *Model) scrollCursorIntoView(content string, prevOffset int) {
	lines := strings.Split(content, "\n")
	cursorLine := -1
	for i, line := range lines {
		if strings.HasPrefix(line, cursorLineMarker) {
			cursorLine = i
			break
		}
	}
	if cursorLine < 0 {
		m.viewport.SetYOffset(prevOffset)
		return
	}
	vpH := m.viewport.Height
	if cursorLine >= prevOffset && cursorLine < prevOffset+vpH {
		m.viewport.SetYOffset(prevOffset)
		return
	}
	// Scroll so cursor is near the top third of the viewport.
	target := cursorLine - vpH/3
	if target < 0 {
		target = 0
	}
	m.viewport.SetYOffset(target)
}

// toggleSettingsItem flips the bool at settingsCursor and persists to disk.
func (m *Model) toggleSettingsItem() {
	items := settingsItems()
	if m.settingsCursor < 0 || m.settingsCursor >= len(items) {
		return
	}
	item := items[m.settingsCursor]
	if item.section || item.toggle == nil {
		return
	}
	item.toggle(&m.Settings)
	if m.ConfigPath != "" {
		if err := config.Save(m.ConfigPath, m.Settings); err != nil {
			m.statusMsg = "config save: " + err.Error()
		} else {
			m.statusMsg = "saved config.toml"
		}
	}
}
