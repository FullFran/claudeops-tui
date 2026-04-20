// Package live discovers currently-active Claude Code sessions by scanning
// ~/.claude/projects/*.jsonl for recently-modified files and classifying
// their activity state (working vs waiting vs idle) heuristically.
//
// State heuristic:
//   - working: file mtime within WorkingWindow (claude is writing output)
//   - waiting: file mtime older than WorkingWindow but within ActiveWindow,
//     OR last event is an assistant turn (waiting for next user prompt)
//   - idle:    file mtime older than ActiveWindow → excluded from results
package live

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sidecar mirrors internal/hooks.Sidecar (kept local to avoid a package cycle).
type sidecar struct {
	SessionID   string    `json:"session_id"`
	ProjectPath string    `json:"project_path"`
	State       string    `json:"state"`
	LastEvent   string    `json:"last_event"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// State classifies how a live session appears to be behaving right now.
type State int

const (
	StateIdle State = iota
	StateWaiting
	StateWorking
)

func (s State) String() string {
	switch s {
	case StateWorking:
		return "working"
	case StateWaiting:
		return "waiting"
	}
	return "idle"
}

// Session is a single live Claude Code session on disk.
type Session struct {
	SessionID   string    // uuid — JSONL filename without extension
	ProjectName string    // derived from parent directory name
	ProjectPath string    // absolute cwd decoded from parent directory name
	File        string    // absolute JSONL path
	ModTime     time.Time // file mtime
	LastEvent   string    // last parsed event type ("user", "assistant", ...)
	State       State
}

// Config tunes the activity heuristic. Zero values fall back to sensible defaults.
type Config struct {
	// WorkingWindow — if the file was written within this window, we consider
	// claude to be actively working.
	WorkingWindow time.Duration
	// ActiveWindow — files older than this are dropped from the result set.
	ActiveWindow time.Duration
	// LiveDir — directory with hook-written sidecars (~/.claudeops/live). When
	// set, sidecars override the mtime-based state and keep sessions visible
	// even if the JSONL mtime is stale.
	LiveDir string
	// Now is an override for testing. Zero means time.Now().
	Now func() time.Time
}

func (c Config) workingWindow() time.Duration {
	if c.WorkingWindow <= 0 {
		return 8 * time.Second
	}
	return c.WorkingWindow
}

func (c Config) activeWindow() time.Duration {
	if c.ActiveWindow <= 0 {
		return 30 * time.Minute
	}
	return c.ActiveWindow
}

func (c Config) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// Scan walks root (e.g. ~/.claude/projects), looks at every *.jsonl file at
// depth 2 (project-dir/session.jsonl), filters by mtime and returns the
// classified live sessions sorted by most-recent first.
func Scan(root string, cfg Config) ([]Session, error) {
	now := cfg.now()
	cutoff := now.Add(-cfg.activeWindow())
	workingCutoff := now.Add(-cfg.workingWindow())

	sidecars := loadSidecars(cfg.LiveDir, cutoff)

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return sidecarsOnly(sidecars), nil
		}
		return nil, err
	}

	var out []Session
	seen := map[string]bool{}
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue
		}
		projDir := filepath.Join(root, projEntry.Name())
		files, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				continue
			}
			path := filepath.Join(projDir, f.Name())
			sid := strings.TrimSuffix(f.Name(), ".jsonl")
			lastType := readLastEventType(path)
			s := Session{
				SessionID:   sid,
				ProjectName: projectDisplayName(projEntry.Name()),
				ProjectPath: decodeProjectPath(projEntry.Name()),
				File:        path,
				ModTime:     info.ModTime(),
				LastEvent:   lastType,
				State:       classify(info.ModTime(), lastType, workingCutoff),
			}
			if sc, ok := sidecars[sid]; ok {
				s.State = stateFromString(sc.State)
				s.LastEvent = sc.LastEvent
			}
			seen[sid] = true
			out = append(out, s)
		}
	}

	// Add sessions that have an active sidecar but whose JSONL mtime fell
	// outside ActiveWindow (a long-idle session that the user hasn't closed).
	for sid, sc := range sidecars {
		if seen[sid] {
			continue
		}
		out = append(out, Session{
			SessionID:   sid,
			ProjectPath: sc.ProjectPath,
			ProjectName: filepath.Base(sc.ProjectPath),
			ModTime:     sc.UpdatedAt,
			LastEvent:   sc.LastEvent,
			State:       stateFromString(sc.State),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out, nil
}

func stateFromString(s string) State {
	switch s {
	case "working":
		return StateWorking
	case "waiting":
		return StateWaiting
	}
	return StateIdle
}

// loadSidecars reads every *.json file under dir and returns a sessionID → sidecar map.
// Entries older than cutoff are dropped (session almost certainly dead).
func loadSidecars(dir string, cutoff time.Time) map[string]sidecar {
	out := map[string]sidecar{}
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var sc sidecar
		if err := json.Unmarshal(data, &sc); err != nil || sc.SessionID == "" {
			continue
		}
		if sc.UpdatedAt.Before(cutoff) {
			continue
		}
		out[sc.SessionID] = sc
	}
	return out
}

func sidecarsOnly(sidecars map[string]sidecar) []Session {
	if len(sidecars) == 0 {
		return nil
	}
	out := make([]Session, 0, len(sidecars))
	for sid, sc := range sidecars {
		out = append(out, Session{
			SessionID:   sid,
			ProjectPath: sc.ProjectPath,
			ProjectName: filepath.Base(sc.ProjectPath),
			ModTime:     sc.UpdatedAt,
			LastEvent:   sc.LastEvent,
			State:       stateFromString(sc.State),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out
}

// classify decides the activity state for a single session.
//
// Rules (in order):
//  1. mtime within WorkingWindow → working (file is being actively appended)
//  2. last event == "user" → working (claude received a prompt, is processing)
//  3. otherwise → waiting
func classify(mtime time.Time, lastType string, workingCutoff time.Time) State {
	if mtime.After(workingCutoff) {
		return StateWorking
	}
	if lastType == "user" {
		return StateWorking
	}
	return StateWaiting
}

// readLastEventType reads the tail of a JSONL file and returns the `type`
// field of the last non-empty line. Returns "" on any failure — callers treat
// that as "unknown" and fall back to mtime-only classification.
func readLastEventType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	const tailSize = 8 * 1024
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	size := info.Size()
	start := int64(0)
	if size > tailSize {
		start = size - tailSize
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	// Walk lines in reverse to find the last non-empty JSON object with "type".
	for {
		idx := bytes.LastIndexByte(buf, '\n')
		var line []byte
		if idx < 0 {
			line = buf
			buf = nil
		} else {
			line = buf[idx+1:]
			buf = buf[:idx]
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if buf == nil {
				return ""
			}
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &head); err == nil && head.Type != "" {
			return head.Type
		}
		if buf == nil {
			return ""
		}
	}
}

// projectDisplayName turns "-home-franblakia-fullfran-ClaudeOps-TUI" into
// "ClaudeOps-TUI" — the basename of the decoded cwd.
func projectDisplayName(dir string) string {
	p := decodeProjectPath(dir)
	base := filepath.Base(p)
	if base == "." || base == "/" || base == "" {
		return dir
	}
	return base
}

// decodeProjectPath inverts Claude Code's encoding: leading "-" plus "-" as
// path separators. "-home-franblakia-foo" → "/home/franblakia/foo".
func decodeProjectPath(dir string) string {
	if dir == "" {
		return ""
	}
	if strings.HasPrefix(dir, "-") {
		return "/" + strings.ReplaceAll(dir[1:], "-", "/")
	}
	return strings.ReplaceAll(dir, "-", "/")
}
