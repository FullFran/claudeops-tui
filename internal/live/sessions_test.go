package live

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeJSONL(t *testing.T, path string, lines []string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestScan_ClassifiesStates(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	// Project dir name encodes /home/me/alpha (no dashes in basename to keep
	// decode unambiguous).
	projA := filepath.Join(root, "-home-me-alpha")
	projB := filepath.Join(root, "-home-me-beta")
	projC := filepath.Join(root, "-home-me-gamma")

	// Case 1: very recently modified → working (regardless of last-event type)
	writeJSONL(t, filepath.Join(projA, "aaa.jsonl"),
		[]string{`{"type":"assistant","uuid":"1"}`},
		now.Add(-2*time.Second))

	// Case 2: not recently modified, last event = "assistant" → waiting
	writeJSONL(t, filepath.Join(projB, "bbb.jsonl"),
		[]string{`{"type":"user","uuid":"1"}`, `{"type":"assistant","uuid":"2"}`},
		now.Add(-45*time.Second))

	// Case 3: old → excluded
	writeJSONL(t, filepath.Join(projC, "ccc.jsonl"),
		[]string{`{"type":"assistant","uuid":"1"}`},
		now.Add(-10*time.Minute))

	sessions, err := Scan(root, Config{
		WorkingWindow: 8 * time.Second,
		ActiveWindow:  5 * time.Minute,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %+v", len(sessions), sessions)
	}
	// Sorted by mtime desc: projA first, projB second.
	if sessions[0].SessionID != "aaa" || sessions[0].State != StateWorking {
		t.Errorf("sessions[0]: want aaa/working, got %s/%s", sessions[0].SessionID, sessions[0].State)
	}
	if sessions[0].ProjectName != "alpha" {
		t.Errorf("projectName: want alpha, got %q", sessions[0].ProjectName)
	}
	if sessions[1].SessionID != "bbb" || sessions[1].State != StateWaiting {
		t.Errorf("sessions[1]: want bbb/waiting, got %s/%s", sessions[1].SessionID, sessions[1].State)
	}
	if sessions[1].LastEvent != "assistant" {
		t.Errorf("lastEvent: want assistant, got %q", sessions[1].LastEvent)
	}
}

func TestScan_LastUserEventMeansWorking(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	// mtime outside working window BUT last event is "user" → claude is processing.
	writeJSONL(t, filepath.Join(root, "-proj", "x.jsonl"),
		[]string{`{"type":"assistant","uuid":"1"}`, `{"type":"user","uuid":"2"}`},
		now.Add(-30*time.Second))

	sessions, err := Scan(root, Config{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1, got %d", len(sessions))
	}
	if sessions[0].State != StateWorking {
		t.Errorf("want working, got %s", sessions[0].State)
	}
}

func TestScan_MissingRootReturnsNil(t *testing.T) {
	sessions, err := Scan(filepath.Join(t.TempDir(), "does-not-exist"), Config{})
	if err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
	if sessions != nil {
		t.Errorf("want nil sessions, got %+v", sessions)
	}
}

func TestScan_IgnoresNonJSONL(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeJSONL(t, filepath.Join(root, "-proj", "notes.txt"),
		[]string{"hello"},
		now.Add(-1*time.Second))
	sessions, err := Scan(root, Config{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0, got %d", len(sessions))
	}
}

func TestDecodeProjectPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"-home-me-foo", "/home/me/foo"},
		{"-home-franblakia-fullfran-ClaudeOps-TUI", "/home/franblakia/fullfran/ClaudeOps/TUI"},
		{"", ""},
	}
	for _, c := range cases {
		if got := decodeProjectPath(c.in); got != c.want {
			t.Errorf("decodeProjectPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReadLastEventType_HandlesTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	content := `{"type":"user","uuid":"1"}` + "\n" + `{"type":"assistant","uuid":"2"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readLastEventType(path); got != "assistant" {
		t.Errorf("want assistant, got %q", got)
	}
}

func TestScan_SidecarOverridesState(t *testing.T) {
	root := t.TempDir()
	liveDir := t.TempDir()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	// JSONL mtime says "idle" (waiting, last event assistant, 2 min old) but the
	// hook sidecar says "working" — sidecar must win.
	writeJSONL(t, filepath.Join(root, "-home-u-proj", "sid-1.jsonl"),
		[]string{`{"type":"assistant","uuid":"1"}`},
		now.Add(-2*time.Minute))

	sc := `{"session_id":"sid-1","project_path":"/home/u/proj","state":"working","last_event":"UserPromptSubmit","updated_at":"2026-04-17T12:00:00Z"}`
	os.WriteFile(filepath.Join(liveDir, "sid-1.json"), []byte(sc), 0o600)

	sessions, err := Scan(root, Config{
		WorkingWindow: 8 * time.Second,
		ActiveWindow:  5 * time.Minute,
		LiveDir:       liveDir,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].State != StateWorking {
		t.Fatalf("want 1 working session from sidecar, got %+v", sessions)
	}
	if sessions[0].LastEvent != "UserPromptSubmit" {
		t.Errorf("LastEvent: got %q", sessions[0].LastEvent)
	}
}

func TestScan_SidecarKeepsStaleSessionVisible(t *testing.T) {
	root := t.TempDir()
	liveDir := t.TempDir()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	// No JSONL fresh enough — without sidecar, session would be dropped.
	writeJSONL(t, filepath.Join(root, "-home-u-old", "sid-2.jsonl"),
		[]string{`{"type":"assistant","uuid":"1"}`},
		now.Add(-2*time.Hour))

	sc := `{"session_id":"sid-2","project_path":"/home/u/old","state":"waiting","last_event":"Stop","updated_at":"2026-04-17T11:59:00Z"}`
	os.WriteFile(filepath.Join(liveDir, "sid-2.json"), []byte(sc), 0o600)

	sessions, err := Scan(root, Config{
		ActiveWindow: 5 * time.Minute,
		LiveDir:      liveDir,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session from sidecar (stale JSONL), got %d", len(sessions))
	}
	if sessions[0].SessionID != "sid-2" || sessions[0].State != StateWaiting {
		t.Errorf("unexpected: %+v", sessions[0])
	}
}
