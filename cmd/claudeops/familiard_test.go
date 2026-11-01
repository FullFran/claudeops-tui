package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/familiars"
)

// noDownloads fails the test if a command tries to install anything.
func noDownloads(t *testing.T) func(io.Writer, string) familiars.Installer {
	return func(io.Writer, string) familiars.Installer {
		t.Fatal("this must not download anything")
		return familiars.Installer{}
	}
}

func program(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, familiars.ProgramName())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho familiard 9.9.9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstallLeavesTheUsersOwnFamiliardAlone(t *testing.T) {
	onPath := t.TempDir()
	path := program(t, onPath)
	t.Setenv("PATH", onPath)
	dir := t.TempDir()
	var out bytes.Buffer
	if err := cmdFamiliardWith(&out, []string{"install"}, dir, noDownloads(t)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "you already have familiard at "+path) {
		t.Errorf("got %q", out.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("nothing should be written: %v", entries)
	}
}

func TestRemoveOnlyTouchesWhatClaudeopsInstalled(t *testing.T) {
	onPath := t.TempDir()
	path := program(t, onPath)
	t.Setenv("PATH", onPath)
	var out bytes.Buffer
	if err := cmdFamiliardWith(&out, []string{"remove"}, t.TempDir(), noDownloads(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the user's own familiard is gone: %v", err)
	}
	if !strings.Contains(out.String(), "was not installed by claudeops") {
		t.Errorf("got %q", out.String())
	}

	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	mine := program(t, dir)
	out.Reset()
	if err := cmdFamiliardWith(&out, []string{"remove"}, dir, noDownloads(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Errorf("ours should be gone: %v", err)
	}
	out.Reset()
	if err := cmdFamiliardWith(&out, []string{"remove"}, dir, noDownloads(t)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "has not installed") {
		t.Errorf("got %q", out.String())
	}
}

func TestStatusSaysWhichOneIsUsed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake program is a shell script")
	}
	dir := t.TempDir()
	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell")
	}
	emptyPath := t.TempDir()
	t.Setenv("PATH", emptyPath)
	var out bytes.Buffer
	if err := cmdFamiliardWith(&out, []string{"status"}, dir, noDownloads(t)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not installed") {
		t.Errorf("got %q", out.String())
	}
	path := program(t, dir)
	out.Reset()
	if err := cmdFamiliardWith(&out, []string{"status"}, dir, noDownloads(t)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "using "+path+" (") {
		t.Errorf("got %q", out.String())
	}
}

func TestFamiliardCommandRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"install", "extra"}} {
		if err := cmdFamiliardWith(io.Discard, args, t.TempDir(), noDownloads(t)); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
}
