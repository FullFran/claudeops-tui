package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Event is the payload Claude Code sends to the hook command over stdin.
// We only decode the fields we care about; unknown keys are ignored.
type Event struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Source         string `json:"source,omitempty"` // SessionStart: startup|resume|clear|compact
	Model          string `json:"model,omitempty"`
}

// Sidecar is the per-session state file we write under liveDir.
type Sidecar struct {
	SessionID   string    `json:"session_id"`
	ProjectPath string    `json:"project_path"`
	State       string    `json:"state"` // "working" | "waiting"
	LastEvent   string    `json:"last_event"`
	Model       string    `json:"model,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Handle reads one event JSON object from r and updates the sidecar under
// liveDir accordingly. SessionEnd deletes the sidecar. Returns nil on success;
// errors are non-fatal for Claude Code (handler exits 0 regardless) but are
// returned here so the caller can log them.
func Handle(r io.Reader, liveDir string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var ev Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return fmt.Errorf("parse event: %w", err)
	}
	if ev.SessionID == "" {
		return nil
	}
	if err := os.MkdirAll(liveDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(liveDir, ev.SessionID+".json")

	if ev.HookEventName == "SessionEnd" {
		_ = os.Remove(path)
		return nil
	}

	sc := Sidecar{
		SessionID:   ev.SessionID,
		ProjectPath: ev.Cwd,
		LastEvent:   ev.HookEventName,
		Model:       ev.Model,
		UpdatedAt:   time.Now().UTC(),
	}
	switch ev.HookEventName {
	case "UserPromptSubmit":
		sc.State = "working"
	case "SessionStart", "Stop":
		sc.State = "waiting"
	default:
		sc.State = "waiting"
	}

	out, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
