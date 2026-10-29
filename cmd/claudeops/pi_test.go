package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/pi"
)

func TestCmdPiSetupStatusRemove(t *testing.T) {
	tempAgent := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", tempAgent)

	var out bytes.Buffer

	// 1. Setup
	if err := cmdPiWith(&out, []string{"setup"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if !strings.Contains(out.String(), "installed claudeops Pi extension") {
		t.Errorf("setup output missing confirmation: %s", out.String())
	}

	target := filepath.Join(tempAgent, "extensions", pi.ExtensionFileName)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected extension file at %s: %v", target, err)
	}

	// 2. Status
	out.Reset()
	if err := cmdPiWith(&out, []string{"status"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !strings.Contains(out.String(), "active") || !strings.Contains(out.String(), target) {
		t.Errorf("status output unexpected: %s", out.String())
	}

	// 3. Remove
	out.Reset()
	if err := cmdPiWith(&out, []string{"remove"}); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if !strings.Contains(out.String(), "removed claudeops Pi extension") {
		t.Errorf("remove output missing confirmation: %s", out.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("expected extension file removed, but still exists")
	}
}

func TestRunArgsPiPluginAlias(t *testing.T) {
	tempAgent := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", tempAgent)

	// install pi-plugin
	if err := runArgs([]string{"install", "pi-plugin"}); err != nil {
		t.Fatalf("install pi-plugin failed: %v", err)
	}
	target := filepath.Join(tempAgent, "extensions", pi.ExtensionFileName)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected extension installed via alias: %v", err)
	}

	// uninstall pi-plugin
	if err := runArgs([]string{"uninstall", "pi-plugin"}); err != nil {
		t.Fatalf("uninstall pi-plugin failed: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected extension removed via alias: %v", err)
	}
}
