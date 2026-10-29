package pi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSafeCwd(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/home/fran/fullfran/ClaudeOps-TUI", "--home-fran-fullfran-ClaudeOps-TUI--"},
		{"/home/fran", "--home-fran--"},
		{"/tmp/test", "--tmp-test--"},
	}
	for _, tc := range cases {
		got := SafeCwd(tc.input)
		if got != tc.want {
			t.Errorf("SafeCwd(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestClassifyProvider(t *testing.T) {
	cases := []struct {
		rawProv     string
		model       string
		wantMapped  string
		wantSupp    bool
		wantUnavail bool
	}{
		{"anthropic", "claude-3-7-sonnet", "claude", true, false},
		{"claude", "claude-3-5-sonnet", "claude", true, false},
		{"agy", "gemini-3.8-flash", "antigravity", true, false},
		{"antigravity", "gemini-pro", "antigravity", true, false},
		{"gemini", "gemini-2.5", "gemini", true, false},
		{"openai", "gpt-6.1-sol", "openai", false, true},
		{"codex", "gpt-5-codex", "openai", false, true},
		{"random-llm", "foo", "random-llm", false, true},
	}

	for _, tc := range cases {
		res := ClassifyProvider(tc.rawProv, tc.model)
		if res.MappedName != tc.wantMapped {
			t.Errorf("ClassifyProvider(%q).MappedName = %q, want %q", tc.rawProv, res.MappedName, tc.wantMapped)
		}
		if res.Supported != tc.wantSupp {
			t.Errorf("ClassifyProvider(%q).Supported = %v, want %v", tc.rawProv, res.Supported, tc.wantSupp)
		}
		if res.Unavailable != tc.wantUnavail {
			t.Errorf("ClassifyProvider(%q).Unavailable = %v, want %v", tc.rawProv, res.Unavailable, tc.wantUnavail)
		}
	}
}

func TestResolveSessionProviderFromJSONL(t *testing.T) {
	tempAgent := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", tempAgent)

	cwd := "/test/workspace"
	sessionDir := filepath.Join(tempAgent, "sessions", SafeCwd(cwd))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create older session file with anthropic
	olderFile := filepath.Join(sessionDir, "2026-10-28T10-00-00-000Z_1.jsonl")
	contentOlder := `{"type":"session","cwd":"/test/workspace"}
{"type":"model_change","provider":"anthropic","modelId":"claude-3-5-sonnet"}
`
	if err := os.WriteFile(olderFile, []byte(contentOlder), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create newer session file where model was switched from openai to agy
	newerFile := filepath.Join(sessionDir, "2026-10-29T10-00-00-000Z_2.jsonl")
	contentNewer := `{"type":"session","cwd":"/test/workspace"}
{"type":"model_change","provider":"openai","modelId":"gpt-6.1-sol"}
{"type":"model_change","provider":"agy","modelId":"gemini-3.8-flash-medium"}
`
	if err := os.WriteFile(newerFile, []byte(contentNewer), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make sure newerFile has a later mtime
	_ = os.Chtimes(newerFile, time.Now().Add(time.Hour), time.Now().Add(time.Hour))

	res := ResolveSessionProvider(cwd)
	if res.RawProvider != "agy" {
		t.Errorf("RawProvider = %q, want %q", res.RawProvider, "agy")
	}
	if res.MappedName != "antigravity" {
		t.Errorf("MappedName = %q, want %q", res.MappedName, "antigravity")
	}
	if res.Unavailable {
		t.Errorf("expected agy to be available, got unavailable")
	}
}

func TestResolveSessionProviderOpenAIUnavailable(t *testing.T) {
	tempAgent := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", tempAgent)

	cwd := "/test/workspace/openai"
	sessionDir := filepath.Join(tempAgent, "sessions", SafeCwd(cwd))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionFile := filepath.Join(sessionDir, "session.jsonl")
	content := `{"type":"session","cwd":"/test/workspace/openai"}
{"type":"model_change","provider":"openai","modelId":"gpt-6.1-sol"}
`
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res := ResolveSessionProvider(cwd)
	if res.RawProvider != "openai" {
		t.Errorf("RawProvider = %q, want %q", res.RawProvider, "openai")
	}
	if !res.Unavailable {
		t.Errorf("expected openai in Pi to be marked unavailable, got %v", res.Unavailable)
	}
}

func TestResolveSessionProviderFallbackToSettings(t *testing.T) {
	tempAgent := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", tempAgent)

	settingsFile := filepath.Join(tempAgent, "settings.json")
	if err := os.WriteFile(settingsFile, []byte(`{"defaultProvider":"anthropic","defaultModel":"claude-3-7-sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// No session folder exists for this cwd
	res := ResolveSessionProvider("/some/unknown/cwd")
	if res.RawProvider != "anthropic" {
		t.Errorf("RawProvider = %q, want %q", res.RawProvider, "anthropic")
	}
	if res.MappedName != "claude" {
		t.Errorf("MappedName = %q, want %q", res.MappedName, "claude")
	}
	if res.Unavailable {
		t.Errorf("expected anthropic to be available")
	}
}
