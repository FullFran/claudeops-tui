package update

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// DefaultCacheTTL is how long an update check result is considered fresh (24h).
const DefaultCacheTTL = 24 * time.Hour

// CheckResult represents the outcome of an update check.
type CheckResult struct {
	CheckedAt      time.Time `json:"checked_at"`
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version"`
	UpToDate       bool      `json:"up_to_date"`
	Downgrade      bool      `json:"downgrade"`
	CanAuto        bool      `json:"can_auto"`
	Method         Method    `json:"method"`
	InstallCommand string    `json:"install_command"`
	Reason         string    `json:"reason,omitempty"`
}

// HasUpdate reports whether a newer version is published and available.
func (r CheckResult) HasUpdate() bool {
	return r.LatestVersion != "" && !r.UpToDate && !r.Downgrade
}

// ReadCache reads a cached CheckResult from disk.
func ReadCache(path string) (*CheckResult, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var res CheckResult
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// WriteCache writes a CheckResult to disk atomically.
func WriteCache(path string, res CheckResult) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Check resolves update status using DefaultCacheTTL.
func Check(ctx context.Context, u Updater, cachePath string) (CheckResult, error) {
	return CheckWithCache(ctx, u, cachePath, DefaultCacheTTL)
}

// CheckWithCache resolves update status, consulting and updating the on-disk cache.
// If ttl > 0, cachePath is non-empty, and the cache is fresh (age < ttl) with
// matching version, it returns the cached result without making network calls.
func CheckWithCache(ctx context.Context, u Updater, cachePath string, ttl time.Duration) (CheckResult, error) {
	if ttl > 0 && cachePath != "" {
		if cached, err := ReadCache(cachePath); err == nil && cached != nil {
			if cached.CurrentVersion == u.Version && time.Since(cached.CheckedAt) < ttl {
				return *cached, nil
			}
		}
	}

	decision, err := u.Decide(ctx)
	if err != nil {
		return CheckResult{}, err
	}

	res := CheckResult{
		CheckedAt:      time.Now(),
		CurrentVersion: u.Version,
		LatestVersion:  decision.LatestVersion,
		UpToDate:       decision.UpToDate,
		Downgrade:      decision.Downgrade,
		CanAuto:        decision.CanAuto,
		Method:         decision.Method,
		InstallCommand: decision.InstallCommand,
		Reason:         decision.Reason,
	}

	if cachePath != "" && decision.LatestVersion != "" {
		_ = WriteCache(cachePath, res)
	}

	return res, nil
}
