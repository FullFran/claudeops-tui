package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/config"
)

func TestMCPRegisteredReadsClaudeConfig(t *testing.T) {
	cases := []struct {
		name    string
		content string // empty means "no file at all"
		want    bool
	}{
		{name: "no config file", content: "", want: false},
		{name: "server registered", content: `{"mcpServers":{"claudeops":{"command":"claudeops"}}}`, want: true},
		{name: "other servers only", content: `{"mcpServers":{"other":{"command":"x"}}}`, want: false},
		{name: "no mcpServers key", content: `{"projects":{}}`, want: false},
		{name: "malformed json", content: `{`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			if tc.content != "" {
				if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := mcpRegistered("claudeops"); got != tc.want {
				t.Errorf("mcpRegistered = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMCPRowIsReadonly(t *testing.T) {
	for _, item := range settingsItems() {
		if item.label != "Claude Code" {
			continue
		}
		if item.toggle != nil {
			t.Error("the MCP row must not pretend to register anything on toggle")
		}
		if !item.skip || item.readValue == nil {
			t.Error("the MCP row should report registration status as a readonly row")
		}
		if got := item.readValue(config.DefaultSettings()); got == "" {
			t.Error("the MCP row should render a status value")
		}
		return
	}
	t.Fatal("no MCP row found in settings")
}
