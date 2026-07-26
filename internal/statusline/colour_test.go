package statusline

import (
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/usage"
)

func TestParseColourMode(t *testing.T) {
	cases := map[string]ColourMode{
		"":      ColourNone,
		"none":  ColourNone,
		"false": ColourNone,
		"off":   ColourNone,
		"0":     ColourNone,
		// Go's flag package turns a bare --color into "true"; that must mean
		// "colour this appropriately", not "assume tmux".
		"true":  ColourAuto,
		"on":    ColourAuto,
		"1":     ColourAuto,
		"auto":  ColourAuto,
		"tmux":  ColourTmux,
		"ansi":  ColourANSI,
		"ANSI":  ColourANSI,
		" tmux": ColourTmux,
	}
	for in, want := range cases {
		got, err := ParseColourMode(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if _, err := ParseColourMode("magenta"); err == nil {
		t.Error("an unknown mode should be rejected rather than silently ignored")
	}
}

func TestAutoResolvesByEnvironment(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	if got := ColourAuto.resolve(); got != ColourTmux {
		t.Errorf("inside tmux got %q want tmux", got)
	}
	t.Setenv("TMUX", "")
	if got := ColourAuto.resolve(); got != ColourANSI {
		t.Errorf("outside tmux got %q want ansi", got)
	}
}

func TestRenderColourSyntaxPerMode(t *testing.T) {
	snap := usage.Snapshot{FiveHour: bucket(42, time.Hour)}

	tmuxOut, err := Render(snap, nil, Options{Colour: ColourTmux, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tmuxOut, "#[fg=") || strings.Contains(tmuxOut, "\x1b[") {
		t.Errorf("tmux mode should emit tmux syntax only, got %q", tmuxOut)
	}

	ansiOut, err := Render(snap, nil, Options{Colour: ColourANSI, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ansiOut, "\x1b[38;2;") || strings.Contains(ansiOut, "#[fg=") {
		t.Errorf("ansi mode should emit SGR only, got %q", ansiOut)
	}

	plainOut, err := Render(snap, nil, Options{Colour: ColourNone, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if plainOut != "5h 42%" {
		t.Errorf("none mode should emit no escapes, got %q", plainOut)
	}
}

func TestAutoOutsideTmuxDoesNotEmitTmuxEscapes(t *testing.T) {
	// The regression this guards: `--color` used to emit tmux syntax
	// unconditionally, so a Zellij bar or a shell prompt showed a literal
	// "#[fg=#9ece6a]" next to the number.
	t.Setenv("TMUX", "")
	got, err := Render(usage.Snapshot{FiveHour: bucket(42, time.Hour)}, nil,
		Options{Colour: ColourAuto, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "#[fg=") {
		t.Errorf("tmux escapes leaked outside tmux: %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("expected ANSI colour outside tmux, got %q", got)
	}
}

func TestColourThresholdsApplyInBothSyntaxes(t *testing.T) {
	for _, tc := range []struct {
		util       float64
		tmuxColour string
		ansiColour string
	}{
		{10, colourOK, ansiOK},
		{70, colourWarn, ansiWarn},
		{95, colourCrit, ansiCrit},
	} {
		snap := usage.Snapshot{FiveHour: bucket(tc.util, time.Hour)}

		got, _ := Render(snap, nil, Options{Colour: ColourTmux, Now: fixedNow})
		if !strings.Contains(got, tc.tmuxColour) {
			t.Errorf("tmux %.0f%%: got %q want %s", tc.util, got, tc.tmuxColour)
		}
		got, _ = Render(snap, nil, Options{Colour: ColourANSI, Now: fixedNow})
		if !strings.Contains(got, tc.ansiColour) {
			t.Errorf("ansi %.0f%%: got %q want SGR", tc.util, got)
		}
	}
}
