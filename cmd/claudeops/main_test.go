package main

import (
	"testing"
)

func TestRunArgsDispatchesUpdateCommand(t *testing.T) {
	called := false
	prev := runUpdateCommand
	runUpdateCommand = func() error {
		called = true
		return nil
	}
	defer func() { runUpdateCommand = prev }()

	if err := runArgs([]string{"update"}); err != nil {
		t.Fatalf("runArgs(update): %v", err)
	}
	if !called {
		t.Fatal("expected update command to be called")
	}
}
