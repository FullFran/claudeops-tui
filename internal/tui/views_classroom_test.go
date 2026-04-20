package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/live"
)

func TestRenderDesk_WorkingHasSparkle(t *testing.T) {
	s := live.Session{
		SessionID:   "abc12345",
		ProjectName: "myapp",
		State:       live.StateWorking,
	}
	got := renderDesk(s, 0)
	if !strings.Contains(got, "myapp") {
		t.Errorf("desk must show project name; got:\n%s", got)
	}
	if !strings.Contains(got, "ᴥ") {
		t.Errorf("desk must show monkey face; got:\n%s", got)
	}
}

func TestRenderDesk_WaitingHasZz(t *testing.T) {
	s := live.Session{
		SessionID:   "xyz",
		ProjectName: "other",
		State:       live.StateWaiting,
	}
	got := renderDesk(s, 0)
	if !strings.Contains(got, "zZ") {
		t.Errorf("waiting desk must show zZ; got:\n%s", got)
	}
}

func TestRenderClassroomTab_EmptyMessage(t *testing.T) {
	m := Model{}
	got := renderClassroomTab(m)
	if !strings.Contains(got, "classroom is empty") {
		t.Errorf("empty classroom needs hint; got:\n%s", got)
	}
}

func TestAnimFrame_Cycles(t *testing.T) {
	a := animFrame(time.Unix(0, 0))
	b := animFrame(time.Unix(1, 0))
	if a == b {
		t.Errorf("animFrame should change per second, got %d and %d", a, b)
	}
}
