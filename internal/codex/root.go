package codex

import (
	"os"
	"path/filepath"
)

// CodexRoot returns the directory where Codex CLI writes session rollout files.
// CODEX_HOME points at the Codex home directory (default ~/.codex); rollouts
// always live in its `sessions` subdirectory.
func CodexRoot() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return filepath.Join(v, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Best-effort: return a relative path if home is not resolvable.
		return filepath.Join(".codex", "sessions")
	}
	return filepath.Join(home, ".codex", "sessions")
}
