package live

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
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
	if err := os.WriteFile(filepath.Join(liveDir, "sid-1.json"), []byte(sc), 0o600); err != nil {
		t.Fatal(err)
	}

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
	if err := os.WriteFile(filepath.Join(liveDir, "sid-2.json"), []byte(sc), 0o600); err != nil {
		t.Fatal(err)
	}

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

// writeRegistered leaves the file Claude Code keeps for a running session.
func writeRegistered(t *testing.T, dir string, pid int, sessionID string) {
	t.Helper()
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"kind":"interactive"}`, pid, sessionID)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", pid)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// goneProcess returns the id of a process that has just finished.
func goneProcess(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func scanIDs(t *testing.T, root string, cfg Config) []string {
	t.Helper()
	sessions, err := Scan(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, s := range sessions {
		ids = append(ids, s.SessionID)
	}
	sort.Strings(ids)
	return ids
}

func TestScan_KeepsOnlyRunningSessionsWhenClaudeListsThem(t *testing.T) {
	root, sessions, liveDir := t.TempDir(), t.TempDir(), t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	project := filepath.Join(root, "-home-me-alpha")
	old := now.Add(-10 * time.Minute)
	last := []string{`{"type":"assistant","uuid":"1"}`}
	writeJSONL(t, filepath.Join(project, "running.jsonl"), last, old)
	writeJSONL(t, filepath.Join(project, "closed.jsonl"), last, old)
	writeJSONL(t, filepath.Join(project, "crashed.jsonl"), last, old)
	writeJSONL(t, filepath.Join(project, "unlisted-but-writing.jsonl"), last, now.Add(-2*time.Second))
	writeRegistered(t, sessions, os.Getpid(), "running")
	writeRegistered(t, sessions, goneProcess(t), "crashed")
	if err := os.WriteFile(filepath.Join(sessions, "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "1.notes.key"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A session that was closed without its last hook still says it is working.
	sidecar := fmt.Sprintf(`{"session_id":"closed","project_path":"/home/me/alpha","state":"working","updated_at":%q}`,
		now.Add(-time.Minute).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(liveDir, "closed.json"), []byte(sidecar), 0o600); err != nil {
		t.Fatal(err)
	}

	got := scanIDs(t, root, Config{SessionsDir: sessions, LiveDir: liveDir, Now: func() time.Time { return now }})
	if want := []string{"running", "unlisted-but-writing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestScan_DropsASidecarOnlySessionWhoseProcessIsGone(t *testing.T) {
	root, sessions, liveDir := t.TempDir(), t.TempDir(), t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"running", "closed"} {
		sidecar := fmt.Sprintf(`{"session_id":%q,"project_path":"/home/me/alpha","state":"waiting","updated_at":%q}`,
			id, now.Add(-time.Minute).Format(time.RFC3339))
		if err := os.WriteFile(filepath.Join(liveDir, id+".json"), []byte(sidecar), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRegistered(t, sessions, os.Getpid(), "running")
	cfg := Config{SessionsDir: sessions, LiveDir: liveDir, Now: func() time.Time { return now }}

	if got, want := scanIDs(t, root, cfg), []string{"running"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("with projects: got %v, want %v", got, want)
	}
	if got, want := scanIDs(t, filepath.Join(root, "missing"), cfg), []string{"running"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("without projects: got %v, want %v", got, want)
	}
}

func TestScan_WithoutClaudesListGoesByRecentActivityAsBefore(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	writeJSONL(t, filepath.Join(root, "-home-me-alpha", "recent.jsonl"),
		[]string{`{"type":"assistant","uuid":"1"}`}, now.Add(-10*time.Minute))
	clock := func() time.Time { return now }

	for name, dir := range map[string]string{"not set": "", "missing": filepath.Join(root, "no-such-dir")} {
		got := scanIDs(t, root, Config{SessionsDir: dir, Now: clock})
		if want := []string{"recent"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
}

func TestScan_AListItCannotReadSaysNothing(t *testing.T) {
	root, sessions := t.TempDir(), t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	writeJSONL(t, filepath.Join(root, "-home-me-alpha", "recent.jsonl"),
		[]string{`{"type":"assistant","uuid":"1"}`}, now.Add(-10*time.Minute))
	// What a future Claude Code that renamed its fields would leave.
	if err := os.WriteFile(filepath.Join(sessions, "42.json"), []byte(`{"process":42,"id":"recent"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := scanIDs(t, root, Config{SessionsDir: sessions, Now: func() time.Time { return now }})
	if want := []string{"recent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestScan_AnEmptyListMeansNothingIsRunning(t *testing.T) {
	root, sessions := t.TempDir(), t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	writeJSONL(t, filepath.Join(root, "-home-me-alpha", "closed.jsonl"),
		[]string{`{"type":"assistant","uuid":"1"}`}, now.Add(-10*time.Minute))
	if got := scanIDs(t, root, Config{SessionsDir: sessions, Now: func() time.Time { return now }}); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}
