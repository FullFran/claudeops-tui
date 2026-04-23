package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/fullfran/claudeops-tui/internal/live"
)

// Classroom view — a grid of "desks" where each desk is a kawaii Claude monkey
// representing one live session. The monkey's face + badge reflect activity:
//   - working → happy sparkle-eyed face with a pulsing ⚡
//   - waiting → sleepy face with 💤
//
// The grid auto-flows based on viewport width; each desk is a fixed-width box.

const (
	deskWidth  = 20 // total width incl. borders
	deskHeight = 7  // total height incl. borders
)

var (
	deskBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "═",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "╰",
		BottomRight: "╯",
	}
	deskStyleWorking = lipgloss.NewStyle().
				Border(deskBorder).
				BorderForeground(lipgloss.Color("10")). // green
				Width(deskWidth-2).
				Padding(0, 1)
	deskStyleWaiting = lipgloss.NewStyle().
				Border(deskBorder).
				BorderForeground(lipgloss.Color("11")). // yellow
				Width(deskWidth-2).
				Padding(0, 1)
	monkeyStyleWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	monkeyStyleWaiting = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	labelStyle         = lipgloss.NewStyle().Bold(true)
	pathStyle          = lipgloss.NewStyle().Faint(true)
	badgeWorking       = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	badgeWaiting       = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// renderClassroomTab paints the grid of live sessions.
func renderClassroomTab(m Model) string {
	var sb strings.Builder

	sb.WriteString(headerStyle.Render("Classroom — live Claude Code sessions") + "  ")
	sb.WriteString(dimStyle.Render(fmt.Sprintf("(%d)", len(m.LiveSessions))) + "\n")
	sb.WriteString(dimStyle.Render("  ✨ working      💤 waiting for input") + "\n\n")

	if len(m.LiveSessions) == 0 {
		sb.WriteString(emptyClassroom())
		return sb.String()
	}

	// Decide columns that fit the viewport width. One desk + 2-space gutter.
	cols := 1
	if m.width > 0 {
		cols = (m.width + 2) / (deskWidth + 2)
		if cols < 1 {
			cols = 1
		}
	}

	// Render desks in row-major order.
	frame := animFrame(time.Now())
	rows := [][]string{}
	var row []string
	for i, s := range m.LiveSessions {
		row = append(row, renderDesk(s, frame))
		if (i+1)%cols == 0 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	for _, r := range rows {
		// JoinHorizontal aligns all desks to their top edge.
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, interleave(r, "  ")...))
		sb.WriteString("\n")
	}
	return sb.String()
}

// renderDesk paints a single kawaii-monkey desk for one session.
// frame is an integer animation frame (0..3) that cycles for subtle motion.
func renderDesk(s live.Session, frame int) string {
	monkey, badge, st, badgeSt := monkeyFor(s.State, frame)
	name := truncate(s.ProjectName, deskWidth-4)
	sess := truncate(s.SessionID, deskWidth-4)

	inner := st.Render(monkey) + "  " + badgeSt.Render(badge) + "\n" +
		labelStyle.Render(name) + "\n" +
		pathStyle.Render(sess)

	if s.State == live.StateWorking {
		return deskStyleWorking.Render(inner)
	}
	return deskStyleWaiting.Render(inner)
}

// monkeyFor returns the ASCII monkey face + badge glyph + styles for a state.
// frame drives a tiny 2-frame animation (sparkle pulse, eye-blink).
func monkeyFor(st live.State, frame int) (monkey, badge string, monkeyStyle, badgeStyle lipgloss.Style) {
	switch st {
	case live.StateWorking:
		// Bright-eyed focused monkey with a banana.
		faces := []string{
			" ∩   ∩ \n(•ᴥ•) 🍌",
			" ∩   ∩ \n(◉ᴥ◉) 🍌",
		}
		badges := []string{"✨", "⚡"}
		return faces[frame%len(faces)], badges[frame%len(badges)],
			monkeyStyleWorking, badgeWorking
	default:
		// Sleepy monkey — eyes closed, zZ.
		faces := []string{
			" ∩   ∩ \n(-ᴥ-) zZ",
			" ∩   ∩ \n(~ᴥ~) zZ",
		}
		return faces[frame%len(faces)], "💤",
			monkeyStyleWaiting, badgeWaiting
	}
}

// animFrame returns a small integer cycling over time — used to drive the
// subtle 2-frame monkey animation. Changes every ~1 second.
func animFrame(now time.Time) int {
	return int(now.Unix()) % 2
}

// emptyClassroom is shown when there are no live sessions.
func emptyClassroom() string {
	return dimStyle.Render(`  🏫 The classroom is empty.

  No Claude Code sessions have been active in the last few minutes.
  Start a session in any project and it will appear here.`) + "\n"
}

// interleave inserts sep between every pair of adjacent strings.
func interleave(parts []string, sep string) []string {
	if len(parts) <= 1 {
		return parts
	}
	out := make([]string, 0, 2*len(parts)-1)
	for i, p := range parts {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, p)
	}
	return out
}
