// Package hooks installs, removes, and serves Claude Code hooks that let
// claudeops know about live sessions in real time (no mtime heuristics).
//
// Design:
//   - `install` performs an idempotent deep-merge into ~/.claude/settings.json.
//   - Every entry we add is tagged with "_source": "claudeops" so we can
//     round-trip them safely (Claude Code ignores unknown fields on hook
//     objects, so the marker survives).
//   - `handle` reads one JSON event from stdin and writes/updates/removes a
//     sidecar file under ~/.claudeops/live/{session_id}.json.
//   - The TUI reads the sidecars directly, no guessing from JSONL mtimes.
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SourceMarker identifies hook entries owned by claudeops.
const SourceMarker = "claudeops"

// ManagedEvents are the Claude Code hook events claudeops registers.
// Order matters only for deterministic output in settings.json.
var ManagedEvents = []string{
	"SessionStart",
	"UserPromptSubmit",
	"Stop",
	"SessionEnd",
}

// Entry is a single hook command in settings.json. Unknown fields are kept
// verbatim on read so we never drop user config we don't understand.
type Entry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	Source  string `json:"_source,omitempty"`
}

// Group is the Claude Code matcher+hooks object (one per matcher value).
type Group struct {
	Matcher string  `json:"matcher,omitempty"`
	Hooks   []Entry `json:"hooks"`
}

// Settings mirrors the subset of ~/.claude/settings.json we read/write.
// Everything outside of "hooks" is preserved verbatim via Extra.
type Settings struct {
	Hooks map[string][]Group         `json:"hooks,omitempty"`
	Extra map[string]json.RawMessage `json:"-"`
}

// Load reads settings.json. Missing file returns an empty Settings (no error).
func Load(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{Hooks: map[string][]Group{}, Extra: map[string]json.RawMessage{}}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return &Settings{Hooks: map[string][]Group{}, Extra: map[string]json.RawMessage{}}, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	s := &Settings{Hooks: map[string][]Group{}, Extra: map[string]json.RawMessage{}}
	for k, v := range raw {
		if k == "hooks" {
			if err := json.Unmarshal(v, &s.Hooks); err != nil {
				return nil, fmt.Errorf("parse hooks: %w", err)
			}
			continue
		}
		s.Extra[k] = v
	}
	if s.Hooks == nil {
		s.Hooks = map[string][]Group{}
	}
	return s, nil
}

// Save writes settings.json atomically (tmp + rename) and takes a timestamped
// backup of the previous contents if the file existed.
func Save(path string, s *Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if prev, err := os.ReadFile(path); err == nil && len(prev) > 0 {
		bak := fmt.Sprintf("%s.bak-%d", path, time.Now().Unix())
		if err := os.WriteFile(bak, prev, 0o600); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}

	out := map[string]json.RawMessage{}
	for k, v := range s.Extra {
		out[k] = v
	}
	if len(s.Hooks) > 0 {
		hb, err := json.Marshal(s.Hooks)
		if err != nil {
			return err
		}
		out["hooks"] = hb
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings.json.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
