package agy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// StatusLineEntry mirrors the "statusLine" object in agy's settings.json:
// https://antigravity.google/docs/cli/statusline. `claudeops agy setup`
// always writes exactly {type, command} — the optional `padding`, `enabled`
// and `stack_with_default` keys agy also documents are not modeled here,
// since Setup replaces the whole object rather than editing it in place.
type StatusLineEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// IsClaudeops reports whether e is the statusLine claudeops itself would
// configure to run command. A nil entry (key absent) is never ours.
func (e *StatusLineEntry) IsClaudeops(command string) bool {
	return e != nil && e.Type == "command" && e.Command == command
}

// Settings mirrors agy's settings.json. Everything besides "statusLine" is
// preserved verbatim via Extra — the same approach internal/hooks.Settings
// uses for Claude Code's settings.json, so an unrelated key we do not model
// (or do not even know about) round-trips untouched.
type Settings struct {
	StatusLine *StatusLineEntry
	Extra      map[string]json.RawMessage
}

// LoadSettings reads agy's settings.json at path, returning the parsed
// settings and the file's existing permission mode (0 when the file does not
// exist yet, so the caller knows to fall back to a sensible default on
// create).
//
// A missing file is an ordinary first run: empty Settings, no error. A file
// that fails to parse — as JSON, or a "statusLine" value that is not the
// {type, command} shape — is reported as an error and the file is never
// touched. Refusing here is what lets the caller leave an unparsable
// settings.json byte-for-byte untouched: agy itself takes the same stance on
// its own config file.
func LoadSettings(path string) (*Settings, os.FileMode, error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return &Settings{Extra: map[string]json.RawMessage{}}, 0, nil
		}
		return nil, 0, statErr
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	mode := info.Mode().Perm()
	s := &Settings{Extra: map[string]json.RawMessage{}}
	if len(data) == 0 {
		return s, mode, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", path, err)
	}
	for k, v := range raw {
		if k != "statusLine" {
			s.Extra[k] = v
			continue
		}
		var e StatusLineEntry
		if err := json.Unmarshal(v, &e); err != nil {
			return nil, 0, fmt.Errorf("parse %s: statusLine: %w", path, err)
		}
		s.StatusLine = &e
	}
	return s, mode, nil
}

// SaveSettings writes s to path atomically (temp file + rename). mode sets
// the file's permission bits; 0 means "no existing file to match", which
// defaults to 0600 — the snapshot-cache convention this codebase already
// uses for anything that is created fresh rather than edited in place.
func SaveSettings(path string, s *Settings, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	out := map[string]json.RawMessage{}
	for k, v := range s.Extra {
		out[k] = v
	}
	if s.StatusLine != nil {
		b, err := json.Marshal(s.StatusLine)
		if err != nil {
			return err
		}
		out["statusLine"] = b
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings.json.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
