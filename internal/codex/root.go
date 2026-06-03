package codex

import (
	"os"
	"path/filepath"
)

// CodexRoot returns the directory where Codex CLI writes session rollout files.
// Resolution order:
//  1. CODEX_HOME environment variable (if non-empty)
//  2. ~/.codex/sessions (conventional default)
func CodexRoot() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Best-effort: return a relative path if home is not resolvable.
		return filepath.Join(".codex", "sessions")
	}
	return filepath.Join(home, ".codex", "sessions")
}
