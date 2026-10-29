package pi

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProviderResolution describes the provider resolved from a Pi / Gentle Shell session.
type ProviderResolution struct {
	RawProvider string
	Model       string
	MappedName  string
	Supported   bool
	Unavailable bool
	Reason      string
}

// SafeCwd converts a directory path to Pi's session directory name convention.
// For example: "/home/user/project" -> "--home-user-project--".
func SafeCwd(cwd string) string {
	clean := filepath.Clean(cwd)
	// Remove leading slashes and drive letters (e.g. C:)
	clean = strings.TrimPrefix(clean, "/")
	clean = strings.TrimPrefix(clean, "\\")
	// Replace separators and colons with dashes
	clean = strings.ReplaceAll(clean, "/", "-")
	clean = strings.ReplaceAll(clean, "\\", "-")
	clean = strings.ReplaceAll(clean, ":", "-")
	return "--" + clean + "--"
}

// AgentDirs returns the candidate directories where Pi or Gentle Shell store configuration and sessions.
func AgentDirs() []string {
	var dirs []string
	if env := os.Getenv("PI_CODING_AGENT_DIR"); env != "" {
		dirs = append(dirs, env)
	}
	if env := os.Getenv("GENTLE_SHELL_AGENT_DIR"); env != "" {
		dirs = append(dirs, env)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		dirs = append(dirs, filepath.Join(home, ".gentle-shell", "agent"))
		dirs = append(dirs, filepath.Join(home, ".pi", "agent"))
	}
	return dirs
}

// FindSessionDir searches the agent candidate directories for a session folder matching cwd.
// Returns the session directory path and the agent directory path where it was found.
func FindSessionDir(cwd string) (string, string) {
	safe := SafeCwd(cwd)
	for _, ad := range AgentDirs() {
		sessionDir := filepath.Join(ad, "sessions", safe)
		if fi, err := os.Stat(sessionDir); err == nil && fi.IsDir() {
			return sessionDir, ad
		}
	}
	// Fall back to the first existing agent dir even if no session directory exists yet
	for _, ad := range AgentDirs() {
		if fi, err := os.Stat(ad); err == nil && fi.IsDir() {
			return "", ad
		}
	}
	return "", ""
}

// ResolveSessionProvider finds the active provider and model for a given working directory.
func ResolveSessionProvider(cwd string) ProviderResolution {
	sessionDir, agentDir := FindSessionDir(cwd)

	var rawProvider, modelId string

	if sessionDir != "" {
		rawProvider, modelId = readSessionFile(sessionDir)
	}

	// Fallback to agent settings.json if not found in session events
	if rawProvider == "" && agentDir != "" {
		rawProvider, modelId = readAgentSettings(agentDir)
	}

	if rawProvider == "" {
		return ProviderResolution{
			RawProvider: "pi",
			Unavailable: true,
			Reason:      "no active session or settings found",
		}
	}

	return ClassifyProvider(rawProvider, modelId)
}

// ClassifyProvider maps Pi's raw provider name to ClaudeOps's provider naming
// and quota availability.
func ClassifyProvider(rawProvider, modelId string) ProviderResolution {
	rawLower := strings.ToLower(strings.TrimSpace(rawProvider))
	switch rawLower {
	case "anthropic", "claude":
		return ProviderResolution{
			RawProvider: rawProvider,
			Model:       modelId,
			MappedName:  "claude",
			Supported:   true,
			Unavailable: false,
		}
	case "agy", "antigravity":
		return ProviderResolution{
			RawProvider: rawProvider,
			Model:       modelId,
			MappedName:  "antigravity",
			Supported:   true,
			Unavailable: false,
		}
	case "gemini", "google":
		return ProviderResolution{
			RawProvider: rawProvider,
			Model:       modelId,
			MappedName:  "gemini",
			Supported:   true,
			Unavailable: false,
		}
	case "openai", "codex":
		// Per issue #98: Pi's OpenAI OAuth discards id_token, and has no verified
		// account_id matching Codex or documented quota endpoint.
		return ProviderResolution{
			RawProvider: rawProvider,
			Model:       modelId,
			MappedName:  "openai",
			Supported:   false,
			Unavailable: true,
			Reason:      "openai oauth has no supported quota endpoint",
		}
	default:
		return ProviderResolution{
			RawProvider: rawProvider,
			Model:       modelId,
			MappedName:  rawProvider,
			Supported:   false,
			Unavailable: true,
			Reason:      "unsupported provider",
		}
	}
}

// readSessionFile scans the latest .jsonl session file in sessionDir for model changes.
func readSessionFile(sessionDir string) (string, string) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return "", ""
	}

	var jsonlFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			jsonlFiles = append(jsonlFiles, filepath.Join(sessionDir, e.Name()))
		}
	}
	if len(jsonlFiles) == 0 {
		return "", ""
	}

	// Sort files by modification time descending
	type fileTime struct {
		path  string
		mtime int64
	}
	var fts []fileTime
	for _, p := range jsonlFiles {
		fi, err := os.Stat(p)
		if err == nil {
			fts = append(fts, fileTime{path: p, mtime: fi.ModTime().UnixNano()})
		}
	}
	sort.Slice(fts, func(i, j int) bool {
		return fts[i].mtime > fts[j].mtime
	})

	for _, ft := range fts {
		prov, model := scanSessionJSONL(ft.path)
		if prov != "" {
			return prov, model
		}
	}
	return "", ""
}

type modelChangeEvent struct {
	Type     string `json:"type"`
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
	Message  *struct {
		Role     string `json:"role"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"message,omitempty"`
}

func scanSessionJSONL(path string) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	// Read lines to find the last model change or assistant message
	var lastProvider, lastModel string
	scanner := bufio.NewScanner(f)
	// Buffer can be large for long messages
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev modelChangeEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type == "model_change" && ev.Provider != "" {
			lastProvider = ev.Provider
			lastModel = ev.ModelID
		} else if ev.Type == "message" && ev.Message != nil && ev.Message.Role == "assistant" && ev.Message.Provider != "" {
			lastProvider = ev.Message.Provider
			lastModel = ev.Message.Model
		}
	}

	return lastProvider, lastModel
}

func readAgentSettings(agentDir string) (string, string) {
	settingsPath := filepath.Join(agentDir, "settings.json")
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", ""
	}
	var s struct {
		DefaultProvider string `json:"defaultProvider"`
		DefaultModel    string `json:"defaultModel"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return "", ""
	}
	return s.DefaultProvider, s.DefaultModel
}
