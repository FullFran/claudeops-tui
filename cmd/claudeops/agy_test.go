package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/agy"
	"github.com/fullfran/claudeops-tui/internal/config"
)

// fixtureAgyPayload is the documented example payload from
// https://antigravity.google/docs/cli/statusline, verbatim.
const fixtureAgyPayload = `{"cwd":"/home/user/my-project","session_id":"12345678-abcd-ef01-2345-6789abcdef01","conversation_id":"12345678-abcd-ef01-2345-6789abcdef01","transcript_path":"/home/user/.gemini/antigravity/brain/12345678-abcd-ef01-2345-6789abcdef01/.system_generated/logs/transcript.jsonl","model":{"id":"Gemini 3.5 Flash (High)","display_name":"Gemini 3.5 Flash (High)"},"workspace":{"current_dir":"/home/user/my-project","project_dir":"/home/user/my-project"},"version":"1.0.13","context_window":{"total_input_tokens":88244,"total_output_tokens":61074,"context_window_size":1048576,"used_percentage":14.24,"remaining_percentage":85.76,"current_usage":{"input_tokens":63382,"output_tokens":346,"cache_creation_input_tokens":0,"cache_read_input_tokens":20857}},"exceeds_200k_tokens":false,"product":"antigravity","quota":{"gemini-weekly":{"remaining_fraction":0.9378,"reset_time":"2099-07-06T07:50:32Z","reset_in_seconds":560580}},"agent_state":"idle","vcs":{"type":"git","branch":"main","dirty":false},"sandbox":{"enabled":false},"artifact_count":2,"plan_tier":"Pro","email":"developer@email.com","task_count":1,"terminal_width":111,"execution_mode":"planning"}`

func TestCmdAgyStatusline(t *testing.T) {
	t.Run("payload persists only allowed fields and prints a line", func(t *testing.T) {
		p := newTestPaths(t)
		var out bytes.Buffer
		if err := cmdAgyStatusline(p, strings.NewReader(fixtureAgyPayload), &out); err != nil {
			t.Fatalf("cmdAgyStatusline: %v", err)
		}
		raw, err := os.ReadFile(p.AntigravityQuotaPath)
		if err != nil {
			t.Fatalf("snapshot was not written: %v", err)
		}
		for _, forbidden := range []string{"developer@email.com", "email", "cwd", "transcript_path", "my-project"} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("persisted snapshot must not contain %q:\n%s", forbidden, raw)
			}
		}
		if !strings.Contains(out.String(), "gemini-weekly") {
			t.Errorf("stdout = %q, want the gemini-weekly bucket", out.String())
		}
		if !strings.Contains(out.String(), "6%") {
			t.Errorf("stdout = %q, want ~6%% used", out.String())
		}
	})

	t.Run("malformed stdin leaves an existing snapshot untouched and prints nothing", func(t *testing.T) {
		p := newTestPaths(t)
		if err := agy.WriteQuotaSnapshot(p.AntigravityQuotaPath, agy.QuotaSnapshot{PlanTier: "Pro"}); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(p.AntigravityQuotaPath)
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := cmdAgyStatusline(p, strings.NewReader("not json"), &out); err != nil {
			t.Fatalf("expected exit 0 for malformed stdin, got %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("stdout = %q, want empty", out.String())
		}
		after, err := os.ReadFile(p.AntigravityQuotaPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Errorf("snapshot changed on malformed stdin:\nbefore %s\nafter  %s", before, after)
		}
	})

	t.Run("empty stdin exits zero with no output and no snapshot", func(t *testing.T) {
		p := newTestPaths(t)
		var out bytes.Buffer
		if err := cmdAgyStatusline(p, strings.NewReader(""), &out); err != nil {
			t.Fatalf("expected exit 0 for empty stdin, got %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("stdout = %q, want empty", out.String())
		}
		if _, err := os.Stat(p.AntigravityQuotaPath); err == nil {
			t.Error("expected no snapshot file to have been written")
		}
	})

	t.Run("statusline disabled: no output but snapshot still written", func(t *testing.T) {
		p := newTestPaths(t)
		if err := p.EnsureDataDir(); err != nil {
			t.Fatal(err)
		}
		off := false
		settings := config.DefaultSettings()
		settings.Statusline.Enabled = &off
		if err := config.Save(p.ConfigPath, settings); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := cmdAgyStatusline(p, strings.NewReader(fixtureAgyPayload), &out); err != nil {
			t.Fatalf("cmdAgyStatusline: %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("stdout = %q, want empty while disabled", out.String())
		}
		if _, err := os.Stat(p.AntigravityQuotaPath); err != nil {
			t.Errorf("snapshot should still be written while disabled: %v", err)
		}
	})

	t.Run("no quota in payload falls back to the existing snapshot", func(t *testing.T) {
		p := newTestPaths(t)
		if err := agy.WriteQuotaSnapshot(p.AntigravityQuotaPath, agy.QuotaSnapshot{
			PlanTier: "Pro",
			Buckets: map[string]agy.QuotaBucket{
				"gemini-weekly": {RemainingFraction: 0.5},
			},
		}); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := cmdAgyStatusline(p, strings.NewReader(`{"plan_tier":"Pro","agent_state":"idle"}`), &out); err != nil {
			t.Fatalf("cmdAgyStatusline: %v", err)
		}
		if !strings.Contains(out.String(), "gemini-weekly") {
			t.Errorf("stdout = %q, want the previously persisted bucket", out.String())
		}
	})
}

func TestCmdAgySetup(t *testing.T) {
	t.Run("creates a missing settings.json with only statusLine", func(t *testing.T) {
		dir := t.TempDir()
		var out bytes.Buffer
		if err := cmdAgySetup(filepath.Join(dir, "settings.json"), &out, nil); err != nil {
			t.Fatalf("cmdAgySetup: %v", err)
		}
		s, _, err := agy.LoadSettings(filepath.Join(dir, "settings.json"))
		if err != nil {
			t.Fatalf("LoadSettings: %v", err)
		}
		if s.StatusLine == nil || s.StatusLine.Type != "command" || !strings.HasSuffix(s.StatusLine.Command, "agy statusline") {
			t.Errorf("StatusLine = %+v", s.StatusLine)
		}
		if len(s.Extra) != 0 {
			t.Errorf("Extra = %v, want empty for a freshly created file", s.Extra)
		}
	})

	t.Run("preserves other keys", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(`{"theme":"dark","padding":2}`), 0o644); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := cmdAgySetup(path, &out, nil); err != nil {
			t.Fatalf("cmdAgySetup: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"theme"`, `"dark"`, `"padding"`} {
			if !strings.Contains(string(raw), want) {
				t.Errorf("settings.json missing %s:\n%s", want, raw)
			}
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("mode = %v, want the pre-existing 0644", info.Mode().Perm())
		}
	})

	t.Run("refuses a foreign statusLine without --force", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"some-other-tool"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err := cmdAgySetup(path, &out, nil)
		if err == nil {
			t.Fatal("expected a refusal for a foreign statusLine")
		}
		if !strings.Contains(err.Error(), "some-other-tool") {
			t.Errorf("error should name the existing command: %v", err)
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !strings.Contains(string(raw), "some-other-tool") {
			t.Errorf("file must be left unchanged:\n%s", raw)
		}
	})

	t.Run("--force replaces a foreign statusLine", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"some-other-tool"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := cmdAgySetup(path, &out, []string{"--force"}); err != nil {
			t.Fatalf("cmdAgySetup --force: %v", err)
		}
		s, _, err := agy.LoadSettings(path)
		if err != nil {
			t.Fatal(err)
		}
		if s.StatusLine == nil || s.StatusLine.Command == "some-other-tool" {
			t.Errorf("StatusLine should have been replaced: %+v", s.StatusLine)
		}
	})

	t.Run("refuses an unparsable file and leaves it byte-identical", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		original := []byte("{not json")
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := cmdAgySetup(path, &out, nil); err == nil {
			t.Fatal("expected a refusal for an unparsable file")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(original) {
			t.Errorf("file was modified:\nwant %q\ngot  %q", original, got)
		}
	})

	t.Run("idempotent re-run", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		var out bytes.Buffer
		if err := cmdAgySetup(path, &out, nil); err != nil {
			t.Fatalf("first setup: %v", err)
		}
		first, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := cmdAgySetup(path, &out, nil); err != nil {
			t.Fatalf("second setup: %v", err)
		}
		second, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var a, b map[string]json.RawMessage
		if err := json.Unmarshal(first, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(second, &b); err != nil {
			t.Fatal(err)
		}
		if string(a["statusLine"]) != string(b["statusLine"]) {
			t.Errorf("re-run changed statusLine:\n%s\n%s", a["statusLine"], b["statusLine"])
		}
	})
}

func TestCmdAgyRemove(t *testing.T) {
	t.Run("removes only when it is ours", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		var out bytes.Buffer
		if err := cmdAgySetup(path, &out, nil); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := cmdAgyRemove(path, &out); err != nil {
			t.Fatalf("remove: %v", err)
		}
		s, _, err := agy.LoadSettings(path)
		if err != nil {
			t.Fatal(err)
		}
		if s.StatusLine != nil {
			t.Errorf("StatusLine = %+v, want nil after remove", s.StatusLine)
		}
	})

	t.Run("leaves a foreign statusLine alone", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"some-other-tool"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := cmdAgyRemove(path, &out); err != nil {
			t.Fatalf("remove: %v", err)
		}
		s, _, err := agy.LoadSettings(path)
		if err != nil {
			t.Fatal(err)
		}
		if s.StatusLine == nil || s.StatusLine.Command != "some-other-tool" {
			t.Errorf("a foreign statusLine must survive remove: %+v", s.StatusLine)
		}
	})

	t.Run("missing file is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		var out bytes.Buffer
		if err := cmdAgyRemove(path, &out); err != nil {
			t.Fatalf("remove: %v", err)
		}
		if _, err := os.Stat(path); err == nil {
			t.Error("remove on a missing file must not create one")
		}
	})
}

func TestCmdAgyStatus(t *testing.T) {
	t.Run("reports settings path, wiring and no quota yet", func(t *testing.T) {
		p := newTestPaths(t)
		dir := t.TempDir()
		settingsPath := filepath.Join(dir, "settings.json")
		var out bytes.Buffer
		if err := cmdAgyStatus(p, settingsPath, &out); err != nil {
			t.Fatalf("status: %v", err)
		}
		if !strings.Contains(out.String(), settingsPath) {
			t.Errorf("output missing settings path:\n%s", out.String())
		}
		if !strings.Contains(strings.ToLower(out.String()), "not") {
			t.Errorf("output should say the status line is not wired up:\n%s", out.String())
		}
		if !strings.Contains(out.String(), "none received yet") {
			t.Errorf("output missing quota-not-yet-received:\n%s", out.String())
		}
	})

	t.Run("reports wired and the last quota reading", func(t *testing.T) {
		p := newTestPaths(t)
		dir := t.TempDir()
		settingsPath := filepath.Join(dir, "settings.json")
		var setupOut bytes.Buffer
		if err := cmdAgySetup(settingsPath, &setupOut, nil); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := agy.WriteQuotaSnapshot(p.AntigravityQuotaPath, agy.QuotaSnapshot{
			PlanTier: "Pro",
			Buckets: map[string]agy.QuotaBucket{
				"gemini-weekly": {RemainingFraction: 0.9378},
			},
		}); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := cmdAgyStatus(p, settingsPath, &out); err != nil {
			t.Fatalf("status: %v", err)
		}
		if !strings.Contains(out.String(), "gemini-weekly") {
			t.Errorf("output missing bucket:\n%s", out.String())
		}
	})
}

func TestCmdAgyWith(t *testing.T) {
	t.Run("missing subcommand errors", func(t *testing.T) {
		if err := cmdAgyWith(newTestPaths(t), t.TempDir(), strings.NewReader(""), &bytes.Buffer{}, nil); err == nil {
			t.Fatal("expected an error for a missing subcommand")
		}
	})

	t.Run("unknown subcommand errors", func(t *testing.T) {
		if err := cmdAgyWith(newTestPaths(t), t.TempDir(), strings.NewReader(""), &bytes.Buffer{}, []string{"nope"}); err == nil {
			t.Fatal("expected an error for an unknown subcommand")
		}
	})

	t.Run("status routes through the agy root", func(t *testing.T) {
		p := newTestPaths(t)
		root := t.TempDir()
		var out bytes.Buffer
		if err := cmdAgyWith(p, root, strings.NewReader(""), &out, []string{"status"}); err != nil {
			t.Fatalf("agy status: %v", err)
		}
		if !strings.Contains(out.String(), filepath.Join(root, "settings.json")) {
			t.Errorf("output should name the settings path under the agy root:\n%s", out.String())
		}
	})
}
