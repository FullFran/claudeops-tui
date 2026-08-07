package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/fullfran/claudeops-tui/internal/buildinfo"
	selfupdate "github.com/fullfran/claudeops-tui/internal/update"
)

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "report whether an update is available without installing it")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Printf("claudeops current version: %s\n", version)

	updater := selfupdate.New(version)
	updater.SourceBuild = buildinfo.FromSource()

	if *check {
		decision, err := updater.Decide(ctx)
		if err != nil {
			return err
		}
		if decision.ExecutablePath != "" {
			fmt.Printf("installed at: %s\n", decision.ExecutablePath)
		}
		switch {
		case decision.LatestVersion == "" && decision.Method == selfupdate.MethodBinary:
			// A binary update has to know which archive to fetch. Without a
			// version there is nothing to try, so "try anyway" would send the
			// user to a guaranteed dead end.
			fmt.Println("could not reach the release index, so there is nothing to download")
			fmt.Printf("check your connection, or download manually: %s\n", selfupdate.ReleasesPage)
		case decision.LatestVersion == "":
			fmt.Println("could not reach the module proxy; run `claudeops update` to try anyway")
		case decision.Downgrade:
			fmt.Printf("published: %s — older than what you are running\n", decision.LatestVersion)
			fmt.Println("this build is ahead of the last release; updating would move you backwards")
		case decision.UpToDate:
			fmt.Println("already up to date")
		default:
			fmt.Printf("update available: %s\n", decision.LatestVersion)
			fmt.Printf("  %s\n", decision.InstallCommand)
		}
		if decision.Method != "" {
			fmt.Printf("update method: %s\n", updateMethodLabel(decision.Method))
		}
		if !decision.CanAuto && decision.Reason != "" {
			fmt.Printf("note: automatic update unavailable — %s\n", decision.Reason)
		}
		return nil
	}

	decision, err := updater.Update(ctx)
	if decision.ExecutablePath != "" {
		fmt.Printf("installed at: %s\n", decision.ExecutablePath)
	}

	if err == nil {
		// UpToDate is decided before installing now, so this costs nothing.
		if decision.UpToDate || (decision.InstalledNow != "" && decision.InstalledNow == "claudeops "+version) {
			fmt.Println("already up to date")
			return nil
		}
		if decision.Method == selfupdate.MethodBinary {
			// InstalledPath, not ExecutablePath: a symlinked install has the
			// release written to the link's target, and naming the link would
			// point at a file that was never touched.
			fmt.Printf("replaced %s with the published release\n", decision.InstalledPath)
		} else {
			fmt.Printf("update command: %s\n", decision.InstallCommand)
		}
		fmt.Println("update complete")
		if decision.InstalledNow != "" {
			fmt.Printf("installed version: %s\n", decision.InstalledNow)
		} else {
			fmt.Println("installed version: unable to verify automatically; run `claudeops version`")
		}
		return nil
	}

	if errors.Is(err, selfupdate.ErrStaleRelease) {
		fmt.Println("nothing installed — your binary was left alone")
		fmt.Println("this happens on a build made between releases; wait for the next tag, or:")
		// GOPROXY is a Go toolchain setting. Prefixing it onto a binary-update
		// command — or onto the releases URL, which is what InstallCommand holds
		// when the install directory is not writable — produces nonsense.
		if decision.Method == selfupdate.MethodBinary {
			fmt.Printf("  download the release you want: %s\n", selfupdate.ReleasesPage)
		} else {
			fmt.Printf("  GOPROXY=direct %s\n", decision.InstallCommand)
		}
		return err
	}

	if errors.Is(err, selfupdate.ErrManual) {
		fmt.Println("automatic update is not available for this installation")
		if decision.Reason != "" {
			fmt.Printf("reason: %s\n", decision.Reason)
		}
		fmt.Println("manual update:")
		if decision.Method == selfupdate.MethodBinary && decision.InstallCommand == "claudeops update" {
			// That is the command that just failed. Sending the user back to it
			// is not instructions, it is a loop.
			fmt.Printf("  %s\n", selfupdate.ReleasesPage)
		} else {
			fmt.Printf("  %s\n", decision.InstallCommand)
		}
		// The GOPROXY and PATH advice below is about `go install`, and telling
		// it to someone updating a downloaded binary is how they end up with a
		// second copy in GOBIN while the one they run stays where it was.
		if decision.Method != selfupdate.MethodBinary {
			fmt.Println("if `@latest` still resolves to an older commit, retry with:")
			fmt.Printf("  GOPROXY=direct %s\n", decision.InstallCommand)
			fmt.Println("if `claudeops` is not on PATH afterwards, add `$(go env GOPATH)/bin` or your `GOBIN` to PATH")
		}
	}

	return err
}

// updateMethodLabel explains a method in the terms a user cares about: where
// the new binary comes from and what it will touch.
func updateMethodLabel(m selfupdate.Method) string {
	if m == selfupdate.MethodBinary {
		return "replace this binary with the published release archive"
	}
	return "go install (this binary is managed by the Go toolchain)"
}
