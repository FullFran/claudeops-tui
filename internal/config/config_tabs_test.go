package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The [tabs] block used to carry a `calendar` toggle for a tab that never
// shipped, and carried no toggle for the Classroom tab that did. Existing
// config files still hold the stale key, so loading one must ignore it without
// disturbing the toggles around it.
func TestLoadIgnoresTheStaleCalendarTabKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[tabs]
calendar = true
sessions = false
classroom = false
`), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("a config with a removed key must still load: %v", err)
	}
	if s.Tabs.Sessions {
		t.Error("sessions = false was not applied")
	}
	if s.Tabs.Classroom {
		t.Error("classroom = false was not applied")
	}
	// Toggles the file never mentions keep their defaults.
	if !s.Tabs.Projects || !s.Tabs.Models || !s.Tabs.Tasks || !s.Tabs.Insights {
		t.Errorf("unmentioned toggles were flipped: %+v", s.Tabs)
	}
}

func TestDefaultSettingsEnableTheClassroomTab(t *testing.T) {
	if !DefaultSettings().Tabs.Classroom {
		t.Error("classroom should default to visible like every other tab")
	}
}
