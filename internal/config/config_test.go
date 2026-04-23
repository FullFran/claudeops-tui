package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- ExportSettings and ClaudeOTelSettings tests ---

func TestExportSettingsLoad(t *testing.T) {
	tests := []struct {
		name  string
		toml  string
		check func(t *testing.T, s Settings)
	}{
		{
			name: "full export section",
			toml: `
[export]
enabled = true
user_name = "alice"
team_name = "eng"
endpoint = "https://otel.example.com/v1/metrics"

[export.headers]
Authorization = "Bearer tok"

[export.claude_otel]
enabled = true
include_user_prompts = true
include_tool_details = false
`,
			check: func(t *testing.T, s Settings) {
				t.Helper()
				if !s.Export.Enabled {
					t.Error("Enabled should be true")
				}
				if s.Export.UserName != "alice" {
					t.Errorf("UserName: want alice got %q", s.Export.UserName)
				}
				if s.Export.TeamName != "eng" {
					t.Errorf("TeamName: want eng got %q", s.Export.TeamName)
				}
				if s.Export.Endpoint != "https://otel.example.com/v1/metrics" {
					t.Errorf("Endpoint: got %q", s.Export.Endpoint)
				}
				if s.Export.Headers["Authorization"] != "Bearer tok" {
					t.Errorf("Headers: got %v", s.Export.Headers)
				}
				if !s.Export.ClaudeOTel.Enabled {
					t.Error("ClaudeOTel.Enabled should be true")
				}
				if !s.Export.ClaudeOTel.IncludeUserPrompts {
					t.Error("IncludeUserPrompts should be true")
				}
				if s.Export.ClaudeOTel.IncludeToolDetails {
					t.Error("IncludeToolDetails should be false")
				}
			},
		},
		{
			name: "no export section yields zero defaults",
			toml: `[dashboard]
show_today = true
`,
			check: func(t *testing.T, s Settings) {
				t.Helper()
				if s.Export.Enabled {
					t.Error("Enabled should be false")
				}
				if s.Export.UserName != "" || s.Export.TeamName != "" || s.Export.Endpoint != "" {
					t.Errorf("strings should be empty: %+v", s.Export)
				}
				if s.Export.ClaudeOTel.Enabled {
					t.Error("ClaudeOTel.Enabled should be false")
				}
			},
		},
		{
			name: "partial export section — only enabled=true",
			toml: `[export]
enabled = true
`,
			check: func(t *testing.T, s Settings) {
				t.Helper()
				if !s.Export.Enabled {
					t.Error("Enabled should be true")
				}
				if s.Export.Endpoint != "" {
					t.Errorf("Endpoint should be empty, got %q", s.Export.Endpoint)
				}
				if s.Export.ClaudeOTel.Enabled {
					t.Error("ClaudeOTel.Enabled should default to false")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.toml), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			tc.check(t, s)
		})
	}
}

func TestExportSettingsSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s := DefaultSettings()
	s.Export.Enabled = true
	s.Export.UserName = "bob"
	s.Export.TeamName = "ops"
	s.Export.Endpoint = "https://otel.example.com/v1/metrics"
	s.Export.Headers = map[string]string{"X-Token": "abc123"}
	s.Export.ClaudeOTel.Enabled = true
	s.Export.ClaudeOTel.IncludeUserPrompts = true
	s.Export.ClaudeOTel.IncludeToolDetails = true

	if err := Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s2, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s2.Export.Enabled != s.Export.Enabled {
		t.Errorf("Enabled mismatch")
	}
	if s2.Export.UserName != s.Export.UserName {
		t.Errorf("UserName: want %q got %q", s.Export.UserName, s2.Export.UserName)
	}
	if s2.Export.Endpoint != s.Export.Endpoint {
		t.Errorf("Endpoint mismatch")
	}
	if s2.Export.Headers["X-Token"] != "abc123" {
		t.Errorf("Headers mismatch: %v", s2.Export.Headers)
	}
	if s2.Export.ClaudeOTel.Enabled != s.Export.ClaudeOTel.Enabled {
		t.Errorf("ClaudeOTel.Enabled mismatch")
	}
	if s2.Export.ClaudeOTel.IncludeUserPrompts != s.Export.ClaudeOTel.IncludeUserPrompts {
		t.Errorf("IncludeUserPrompts mismatch")
	}
}

func TestExportSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		export  ExportSettings
		wantErr string // substring in error, or "" for nil
	}{
		{
			name:    "enabled with valid endpoint — ok",
			export:  ExportSettings{Enabled: true, Endpoint: "https://otel.example.com/v1/metrics", Headers: map[string]string{}},
			wantErr: "",
		},
		{
			name:    "enabled with empty endpoint — error",
			export:  ExportSettings{Enabled: true, Endpoint: "", Headers: map[string]string{}},
			wantErr: "endpoint",
		},
		{
			name:    "enabled with non-url endpoint — error",
			export:  ExportSettings{Enabled: true, Endpoint: "not-a-url", Headers: map[string]string{}},
			wantErr: "endpoint",
		},
		{
			name:    "disabled — nil regardless",
			export:  ExportSettings{Enabled: false, Endpoint: "", Headers: map[string]string{}},
			wantErr: "",
		},
		{
			name: "claude_otel enabled but export disabled — error",
			export: ExportSettings{
				Enabled:    false,
				Endpoint:   "",
				Headers:    map[string]string{},
				ClaudeOTel: ClaudeOTelSettings{Enabled: true},
			},
			wantErr: "claude_otel requires",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.export.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("want nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

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
	// Re-load round-trip: spot-check key fields.
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Dashboard != s.Dashboard || s2.Tabs != s.Tabs || s2.Calendar != s.Calendar {
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
