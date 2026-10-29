package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/fullfran/claudeops-tui/internal/pi"
)

func cmdPi(args []string) error {
	return cmdPiWith(os.Stdout, args)
}

func cmdPiWith(out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("pi: missing subcommand (setup|install|remove|uninstall|status)")
	}
	switch args[0] {
	case "setup", "install":
		return cmdPiSetup(out, args[1:])
	case "remove", "uninstall":
		return cmdPiRemove(out)
	case "status":
		return cmdPiStatus(out)
	default:
		return fmt.Errorf("pi: unknown subcommand %q", args[0])
	}
}

func cmdPiSetup(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("pi setup", flag.ContinueOnError)
	fs.SetOutput(out)
	force := fs.Bool("force", false, "overwrite existing extension file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	installed, err := pi.InstallExtension(*force)
	if err != nil {
		return fmt.Errorf("pi setup: %w", err)
	}
	if len(installed) == 0 {
		_, _ = fmt.Fprintln(out, "no Pi or Gentle Shell directories found to install extension")
		return nil
	}
	for _, path := range installed {
		_, _ = fmt.Fprintf(out, "installed claudeops Pi extension: %s\n", path)
	}
	_, _ = fmt.Fprintln(out, "extension active on next Pi / Gentle Shell startup (/claudeops command available)")
	return nil
}

func cmdPiRemove(out io.Writer) error {
	removed, err := pi.UninstallExtension()
	if err != nil {
		return fmt.Errorf("pi remove: %w", err)
	}
	if len(removed) == 0 {
		_, _ = fmt.Fprintln(out, "no installed claudeops Pi extension found")
		return nil
	}
	for _, path := range removed {
		_, _ = fmt.Fprintf(out, "removed claudeops Pi extension: %s\n", path)
	}
	return nil
}

func cmdPiStatus(out io.Writer) error {
	statuses := pi.ExtensionStatus()
	installedCount := 0
	for _, s := range statuses {
		if s.Installed {
			installedCount++
			_, _ = fmt.Fprintf(out, "installed:   %s (%d bytes)\n", s.Path, s.Size)
		} else {
			_, _ = fmt.Fprintf(out, "not present: %s\n", s.Path)
		}
	}
	if installedCount == 0 {
		_, _ = fmt.Fprintln(out, "status:      not installed in any candidate directory")
	} else {
		_, _ = fmt.Fprintf(out, "status:      active (%d extension file(s) installed)\n", installedCount)
	}
	return nil
}
