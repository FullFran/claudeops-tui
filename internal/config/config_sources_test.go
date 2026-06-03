package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSourceConfigDefaults covers REQ-1.6.1 and REQ-1.6.2.
func TestSourceConfigDefaults(t *testing.T) {
	t.Run("REQ-1.6.1 default config has claude enabled", func(t *testing.T) {
		dir := t.TempDir()
		// No [[sources]] section in config → only claude enabled.
		p := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(p, []byte(`[dashboard]
show_today = true
`), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		sources := s.SourceConfigs()
		var found bool
		for _, sc := range sources {
			if sc.Name == "claude" {
				found = true
				if !sc.Enabled {
					t.Error("claude source should be enabled by default")
				}
			}
		}
		if !found {
			t.Errorf("default SourceConfigs missing claude entry; got: %+v", sources)
		}
		// Should have exactly one entry (claude) by default.
		if len(sources) != 1 {
			t.Errorf("want 1 source (claude), got %d: %+v", len(sources), sources)
		}
	})

	t.Run("REQ-1.6.2 codex.enabled=false in TOML is reflected", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(p, []byte(`
[[sources]]
name = "codex"
enabled = false
root = "/tmp/codex-sessions"
`), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		sources := s.SourceConfigs()
		var codexFound bool
		for _, sc := range sources {
			if sc.Name == "codex" {
				codexFound = true
				if sc.Enabled {
					t.Error("codex should be disabled")
				}
			}
		}
		if !codexFound {
			t.Errorf("codex source not found in config; got: %+v", sources)
		}
	})
}
