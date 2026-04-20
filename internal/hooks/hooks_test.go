package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCreatesAllManagedEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := Install(path, "/usr/local/bin/claudeops"); err != nil {
		t.Fatalf("install: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, ev := range ManagedEvents {
		groups := s.Hooks[ev]
		if len(groups) != 1 {
			t.Fatalf("%s: want 1 group, got %d", ev, len(groups))
		}
		if len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Source != SourceMarker {
			t.Fatalf("%s: missing claudeops-tagged entry: %+v", ev, groups[0])
		}
		if !strings.Contains(groups[0].Hooks[0].Command, "hooks handle") {
			t.Fatalf("%s: bad command %q", ev, groups[0].Hooks[0].Command)
		}
	}
}

func TestInstallPreservesUserHooksAndExtras(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Pre-existing user config with unrelated hooks + unknown keys.
	initial := `{
		"model": "claude-opus-4-7",
		"hooks": {
			"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "user-script.sh"}]}],
			"SessionStart": [{"matcher": "", "hooks": [{"type": "command", "command": "user-start.sh"}]}]
		}
	}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Install(path, "/bin/claudeops"); err != nil {
		t.Fatalf("install: %v", err)
	}

	data, _ := os.ReadFile(path)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if _, ok := raw["model"]; !ok {
		t.Error("lost unknown top-level key 'model'")
	}

	s, _ := Load(path)
	if len(s.Hooks["PreToolUse"]) != 1 {
		t.Error("lost user PreToolUse hook")
	}
	// SessionStart should now have 2 groups: user's + ours.
	if len(s.Hooks["SessionStart"]) != 2 {
		t.Fatalf("SessionStart: want 2 groups, got %d", len(s.Hooks["SessionStart"]))
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	for i := 0; i < 3; i++ {
		if err := Install(path, "/bin/claudeops"); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
	}
	s, _ := Load(path)
	for _, ev := range ManagedEvents {
		ours := 0
		for _, g := range s.Hooks[ev] {
			for _, h := range g.Hooks {
				if h.Source == SourceMarker {
					ours++
				}
			}
		}
		if ours != 1 {
			t.Errorf("%s: want 1 claudeops entry after re-install, got %d", ev, ours)
		}
	}
}

func TestUninstallRemovesOnlyOurs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	initial := `{
		"hooks": {
			"SessionStart": [{"matcher": "", "hooks": [{"type": "command", "command": "user-start.sh"}]}]
		}
	}`
	os.WriteFile(path, []byte(initial), 0o600)

	if err := Install(path, "/bin/claudeops"); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(path); err != nil {
		t.Fatal(err)
	}
	s, _ := Load(path)
	if len(s.Hooks["SessionStart"]) != 1 {
		t.Fatalf("SessionStart: want 1 group after uninstall, got %d", len(s.Hooks["SessionStart"]))
	}
	if s.Hooks["SessionStart"][0].Hooks[0].Command != "user-start.sh" {
		t.Error("user hook not preserved")
	}
	// Events we added that didn't exist before must be gone.
	if _, has := s.Hooks["UserPromptSubmit"]; has {
		t.Error("UserPromptSubmit not pruned")
	}
}

func TestStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	binary := filepath.Join(dir, "claudeops")
	os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755)

	r, err := Status(path, binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range ManagedEvents {
		if r.Events[ev] {
			t.Errorf("%s: expected not installed", ev)
		}
	}
	if !r.BinaryExists {
		t.Error("BinaryExists should be true")
	}

	Install(path, binary)
	r, _ = Status(path, binary)
	for _, ev := range ManagedEvents {
		if !r.Events[ev] {
			t.Errorf("%s: expected installed after Install", ev)
		}
	}
}

func TestHandleWritesSidecar(t *testing.T) {
	dir := t.TempDir()
	liveDir := filepath.Join(dir, "live")

	cases := []struct {
		name      string
		event     string
		wantState string
	}{
		{"session-start", "SessionStart", "waiting"},
		{"user-prompt", "UserPromptSubmit", "working"},
		{"stop", "Stop", "waiting"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload := map[string]string{
				"session_id":      "abc-123",
				"cwd":             "/home/u/proj",
				"hook_event_name": c.event,
			}
			body, _ := json.Marshal(payload)
			if err := Handle(bytes.NewReader(body), liveDir); err != nil {
				t.Fatalf("handle: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(liveDir, "abc-123.json"))
			if err != nil {
				t.Fatalf("sidecar missing: %v", err)
			}
			var sc Sidecar
			json.Unmarshal(raw, &sc)
			if sc.State != c.wantState {
				t.Errorf("state: want %q got %q", c.wantState, sc.State)
			}
			if sc.ProjectPath != "/home/u/proj" {
				t.Errorf("project_path: got %q", sc.ProjectPath)
			}
		})
	}
}

func TestHandleSessionEndRemovesSidecar(t *testing.T) {
	dir := t.TempDir()
	liveDir := filepath.Join(dir, "live")

	os.MkdirAll(liveDir, 0o700)
	sidecarPath := filepath.Join(liveDir, "abc-123.json")
	os.WriteFile(sidecarPath, []byte("{}"), 0o600)

	payload := `{"session_id":"abc-123","hook_event_name":"SessionEnd"}`
	if err := Handle(strings.NewReader(payload), liveDir); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if _, err := os.Stat(sidecarPath); !os.IsNotExist(err) {
		t.Error("sidecar should have been deleted on SessionEnd")
	}
}

func TestHandleIgnoresEmptyOrBadInput(t *testing.T) {
	dir := t.TempDir()
	if err := Handle(strings.NewReader(""), dir); err != nil {
		t.Errorf("empty input: %v", err)
	}
	if err := Handle(strings.NewReader(`{"session_id":""}`), dir); err != nil {
		t.Errorf("no session_id: %v", err)
	}
}

func TestSaveBacksUpPreviousFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"model":"old"}`), 0o600)

	Install(path, "/bin/claudeops")

	entries, _ := os.ReadDir(dir)
	foundBackup := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.bak-") {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Error("expected timestamped backup file")
	}
}
