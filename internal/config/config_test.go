package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(filepath.Join(dir, "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Dashboard.ShowToday || !s.Tabs.Sessions {
		t.Errorf("missing file should yield defaults, got %+v", s)
	}
}

func TestLoadOrCreateWritesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Dashboard.ShowSparkline14d {
		t.Error("default should enable sparkline")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("file should have been created:", err)
	}
	body := string(b)
	if !strings.Contains(body, "[dashboard]") || !strings.Contains(body, "show_sparkline_14d") {
		t.Errorf("written file missing expected sections:\n%s", body)
	}
	// Re-load round-trip should be a no-op.
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2 != s {
		t.Errorf("round-trip mismatch:\nwant %+v\ngot  %+v", s, s2)
	}
}

func TestLoadPartialOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// User only sets one toggle off; everything else should stay default.
	if err := os.WriteFile(path, []byte(`
[dashboard]
show_sparkline_14d = false

[dashboard.thresholds]
daily_warn_eur = 5
`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Dashboard.ShowSparkline14d {
		t.Error("explicit false should disable sparkline")
	}
	if !s.Dashboard.ShowToday {
		t.Error("unspecified toggles should remain default-true")
	}
	if s.Dashboard.Thresholds.DailyWarnEUR != 5 {
		t.Errorf("threshold override lost: %v", s.Dashboard.Thresholds.DailyWarnEUR)
	}
	if s.Dashboard.Thresholds.DailyAlertEUR != 50 {
		t.Errorf("unset threshold should keep default 50, got %v", s.Dashboard.Thresholds.DailyAlertEUR)
	}
}

func TestLoadInvalidTOMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	_ = os.WriteFile(path, []byte("this is = not = valid"), 0o600)
	if _, err := Load(path); err == nil {
		t.Error("expected parse error")
	}
}
