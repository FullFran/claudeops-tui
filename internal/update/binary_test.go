package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestAssetName(t *testing.T) {
	tests := []struct {
		name    string
		version string
		goos    string
		goarch  string
		want    string
	}{
		{name: "linux amd64", version: "0.14.0", goos: "linux", goarch: "amd64", want: "claudeops_0.14.0_linux_amd64.tar.gz"},
		{name: "darwin arm64", version: "0.14.0", goos: "darwin", goarch: "arm64", want: "claudeops_0.14.0_darwin_arm64.tar.gz"},
		{name: "windows is a zip", version: "0.14.0", goos: "windows", goarch: "amd64", want: "claudeops_0.14.0_windows_amd64.zip"},
		{name: "leading v is dropped", version: "v0.14.0", goos: "linux", goarch: "arm64", want: "claudeops_0.14.0_linux_arm64.tar.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AssetName(tt.version, tt.goos, tt.goarch); got != tt.want {
				t.Fatalf("AssetName(%q, %q, %q) = %q, want %q", tt.version, tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

// releaseServer serves a checksums.txt and archive pair the way GitHub does.
// corrupt makes the served archive disagree with the checksum it publishes.
func releaseServer(t *testing.T, version, payload string, corrupt bool) *httptest.Server {
	t.Helper()

	asset := AssetName(version, runtime.GOOS, runtime.GOARCH)
	archive := buildArchive(t, payload)
	sum := sha256.Sum256(archive)
	if corrupt {
		archive = append(archive, 'x')
	}
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)

	mux := http.NewServeMux()
	mux.HandleFunc("/v"+version+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	mux.HandleFunc("/v"+version+"/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// buildArchive produces the archive shape GoReleaser publishes for this
// platform, carrying a binary whose contents are payload.
func buildArchive(t *testing.T, payload string) []byte {
	t.Helper()
	var buf bytes.Buffer

	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("claudeops.exe")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// README and LICENSE ship alongside the binary; the installer must pick the
	// binary out rather than take the first entry.
	for _, f := range []struct{ name, body string }{
		{"README.md", "readme"},
		{"LICENSE", "license"},
		{"claudeops", payload},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBinaryInstallerReplacesTheRunningBinary(t *testing.T) {
	server := releaseServer(t, "0.14.0", "new binary", false)

	dir := t.TempDir()
	target := filepath.Join(dir, "claudeops")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	inst := BinaryInstaller{DownloadBase: server.URL}
	if err := inst.Install(context.Background(), "0.14.0", target); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Fatalf("binary contents = %q, want %q", got, "new binary")
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("replaced binary is not executable: %v", info.Mode())
	}

	// Nothing may be left behind next to the binary.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "claudeops" {
			t.Fatalf("leftover file in install dir: %s", e.Name())
		}
	}
}

// A checksum that does not match means the download is not what was published.
// The binary on disk must survive that untouched.
func TestBinaryInstallerRefusesAChecksumMismatch(t *testing.T) {
	server := releaseServer(t, "0.14.0", "new binary", true)

	dir := t.TempDir()
	target := filepath.Join(dir, "claudeops")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	inst := BinaryInstaller{DownloadBase: server.URL}
	err := inst.Install(context.Background(), "0.14.0", target)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want ErrChecksumMismatch", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old binary" {
		t.Fatalf("binary was modified despite a bad checksum: %q", got)
	}
}

func TestBinaryInstallerReportsAMissingAsset(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	dir := t.TempDir()
	target := filepath.Join(dir, "claudeops")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	inst := BinaryInstaller{DownloadBase: server.URL}
	err := inst.Install(context.Background(), "0.14.0", target)
	if err == nil {
		t.Fatal("expected an error when the release assets are missing")
	}
	if !strings.Contains(err.Error(), "checksums.txt") {
		t.Fatalf("error should name what could not be fetched, got: %v", err)
	}
}

// Installing into a directory the user cannot write is the common case for
// /usr/local/bin. It must be reported as needing manual action, before anything
// is downloaded, and never by escalating privileges on the user's behalf.
func TestWritableReportsAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	if runtime.GOOS == "windows" {
		t.Skip("unix permission semantics")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "claudeops")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := Writable(target); err == nil {
		t.Fatal("expected an error for a read-only install directory")
	}
}

func TestWritableAcceptsAWritableDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "claudeops")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Writable(target); err != nil {
		t.Fatalf("expected a writable directory to pass, got: %v", err)
	}
	// The probe must not leave anything behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("probe left files behind: %d entries", len(entries))
	}
}

// The whole design rests on this: `claudeops update` replaces the binary that
// is executing it. Writing over the file in place would fail with ETXTBSY, so
// the new binary is renamed on top of it — which swaps the directory entry and
// leaves the running process on its original inode.
func TestReplaceExecutableWorksOnARunningBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows cannot rename over a running executable; that path moves it aside instead")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary to stand in for a running claudeops")
	}
	source, err := os.ReadFile(sleep)
	if err != nil {
		t.Skip("cannot read sleep binary")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "running")
	if err := os.WriteFile(target, source, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(target, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	if err := replaceExecutable(target, []byte("new binary")); err != nil {
		t.Fatalf("could not replace a running binary: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Fatalf("contents = %q, want %q", got, "new binary")
	}

	// The process that was running must be undisturbed by the swap.
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("the running process died when its binary was replaced: %v", err)
	}
}

// A restrictive installation must not be quietly widened by an update.
func TestReplaceExecutablePreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission semantics")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "claudeops")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutable(target, []byte("new")); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("permissions = %v, want -rwx------", info.Mode().Perm())
	}
}

func TestParseChecksums(t *testing.T) {
	in := "abc123  claudeops_0.14.0_linux_amd64.tar.gz\ndef456  claudeops_0.14.0_darwin_arm64.tar.gz\n\n"
	got := parseChecksums(in)
	if got["claudeops_0.14.0_linux_amd64.tar.gz"] != "abc123" {
		t.Fatalf("linux sum = %q", got["claudeops_0.14.0_linux_amd64.tar.gz"])
	}
	if got["claudeops_0.14.0_darwin_arm64.tar.gz"] != "def456" {
		t.Fatalf("darwin sum = %q", got["claudeops_0.14.0_darwin_arm64.tar.gz"])
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
}
