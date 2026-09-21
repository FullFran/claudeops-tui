package agy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSettingsMissingFile(t *testing.T) {
	dir := t.TempDir()
	s, mode, err := LoadSettings(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if mode != 0 {
		t.Errorf("mode = %v, want 0 for a missing file", mode)
	}
	if s.StatusLine != nil {
		t.Errorf("StatusLine = %+v, want nil for a missing file", s.StatusLine)
	}
}

func TestLoadSettingsUnparsableFileRefuses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte("{not json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSettings(path); err == nil {
		t.Fatal("expected an error for unparsable settings.json")
	}
	// Byte-identical: LoadSettings must never touch the file on disk.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("file was modified:\nwant %q\ngot  %q", original, got)
	}
}

func TestLoadSettingsPreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body := `{"theme":"dark","statusLine":{"type":"command","command":"/usr/bin/claudeops agy statusline"},"padding":2}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, mode, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if mode.Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", mode.Perm())
	}
	if s.StatusLine == nil || s.StatusLine.Type != "command" || s.StatusLine.Command != "/usr/bin/claudeops agy statusline" {
		t.Fatalf("StatusLine = %+v", s.StatusLine)
	}
	if _, ok := s.Extra["theme"]; !ok {
		t.Error(`Extra["theme"] missing`)
	}
	if _, ok := s.Extra["padding"]; !ok {
		t.Error(`Extra["padding"] missing`)
	}

	// Round trip: saving must not drop the unrelated keys.
	if err := SaveSettings(path, s, mode); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"theme"`, `"dark"`, `"padding"`, `"statusLine"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("saved file missing %s:\n%s", want, out)
		}
	}
}

func TestSaveSettingsCreatesFileWithDefaultMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := &Settings{
		StatusLine: &StatusLineEntry{Type: "command", Command: "/usr/bin/claudeops agy statusline"},
		Extra:      map[string]json.RawMessage{},
	}
	if err := SaveSettings(path, s, 0); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 for a new file", info.Mode().Perm())
	}

	got, _, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got.StatusLine == nil || got.StatusLine.Command != "/usr/bin/claudeops agy statusline" {
		t.Errorf("round trip lost StatusLine: %+v", got.StatusLine)
	}
}

func TestStatusLineEntryIsClaudeops(t *testing.T) {
	const cmd = "/usr/bin/claudeops agy statusline"
	cases := []struct {
		name  string
		entry *StatusLineEntry
		want  bool
	}{
		{"nil entry", nil, false},
		{"matching command", &StatusLineEntry{Type: "command", Command: cmd}, true},
		{"different command", &StatusLineEntry{Type: "command", Command: "some-other-tool"}, false},
		{"different type", &StatusLineEntry{Type: "script", Command: cmd}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.entry.IsClaudeops(cmd); got != tc.want {
				t.Errorf("IsClaudeops() = %v, want %v", got, tc.want)
			}
		})
	}
}
