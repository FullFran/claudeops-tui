package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

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

	if *check {
		decision, err := updater.Decide(ctx)
		if err != nil {
			return err
		}
		if decision.ExecutablePath != "" {
			fmt.Printf("installed at: %s\n", decision.ExecutablePath)
		}
		switch {
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
		fmt.Printf("update command: %s\n", decision.InstallCommand)
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
		fmt.Printf("  GOPROXY=direct %s\n", decision.InstallCommand)
		return err
	}

	if errors.Is(err, selfupdate.ErrManual) {
		fmt.Println("automatic update is not available for this installation")
		if decision.Reason != "" {
			fmt.Printf("reason: %s\n", decision.Reason)
		}
		fmt.Println("manual update:")
		fmt.Printf("  %s\n", decision.InstallCommand)
		fmt.Println("if `@latest` still resolves to an older commit, retry with:")
		fmt.Printf("  GOPROXY=direct %s\n", decision.InstallCommand)
		fmt.Println("if `claudeops` is not on PATH afterwards, add `$(go env GOPATH)/bin` or your `GOBIN` to PATH")
	}

	return err
}
