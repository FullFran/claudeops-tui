package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/buildinfo"
)

// The first line of `claudeops version` is a contract, not cosmetics:
// internal/update runs the freshly installed binary and parses that line to
// confirm the proxy did not serve a stale release. Build metadata goes on the
// lines below it, where it cannot be mistaken for the version.
func TestRunArgsVersionPrintsReleaseVersion(t *testing.T) {
	got := captureStdout(t, func() error {
		return runArgs([]string{"version"})
	})
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	want := "claudeops " + version
	if lines[0] != want {
		t.Fatalf("first line = %q, want %q", lines[0], want)
	}
	if version == "" {
		t.Fatal("version must not be empty")
	}
	if len(lines) != 3 {
		t.Fatalf("version output = %q, want 3 lines", got)
	}
	if !strings.HasPrefix(lines[1], "commit: ") {
		t.Fatalf("second line = %q, want a commit line", lines[1])
	}
	if !strings.HasPrefix(lines[2], "built: ") {
		t.Fatalf("third line = %q, want a built line", lines[2])
	}
}

// A `go build` of this source tree reports the version baked into buildinfo.
// If those two ever disagree the release workflow's tag check is checking the
// wrong thing.
func TestVersionComesFromBuildinfo(t *testing.T) {
	if version != buildinfo.Version() {
		t.Fatalf("version = %q, want buildinfo.Version() = %q", version, buildinfo.Version())
	}
}

func captureStdout(t *testing.T, run func() error) string {
	t.Helper()
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = previous
		_ = reader.Close()
		_ = writer.Close()
	})

	if err := run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestRunArgsDispatchesUpdateCommand(t *testing.T) {
	called := false
	var gotArgs []string
	prev := runUpdateCommand
	runUpdateCommand = func(args []string) error {
		called = true
		gotArgs = args
		return nil
	}
	defer func() { runUpdateCommand = prev }()

	if err := runArgs([]string{"update"}); err != nil {
		t.Fatalf("runArgs(update): %v", err)
	}
	if len(gotArgs) != 0 {
		t.Errorf("bare `update` should pass no flags, got %v", gotArgs)
	}

	// Flags have to reach the subcommand, or --check silently does nothing.
	gotArgs = nil
	if err := runArgs([]string{"update", "--check"}); err != nil {
		t.Fatalf("runArgs(update --check): %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "--check" {
		t.Errorf("flags not forwarded: %v", gotArgs)
	}
	if !called {
		t.Fatal("expected update command to be called")
	}
}

func TestRunArgsDispatchesReingestCommand(t *testing.T) {
	var gotArgs []string
	prev := runReingestCommand
	runReingestCommand = func(args []string) error {
		gotArgs = args
		return nil
	}
	defer func() { runReingestCommand = prev }()

	if err := runArgs([]string{"reingest", "--yes"}); err != nil {
		t.Fatalf("runArgs(reingest): %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "--yes" {
		t.Fatalf("expected reingest to receive [--yes], got %v", gotArgs)
	}
}

func TestRunArgsDispatchesPushCommand(t *testing.T) {
	called := false
	prev := runPushCommand
	runPushCommand = func(args []string) error {
		called = true
		return nil
	}
	defer func() { runPushCommand = prev }()

	if err := runArgs([]string{"push"}); err != nil {
		t.Fatalf("runArgs(push): %v", err)
	}
	if !called {
		t.Fatal("expected push command to be called")
	}
}

func TestRunArgsPushDryRunFlag(t *testing.T) {
	var gotArgs []string
	prev := runPushCommand
	runPushCommand = func(args []string) error {
		gotArgs = args
		return nil
	}
	defer func() { runPushCommand = prev }()

	if err := runArgs([]string{"push", "--dry-run"}); err != nil {
		t.Fatalf("runArgs(push --dry-run): %v", err)
	}
	found := false
	for _, a := range gotArgs {
		if a == "--dry-run" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --dry-run in args, got %v", gotArgs)
	}
}

func TestRunArgsPushSinceFlag(t *testing.T) {
	var gotArgs []string
	prev := runPushCommand
	runPushCommand = func(args []string) error {
		gotArgs = args
		return nil
	}
	defer func() { runPushCommand = prev }()

	since := "2026-01-01T00:00:00Z"
	if err := runArgs([]string{"push", "--since", since}); err != nil {
		t.Fatalf("runArgs(push --since): %v", err)
	}
	found := false
	for i, a := range gotArgs {
		if a == "--since" && i+1 < len(gotArgs) && gotArgs[i+1] == since {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --since %s in args, got %v", since, gotArgs)
	}
}

func TestRunArgsPushBadSince(t *testing.T) {
	// cmdPush should return error for bad --since value.
	// We do NOT mock here — we let it call the real cmdPush with an isolated fake
	// by swapping runPushCommand back to a wrapper that validates --since.
	// Since cmdPush does flag parsing, test via a dedicated unit test instead.
	if err := cmdPush([]string{"--since", "not-a-date"}); err == nil {
		t.Fatal("expected error for invalid --since date")
	}
}

func TestRunArgsDispatchesOTelConfigCommand(t *testing.T) {
	called := false
	prev := runOTelConfigCommand
	runOTelConfigCommand = func(args []string) error {
		called = true
		return nil
	}
	defer func() { runOTelConfigCommand = prev }()

	if err := runArgs([]string{"otel-config", "apply"}); err != nil {
		t.Fatalf("runArgs(otel-config apply): %v", err)
	}
	if !called {
		t.Fatal("expected otel-config command to be called")
	}
}

func TestRunArgsOTelConfigStatus(t *testing.T) {
	called := false
	prev := runOTelConfigCommand
	runOTelConfigCommand = func(args []string) error {
		called = true
		return nil
	}
	defer func() { runOTelConfigCommand = prev }()

	if err := runArgs([]string{"otel-config", "status"}); err != nil {
		t.Fatalf("runArgs(otel-config status): %v", err)
	}
	if !called {
		t.Fatal("expected otel-config command to be called for status")
	}
}

func TestRunArgsOTelConfigRemove(t *testing.T) {
	called := false
	prev := runOTelConfigCommand
	runOTelConfigCommand = func(args []string) error {
		called = true
		return nil
	}
	defer func() { runOTelConfigCommand = prev }()

	if err := runArgs([]string{"otel-config", "remove"}); err != nil {
		t.Fatalf("runArgs(otel-config remove): %v", err)
	}
	if !called {
		t.Fatal("expected otel-config command to be called for remove")
	}
}

func TestRunArgsOTelConfigNoSubcommand(t *testing.T) {
	// When no subcommand is given, cmdOTelConfig should return an error.
	// We bypass the var-swap and test cmdOTelConfig directly.
	if err := cmdOTelConfig([]string{}); err == nil {
		t.Fatal("expected error for otel-config with no subcommand")
	}
}
