package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/familiars"
)

const familiardInstallTimeout = 5 * time.Minute

// familiardDir is the only place claudeops ever puts familiard.
func familiardDir(p config.Paths) string {
	return filepath.Join(p.DataDir, "bin")
}

func cmdFamiliard(args []string) error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	return cmdFamiliardWith(os.Stdout, args, familiardDir(p), func(out io.Writer, dir string) familiars.Installer {
		return familiars.Installer{
			Dir: dir, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Log: out,
			HTTP:      &http.Client{Timeout: familiardInstallTimeout},
			LatestURL: familiars.LatestRelease, FilesURL: familiars.ReleaseFiles,
			AddDemo: addDemoIfEmpty,
		}
	})
}

func cmdFamiliardWith(out io.Writer, args []string, dir string, installer func(io.Writer, string) familiars.Installer) error {
	if len(args) != 1 {
		return fmt.Errorf("familiard: expected one subcommand (install|status|remove)")
	}
	own := filepath.Join(dir, familiars.ProgramName())
	found := familiars.Find(dir)
	mine := found != nil && found.Path == own
	switch args[0] {
	case "install":
		if found != nil && !mine {
			fmt.Fprintf(out, "you already have familiard at %s; the Classroom tab will use it\n", found.Path)
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), familiardInstallTimeout)
		defer cancel()
		path, err := installer(out, dir).Install(ctx)
		if err != nil {
			return fmt.Errorf("familiard: %w", err)
		}
		fmt.Fprintf(out, "installed %s\nopen the Classroom tab to see your familiars\n", path)
		return nil
	case "status":
		if found == nil {
			fmt.Fprintln(out, "familiard is not installed; the Classroom tab shows monkeys (claudeops familiard install)")
			return nil
		}
		fmt.Fprintf(out, "using %s (%s)\n", found.Path, familiardVersion(found.Path))
		return nil
	case "remove", "uninstall":
		if !mine {
			if found != nil {
				fmt.Fprintf(out, "%s was not installed by claudeops, so it stays\n", found.Path)
			} else {
				fmt.Fprintln(out, "claudeops has not installed familiard")
			}
			return nil
		}
		if err := os.Remove(own); err != nil {
			return fmt.Errorf("familiard: %w", err)
		}
		fmt.Fprintf(out, "removed %s; your pets are still in familiard's gallery\n", own)
		return nil
	default:
		return fmt.Errorf("familiard: unknown subcommand %q", args[0])
	}
}

func familiardVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "version unknown"
	}
	return strings.TrimSpace(string(out))
}

// addDemoIfEmpty gives a new gallery one pet, and never touches one that already has some.
func addDemoIfEmpty(ctx context.Context, program, demo string) error {
	pets, err := familiars.NewClient(program).List(ctx)
	if err != nil {
		return err
	}
	if len(pets) > 0 {
		return nil
	}
	return exec.CommandContext(ctx, program, "add", demo).Run()
}
