package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fullfran/claudeops-tui/internal/agy"
	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/statusline"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

func cmdAgy(args []string) error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	return cmdAgyWith(p, agyDefaultRoot(), os.Stdin, os.Stdout, args)
}

// cmdAgyWith routes the agy subcommands. It never opens the agy settings
// file itself — each handler below does that with the exact path it needs —
// so every path here is a parameter, and a test can point agyRoot at a temp
// dir without ever touching a real ~/.gemini.
func cmdAgyWith(p config.Paths, agyRoot string, in io.Reader, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("agy: missing subcommand (statusline|setup|remove|status)")
	}
	settingsPath := filepath.Join(agyRoot, "settings.json")
	switch args[0] {
	case "statusline":
		return cmdAgyStatusline(p, in, out)
	case "setup":
		return cmdAgySetup(settingsPath, out, args[1:])
	case "remove":
		return cmdAgyRemove(settingsPath, out)
	case "status":
		return cmdAgyStatus(p, settingsPath, out)
	default:
		return fmt.Errorf("agy: unknown subcommand %q", args[0])
	}
}

// cmdAgyStatusline is the command agy itself runs on every agent-state
// change, wired via its own `statusLine` setting. Reads the documented
// status-line payload from stdin, persists whatever quota it carries, and
// prints one status-line-formatted line.
//
// Contract: quiet failure, exit zero, always. agy's prompt is not a place to
// learn that stdin was malformed or that a write failed — see
// cmdStatuslineWith's doc comment for the same reasoning applied to the
// Anthropic status line.
func cmdAgyStatusline(p config.Paths, in io.Reader, out io.Writer) error {
	settings, _ := config.Load(p.ConfigPath) // missing file yields defaults

	payload, err := agy.ParseStatuslinePayload(in)
	if err != nil {
		return nil
	}

	now := time.Now()
	snap := agy.QuotaSnapshot{}
	haveSnap := false
	// Only a payload that actually carries a quota reading is worth
	// persisting: an update with none (e.g. a plain agent_state change) must
	// not overwrite the last good reading with nothing.
	if len(payload.Quota) > 0 {
		snap = agy.SnapshotFromPayload(payload, now)
		if err := agy.WriteQuotaSnapshot(p.AntigravityQuotaPath, snap); err != nil {
			return nil
		}
		haveSnap = true
	}

	// Persisting happens unconditionally above — the TUI reads that snapshot
	// independently of whether the status line itself is turned on. Only the
	// printed line is gated.
	if !settings.Statusline.IsEnabled() {
		return nil
	}

	if !haveSnap {
		snap, err = agy.ReadQuotaSnapshot(p.AntigravityQuotaPath)
		if err != nil {
			return nil
		}
	}

	u := provider.AntigravityUsage(snap, now)
	line, err := statusline.Render(usage.Snapshot{}, []provider.Usage{u}, statusline.Options{
		Format:   statusline.FormatCompact,
		Provider: "antigravity",
	})
	if err != nil || line == "" {
		return nil
	}
	_, _ = fmt.Fprintln(out, line)
	return nil
}

// agyStatuslineCommand is the exact command `agy setup` wires into agy's
// settings.json, and what every ownership check below compares against.
func agyStatuslineCommand() (string, error) {
	bin, err := resolveBinary()
	if err != nil {
		return "", err
	}
	return bin + " agy statusline", nil
}

// cmdAgySetup points agy's own statusLine at claudeops, preserving every
// other key in its settings.json.
func cmdAgySetup(settingsPath string, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("agy setup", flag.ContinueOnError)
	fs.SetOutput(out)
	force := fs.Bool("force", false, "replace a statusLine command that is not claudeops's own")
	if err := fs.Parse(args); err != nil {
		return err
	}

	command, err := agyStatuslineCommand()
	if err != nil {
		return err
	}

	s, mode, err := agy.LoadSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("agy setup: %w", err)
	}
	if s.StatusLine != nil && !s.StatusLine.IsClaudeops(command) && !*force {
		return fmt.Errorf("agy setup: a different status line is already configured (type=%q, command=%q); rerun with --force to replace it",
			s.StatusLine.Type, s.StatusLine.Command)
	}

	s.StatusLine = &agy.StatusLineEntry{Type: "command", Command: command}
	if err := agy.SaveSettings(settingsPath, s, mode); err != nil {
		return fmt.Errorf("agy setup: %w", err)
	}
	_, _ = fmt.Fprintf(out, "agy status line set to: %s\n", command)
	_, _ = fmt.Fprintf(out, "settings: %s\n", settingsPath)
	return nil
}

// cmdAgyRemove deletes the statusLine key, but only when it is the one
// claudeops itself would set. A statusLine belonging to something else, or
// no statusLine at all, is left exactly as found.
func cmdAgyRemove(settingsPath string, out io.Writer) error {
	command, err := agyStatuslineCommand()
	if err != nil {
		return err
	}

	s, mode, err := agy.LoadSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("agy remove: %w", err)
	}
	if !s.StatusLine.IsClaudeops(command) {
		_, _ = fmt.Fprintln(out, "agy status line is not set to claudeops; nothing removed")
		return nil
	}

	s.StatusLine = nil
	if err := agy.SaveSettings(settingsPath, s, mode); err != nil {
		return fmt.Errorf("agy remove: %w", err)
	}
	_, _ = fmt.Fprintf(out, "removed the claudeops status line from %s\n", settingsPath)
	return nil
}

// cmdAgyStatus reports whether agy is wired up to claudeops and the freshness
// of the last quota reading it sent.
func cmdAgyStatus(p config.Paths, settingsPath string, out io.Writer) error {
	command, err := agyStatuslineCommand()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "settings:    %s\n", settingsPath)
	s, _, err := agy.LoadSettings(settingsPath)
	switch {
	case err != nil:
		_, _ = fmt.Fprintf(out, "status line: unreadable (%v)\n", err)
	case s.StatusLine.IsClaudeops(command):
		_, _ = fmt.Fprintln(out, "status line: wired to claudeops")
	case s.StatusLine != nil:
		_, _ = fmt.Fprintf(out, "status line: not claudeops (type=%q, command=%q)\n", s.StatusLine.Type, s.StatusLine.Command)
	default:
		_, _ = fmt.Fprintln(out, "status line: not configured")
	}

	snap, err := agy.ReadQuotaSnapshot(p.AntigravityQuotaPath)
	if err != nil {
		_, _ = fmt.Fprintln(out, "quota:       none received yet")
		return nil
	}
	age := time.Since(snap.ObservedAt).Round(time.Second)
	_, _ = fmt.Fprintf(out, "quota:       observed %s ago\n", age)
	for _, w := range provider.AntigravityUsage(snap, time.Now()).Windows {
		_, _ = fmt.Fprintf(out, "  %-16s %5.2f%% used", w.Label, w.Utilization)
		if !w.ResetsAt.IsZero() {
			_, _ = fmt.Fprintf(out, "  resets in %s", statusline.ShortDuration(time.Until(w.ResetsAt)))
		}
		_, _ = fmt.Fprintln(out)
	}
	return nil
}
