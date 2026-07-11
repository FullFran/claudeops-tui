package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestHumanDur(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Minute, "now"},
		{0, "now"},
		{45 * time.Minute, "45m"},
		{3*time.Hour + 20*time.Minute, "3h 20m"},
		{5 * 24 * time.Hour, "5d 0h"},
		{119*time.Hour + 59*time.Minute, "4d 23h"},
	}
	for _, tt := range tests {
		if got := humanDur(tt.d); got != tt.want {
			t.Errorf("humanDur(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestPadRightDisplayWidth(t *testing.T) {
	// ASCII pads to the requested width.
	if got := padRight("ab", 5); got != "ab   " {
		t.Errorf("padRight ascii = %q", got)
	}
	// An emoji occupies 2 display columns, so padding must account for it and
	// keep the total display width equal to the ASCII case (column alignment).
	emoji := padRight("✨", 5)
	ascii := padRight("xx", 5)
	if lipgloss.Width(emoji) != lipgloss.Width(ascii) {
		t.Errorf("emoji width %d != ascii width %d (misaligned columns)",
			lipgloss.Width(emoji), lipgloss.Width(ascii))
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	// Multi-byte runes must never be cut mid-rune (which would emit garbage).
	got := truncate("áéíóúñ", 3)
	if lipgloss.Width(got) > 3 {
		t.Errorf("truncate width = %d, want <= 3 (%q)", lipgloss.Width(got), got)
	}
	for _, r := range got {
		if r == '�' {
			t.Errorf("truncate produced replacement char (mid-rune cut): %q", got)
		}
	}
	// Short strings are returned unchanged.
	if got := truncate("hi", 10); got != "hi" {
		t.Errorf("truncate short = %q, want hi", got)
	}
}

func TestRuleLine(t *testing.T) {
	if got := lipgloss.Width(ruleLine(40)); got != 40 {
		t.Errorf("ruleLine(40) width = %d, want 40", got)
	}
	// Capped at 100 for very wide terminals.
	if got := lipgloss.Width(ruleLine(400)); got != 100 {
		t.Errorf("ruleLine(400) width = %d, want 100", got)
	}
	// Unknown width falls back without panicking.
	if got := lipgloss.Width(ruleLine(0)); got == 0 {
		t.Error("ruleLine(0) should render a fallback rule")
	}
}
