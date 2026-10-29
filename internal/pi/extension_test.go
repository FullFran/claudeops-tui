package pi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallAndUninstallExtension(t *testing.T) {
	tempAgent := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", tempAgent)

	// Verify embedded extension source is present
	if len(ExtensionSource) == 0 {
		t.Fatal("ExtensionSource is empty")
	}

	// Install
	installed, err := InstallExtension(false)
	if err != nil {
		t.Fatalf("InstallExtension failed: %v", err)
	}
	if len(installed) == 0 {
		t.Fatal("expected at least one installed path")
	}

	target := filepath.Join(tempAgent, "extensions", ExtensionFileName)
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("could not read installed file: %v", err)
	}
	if string(content) != string(ExtensionSource) {
		t.Errorf("installed content mismatch")
	}

	// Status check
	statuses := ExtensionStatus()
	found := false
	for _, s := range statuses {
		if s.Path == target && s.Installed && s.Size > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ExtensionStatus did not report installed file at %s: %+v", target, statuses)
	}

	// Idempotent re-install without force
	reinstalled, err := InstallExtension(false)
	if err != nil {
		t.Fatalf("re-install failed: %v", err)
	}
	if len(reinstalled) == 0 {
		t.Fatal("expected re-install to report installed file")
	}

	// Uninstall
	removed, err := UninstallExtension()
	if err != nil {
		t.Fatalf("UninstallExtension failed: %v", err)
	}
	if len(removed) == 0 {
		t.Fatal("expected at least one removed path")
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target file still exists after uninstall: %v", err)
	}
}
