package statusline

import (
	"fmt"
	"os"
	"strings"
)

// ColourMode selects how colour is emitted, because the escapes are not
// portable: tmux understands its own `#[fg=...]` syntax and nothing else does,
// while every other consumer wants ANSI.
//
// Emitting tmux escapes outside tmux is the failure this type exists to
// prevent — they render as literal `#[fg=#9ece6a]` text in a Zellij bar or a
// shell prompt.
type ColourMode string

const (
	// ColourNone emits no escapes at all.
	ColourNone ColourMode = "none"
	// ColourTmux emits tmux `#[fg=...]` style.
	ColourTmux ColourMode = "tmux"
	// ColourANSI emits SGR escapes, for Zellij, shell prompts and anything else.
	ColourANSI ColourMode = "ansi"
	// ColourAuto picks tmux inside tmux and ANSI elsewhere.
	ColourAuto ColourMode = "auto"
)

// ParseColourMode accepts the flag's values, including the booleans Go's flag
// package produces for a bare `--color`.
func ParseColourMode(v string) (ColourMode, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "false", "0", "off":
		return ColourNone, nil
	case "true", "1", "on", "auto":
		// A bare `--color` means "colour this appropriately", not "assume tmux".
		return ColourAuto, nil
	case "tmux":
		return ColourTmux, nil
	case "ansi":
		return ColourANSI, nil
	default:
		return ColourNone, fmt.Errorf("unknown colour mode %q: want none, auto, tmux or ansi", v)
	}
}

// resolve turns auto into a concrete mode.
func (m ColourMode) resolve() ColourMode {
	if m != ColourAuto {
		return m
	}
	if os.Getenv("TMUX") != "" {
		return ColourTmux
	}
	return ColourANSI
}

// ANSI equivalents of the tmux palette, as 24-bit SGR.
const (
	ansiOK   = "\x1b[38;2;158;206;106m"
	ansiWarn = "\x1b[38;2;224;175;104m"
	ansiCrit = "\x1b[38;2;247;118;142m"
	ansiOff  = "\x1b[0m"
)

// wrap colours one segment according to the mode.
func (m ColourMode) wrap(text string, level colourLevel) string {
	switch m.resolve() {
	case ColourTmux:
		return "#[fg=" + level.tmux() + "]" + text + colourOff
	case ColourANSI:
		return level.ansi() + text + ansiOff
	default:
		return text
	}
}

type colourLevel int

const (
	levelOK colourLevel = iota
	levelWarn
	levelCrit
)

func (l colourLevel) tmux() string {
	switch l {
	case levelCrit:
		return colourCrit
	case levelWarn:
		return colourWarn
	default:
		return colourOK
	}
}

func (l colourLevel) ansi() string {
	switch l {
	case levelCrit:
		return ansiCrit
	case levelWarn:
		return ansiWarn
	default:
		return ansiOK
	}
}

// colourFlag adapts ColourMode to Go's flag package. Implementing IsBoolFlag
// lets `--color` work with no value while `--color=ansi` still parses, so the
// original boolean spelling keeps working.
type colourFlag struct{ mode *ColourMode }

func (f colourFlag) String() string {
	if f.mode == nil {
		return string(ColourNone)
	}
	return string(*f.mode)
}

func (f colourFlag) Set(v string) error {
	m, err := ParseColourMode(v)
	if err != nil {
		return err
	}
	*f.mode = m
	return nil
}

func (colourFlag) IsBoolFlag() bool { return true }

// ColourFlagValue returns a flag.Value writing into mode.
func ColourFlagValue(mode *ColourMode) interface {
	String() string
	Set(string) error
} {
	return colourFlag{mode: mode}
}
