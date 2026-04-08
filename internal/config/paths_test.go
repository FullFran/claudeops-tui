package config

import (
	"os"
	"testing"
)

func TestForHome(t *testing.T) {
	p := ForHome("/tmp/fakehome")
	cases := map[string]string{
		p.ClaudeDir:       "/tmp/fakehome/.claude",
		p.ClaudeProjects:  "/tmp/fakehome/.claude/projects",
		p.ClaudeCreds:     "/tmp/fakehome/.claude/.credentials.json",
		p.DataDir:         "/tmp/fakehome/.claudeops",
		p.DBPath:          "/tmp/fakehome/.claudeops/claudeops.db",
		p.PricingPath:     "/tmp/fakehome/.claudeops/pricing.toml",
		p.CurrentTaskPath: "/tmp/fakehome/.claudeops/current-task.json",
		p.ConfigPath:      "/tmp/fakehome/.claudeops/config.toml",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestEnsureDataDir(t *testing.T) {
	dir := t.TempDir()
	p := ForHome(dir)
	if err := p.EnsureDataDir(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Errorf("expected dir at %s", p.DataDir)
	}
}
