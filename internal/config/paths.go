// Package config resolves filesystem paths used by claudeops.
package config

import (
	"os"
	"path/filepath"
)

// Paths bundles every directory and file claudeops reads or writes.
type Paths struct {
	Home            string // user home
	ClaudeDir       string // ~/.claude
	ClaudeProjects  string // ~/.claude/projects
	ClaudeCreds     string // ~/.claude/.credentials.json
	ClaudeSettings  string // ~/.claude/settings.json
	DataDir         string // ~/.claudeops
	DBPath          string // ~/.claudeops/claudeops.db
	PricingPath     string // ~/.claudeops/pricing.toml
	CurrentTaskPath string // ~/.claudeops/current-task.json
	ConfigPath      string // ~/.claudeops/config.toml
	LiveDir         string // ~/.claudeops/live — hook-written session sidecars
	// SnapshotCachePath is the Anthropic quota snapshot, shared by every
	// process so the endpoint is polled once no matter how many consumers run.
	// Owned by usage.Client.
	SnapshotCachePath string // ~/.claudeops/snapshot-cache.json
	// UsageCachePath is the status line's own cache: registry providers, plus
	// the last good snapshot to fall back on when a refresh fails.
	UsageCachePath string // ~/.claudeops/usage-cache.json
}

// Default builds Paths from the user's HOME (or HOME override for tests).
func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return ForHome(home), nil
}

// ForHome builds Paths rooted at the given home directory. Useful in tests.
func ForHome(home string) Paths {
	claude := filepath.Join(home, ".claude")
	data := filepath.Join(home, ".claudeops")
	return Paths{
		Home:              home,
		ClaudeDir:         claude,
		ClaudeProjects:    filepath.Join(claude, "projects"),
		ClaudeCreds:       filepath.Join(claude, ".credentials.json"),
		ClaudeSettings:    filepath.Join(claude, "settings.json"),
		DataDir:           data,
		DBPath:            filepath.Join(data, "claudeops.db"),
		PricingPath:       filepath.Join(data, "pricing.toml"),
		CurrentTaskPath:   filepath.Join(data, "current-task.json"),
		ConfigPath:        filepath.Join(data, "config.toml"),
		LiveDir:           filepath.Join(data, "live"),
		SnapshotCachePath: filepath.Join(data, "snapshot-cache.json"),
		UsageCachePath:    filepath.Join(data, "usage-cache.json"),
	}
}

// EnsureDataDir creates ~/.claudeops if missing, with mode 0700.
func (p Paths) EnsureDataDir() error {
	return os.MkdirAll(p.DataDir, 0o700)
}
