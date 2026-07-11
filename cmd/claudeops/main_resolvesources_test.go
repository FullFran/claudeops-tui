package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/config"
)

func names(srcs []config.SourceConfig) []string {
	out := make([]string, len(srcs))
	for i, s := range srcs {
		out[i] = s.Name
	}
	return out
}

func TestResolveSourcesExplicitConfigWins(t *testing.T) {
	settings := config.Settings{Sources: []config.SourceConfig{
		{Name: "claude", Enabled: true},
	}}
	// Even though codex/opencode paths exist, an explicit config is returned as-is.
	dir := t.TempDir()
	codexRoot := filepath.Join(dir, "sessions")
	_ = os.MkdirAll(codexRoot, 0o755)
	ocDB := filepath.Join(dir, "opencode.db")
	_ = os.WriteFile(ocDB, []byte("x"), 0o644)

	got := resolveSourcesWith(settings, codexRoot, ocDB)
	if len(got) != 1 || got[0].Name != "claude" {
		t.Fatalf("explicit config not returned as-is: %v", names(got))
	}
}

func TestResolveSourcesAutoDetects(t *testing.T) {
	dir := t.TempDir()
	codexRoot := filepath.Join(dir, "sessions")
	ocDB := filepath.Join(dir, "opencode.db")

	tests := []struct {
		name        string
		mkCodexDir  bool
		mkOpencode  bool
		wantSources []string
	}{
		{name: "claude only", wantSources: []string{"claude"}},
		{name: "codex present", mkCodexDir: true, wantSources: []string{"claude", "codex"}},
		{name: "opencode present", mkOpencode: true, wantSources: []string{"claude", "opencode"}},
		{name: "all present", mkCodexDir: true, mkOpencode: true, wantSources: []string{"claude", "codex", "opencode"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.RemoveAll(codexRoot)
			_ = os.Remove(ocDB)
			if tt.mkCodexDir {
				if err := os.MkdirAll(codexRoot, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tt.mkOpencode {
				if err := os.WriteFile(ocDB, []byte("db"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			got := names(resolveSourcesWith(config.Settings{}, codexRoot, ocDB))
			if len(got) != len(tt.wantSources) {
				t.Fatalf("got %v, want %v", got, tt.wantSources)
			}
			for i, want := range tt.wantSources {
				if got[i] != want {
					t.Errorf("source[%d] = %q, want %q (%v)", i, got[i], want, got)
				}
			}
		})
	}
}

func TestResolveSourcesIgnoresCodexFile(t *testing.T) {
	// A codex "sessions" path that is a file (not a dir) must not enable codex.
	dir := t.TempDir()
	codexPath := filepath.Join(dir, "sessions")
	if err := os.WriteFile(codexPath, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := names(resolveSourcesWith(config.Settings{}, codexPath, filepath.Join(dir, "none.db")))
	if len(got) != 1 || got[0] != "claude" {
		t.Errorf("codex enabled from a non-dir path: %v", got)
	}
}
