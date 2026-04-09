package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Settings is the user-editable configuration loaded from
// ~/.claudeops/config.toml. It controls which dashboard widgets are visible,
// thresholds for color-coding, calendar defaults, and key bindings.
//
// Adding a new field is safe: missing keys fall back to the default value
// from DefaultSettings(). Removing a field is also safe — unknown keys in
// the user's file are ignored on load.
type Settings struct {
	Dashboard   DashboardSettings   `toml:"dashboard"`
	Tabs        TabSettings         `toml:"tabs"`
	Calendar    CalendarSettings    `toml:"calendar"`
	Keybindings KeybindingsSettings `toml:"keybindings"`
	Usage       UsageSettings       `toml:"usage"`
	Insights    InsightsSettings    `toml:"insights"`
}

// UsageSettings controls how often the Anthropic usage endpoint is polled.
// The endpoint is undocumented and shared with Claude Code, so conservative
// defaults help avoid HTTP 429 responses.
type UsageSettings struct {
	CacheTTLSeconds int `toml:"cache_ttl_seconds"`
}

// DashboardSettings toggles individual widgets on the main dashboard tab.
// Each Show* field is consulted by views_dashboard.go before rendering.
type DashboardSettings struct {
	ShowSubscription   bool                `toml:"show_subscription"`
	ShowToday          bool                `toml:"show_today"`
	ShowTopSessions    bool                `toml:"show_top_sessions"`
	ShowTopProjects    bool                `toml:"show_top_projects"`
	ShowActiveTask     bool                `toml:"show_active_task"`
	ShowSparkline14d   bool                `toml:"show_sparkline_14d"`
	ShowPerModelToday  bool                `toml:"show_per_model_today"`
	ShowBurnRate       bool                `toml:"show_burn_rate"`
	ShowStreak         bool                `toml:"show_streak"`
	ShowAvgPerSession  bool                `toml:"show_avg_per_session"`
	ShowCacheHitRatio  bool                `toml:"show_cache_hit_ratio"`
	ShowTokensPerEuro  bool                `toml:"show_tokens_per_euro"`
	ShowMaxDay30d      bool                `toml:"show_max_day_30d"`
	ShowVsAvg7d        bool                `toml:"show_vs_avg_7d"`
	Thresholds         ThresholdsSettings  `toml:"thresholds"`
}

// ThresholdsSettings sets the cutoffs for color-coding daily spend.
// Values are in EUR.
type ThresholdsSettings struct {
	DailyWarnEUR  float64 `toml:"daily_warn_eur"`
	DailyAlertEUR float64 `toml:"daily_alert_eur"`
}

// TabSettings toggles entire tabs on/off. A disabled tab is hidden from the
// tab bar and unreachable via number keys. Dashboard cannot be disabled.
type TabSettings struct {
	Calendar bool `toml:"calendar"`
	Sessions bool `toml:"sessions"`
	Projects bool `toml:"projects"`
	Models   bool `toml:"models"`
	Tasks    bool `toml:"tasks"`
	Insights bool `toml:"insights"`
}

// InsightsSettings toggles individual insight cards on the Insights tab.
type InsightsSettings struct {
	ShowCacheEfficiency   bool `toml:"show_cache_efficiency"`
	ShowModelMix          bool `toml:"show_model_mix"`
	ShowCostTrend         bool `toml:"show_cost_trend"`
	ShowSessionEfficiency bool `toml:"show_session_efficiency"`
	ShowPeakHours         bool `toml:"show_peak_hours"`
}

// CalendarSettings configures the calendar tab (shipped in a follow-up PR).
type CalendarSettings struct {
	DefaultView  string `toml:"default_view"`  // "grid" | "timeline"
	Timezone     string `toml:"timezone"`      // "local" | "utc"
	TimelineDays int    `toml:"timeline_days"` // how many days to show in timeline mode
}

// KeybindingsSettings allows the user to override key bindings.
type KeybindingsSettings struct {
	CommandPalette string `toml:"command_palette"`
}

// DefaultSettings returns a Settings struct with all widgets enabled and
// sensible defaults. This is what gets written to disk on first run and what
// every missing field falls back to.
func DefaultSettings() Settings {
	return Settings{
		Dashboard: DashboardSettings{
			ShowSubscription:  true,
			ShowToday:         true,
			ShowTopSessions:   true,
			ShowTopProjects:   true,
			ShowActiveTask:    true,
			ShowSparkline14d:  true,
			ShowPerModelToday: true,
			ShowBurnRate:      true,
			ShowStreak:        true,
			ShowAvgPerSession: true,
			ShowCacheHitRatio: true,
			ShowTokensPerEuro: true,
			ShowMaxDay30d:     true,
			ShowVsAvg7d:       true,
			Thresholds: ThresholdsSettings{
				DailyWarnEUR:  20,
				DailyAlertEUR: 50,
			},
		},
		Tabs: TabSettings{
			Calendar: true,
			Sessions: true,
			Projects: true,
			Models:   true,
			Tasks:    true,
			Insights: true,
		},
		Insights: InsightsSettings{
			ShowCacheEfficiency:   true,
			ShowModelMix:          true,
			ShowCostTrend:         true,
			ShowSessionEfficiency: true,
			ShowPeakHours:         true,
		},
		Calendar: CalendarSettings{
			DefaultView:  "grid",
			Timezone:     "local",
			TimelineDays: 90,
		},
		Keybindings: KeybindingsSettings{
			CommandPalette: "ctrl+p",
		},
		Usage: UsageSettings{
			CacheTTLSeconds: 300,
		},
	}
}

// Load reads the config file at path and merges it onto the defaults.
// Missing files return DefaultSettings() with no error.
func Load(path string) (Settings, error) {
	s := DefaultSettings()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := toml.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("config %s: %w", path, err)
	}
	return s, nil
}

// LoadOrCreate reads the config; if missing, writes the defaults to disk
// (with a friendly header comment) and returns them. This is what the TUI
// calls at startup so that first-time users get a discoverable file they
// can edit.
func LoadOrCreate(path string) (Settings, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		s := DefaultSettings()
		if err := writeWithHeader(path, s); err != nil {
			return s, err
		}
		return s, nil
	}
	return Load(path)
}

// Save writes the current settings to disk, atomically.
func Save(path string, s Settings) error {
	return writeWithHeader(path, s)
}

func writeWithHeader(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	header := `# claudeops configuration
# This file is managed by claudeops but safe to edit by hand.
# Any field you delete will fall back to the built-in default on next load.
# Re-run claudeops to regenerate a missing file.

`
	if _, err := tmp.WriteString(header); err != nil {
		_ = tmp.Close()
		return err
	}
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(s); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
