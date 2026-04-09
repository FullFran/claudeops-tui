package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	selfupdate "github.com/fullfran/claudeops-tui/internal/update"
)

func cmdUpdate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Printf("claudeops current version: %s\n", version)

	decision, err := selfupdate.New(version).Update(ctx)
	if decision.ExecutablePath != "" {
		fmt.Printf("installed at: %s\n", decision.ExecutablePath)
	}

	if err == nil {
		fmt.Printf("update command: %s\n", decision.InstallCommand)
		fmt.Println("update complete")
		if decision.InstalledNow != "" {
			fmt.Printf("installed version: %s\n", decision.InstalledNow)
		} else {
			fmt.Println("installed version: unable to verify automatically; run `claudeops version`")
		}
		return nil
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
