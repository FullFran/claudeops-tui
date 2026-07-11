package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OpencodeCred is one entry in opencode's auth.json. opencode stores a map
// keyed by provider id (openai, google, github-copilot, ...); each value is
// either an OAuth block (access/refresh/expires, plus accountId for openai) or
// an API-key block (key). We read it so ClaudeOps can reuse a session the user
// already authenticated inside opencode, without a second login.
type OpencodeCred struct {
	Type      string `json:"type"` // "oauth" | "api"
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	Key       string `json:"key"`
	AccountID string `json:"accountId"`
	Expires   int64  `json:"expires"` // unix millis
}

// opencodeAuthPath resolves opencode's auth.json honoring $XDG_DATA_HOME, then
// the conventional ~/.local/share/opencode/auth.json.
func opencodeAuthPath() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "opencode", "auth.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "share", "opencode", "auth.json")
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

// LoadOpencodeAuth reads and decodes opencode's auth.json. An empty path uses
// the conventional location.
func LoadOpencodeAuth(path string) (map[string]OpencodeCred, error) {
	if path == "" {
		path = opencodeAuthPath()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]OpencodeCred
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
