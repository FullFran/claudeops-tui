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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Everyone who installs claudeops from a release archive was, until now, told
// to update with `go install`. That advice is wrong twice over: it needs the Go
// toolchain they deliberately avoided, and it writes to GOBIN — a second copy,
// somewhere else, leaving the binary actually on their PATH untouched at the
// old version. The update appears to succeed and changes nothing.
//
// This installs the published archive over the running binary instead: resolve
// the asset for this platform, verify it against the release's checksums.txt,
// and swap it in atomically.

// DefaultDownloadBase is where GoReleaser's archives are published.
const DefaultDownloadBase = "https://github.com/fullfran/claudeops-tui/releases/download"

// ErrChecksumMismatch reports that a download did not match the checksum the
// release published for it. The binary on disk is never touched in that case.
var ErrChecksumMismatch = errors.New("downloaded archive does not match its published checksum")

// ErrNotWritable reports that the running binary cannot be replaced in place.
var ErrNotWritable = errors.New("install directory is not writable")

// maxArchiveBytes caps what will be pulled into memory. The archives are around
// 7MB; this leaves generous room without letting a bad URL exhaust it.
const maxArchiveBytes = 128 << 20

// BinaryInstaller replaces the running executable with a published release
// archive.
type BinaryInstaller struct {
	// DownloadBase is the release download root. Empty uses DefaultDownloadBase.
	DownloadBase string
	HTTP         *http.Client
}

func (b BinaryInstaller) base() string {
	if b.DownloadBase != "" {
		return strings.TrimRight(b.DownloadBase, "/")
	}
	return DefaultDownloadBase
}

func (b BinaryInstaller) client() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// AssetName returns the archive GoReleaser publishes for a platform. It must
// stay in step with archives.name_template in .goreleaser.yaml.
func AssetName(version, goos, goarch string) string {
	version = strings.TrimPrefix(version, "v")
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("claudeops_%s_%s_%s.%s", version, goos, goarch, ext)
}

// Writable reports whether the running binary can be replaced in place, by
// actually creating and removing a file next to it.
//
// Checking permission bits would be a guess: the answer depends on ownership,
// ACLs, and read-only mounts. Probing is the only honest test, and doing it
// before anything is downloaded means an unwritable /usr/local/bin fails in a
// second rather than after a 7MB download.
func Writable(execPath string) error {
	dir := filepath.Dir(installTarget(execPath))
	probe, err := os.CreateTemp(dir, ".claudeops-update-*")
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotWritable, dir)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// Install downloads the release archive for this platform, verifies it, and
// replaces execPath with the binary inside it.
func (b BinaryInstaller) Install(ctx context.Context, version, execPath string) error {
	version = strings.TrimPrefix(version, "v")
	asset := AssetName(version, runtime.GOOS, runtime.GOARCH)

	// Fetch the checksum first: without it there is nothing to verify against,
	// and finding that out after the download wastes the download.
	sums, err := b.fetch(ctx, version, "checksums.txt")
	if err != nil {
		// Releases published before the GoReleaser pipeline carry bare binaries
		// and no checksums file at all, so there is nothing to verify against
		// and nothing safe to install.
		return fmt.Errorf(
			"could not fetch checksums.txt for v%s: %w\n"+
				"if that release predates the current archive format, update manually: %s",
			version, err, releasesPage)
	}
	want, ok := parseChecksums(string(sums))[asset]
	if !ok {
		return fmt.Errorf(
			"release v%s publishes no %s\n"+
				"this platform may not be built for that release — download manually: %s",
			version, asset, releasesPage)
	}

	archive, err := b.fetch(ctx, version, asset)
	if err != nil {
		return fmt.Errorf("could not download %s: %w", asset, err)
	}

	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%w: %s", ErrChecksumMismatch, asset)
	}

	binary, err := extractBinary(archive, asset)
	if err != nil {
		return err
	}

	return replaceExecutable(execPath, binary)
}

func (b BinaryInstaller) fetch(ctx context.Context, version, name string) ([]byte, error) {
	url := fmt.Sprintf("%s/v%s/%s", b.base(), version, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes))
}

// parseChecksums reads the "<sha256>  <filename>" lines GoReleaser writes.
func parseChecksums(s string) map[string]string {
	sums := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sums[fields[1]] = fields[0]
	}
	return sums
}

// extractBinary pulls the claudeops executable out of a release archive.
//
// Only the entry named claudeops (or claudeops.exe) is read, and its name is
// never used as a path — the archive also carries README.md and LICENSE, and a
// crafted archive must not be able to write anywhere by naming itself
// "../../something".
func extractBinary(archive []byte, asset string) ([]byte, error) {
	wanted := "claudeops"
	if runtime.GOOS == "windows" {
		wanted = "claudeops.exe"
	}

	if strings.HasSuffix(asset, ".zip") {
		return extractFromZip(archive, wanted)
	}
	return extractFromTarGz(archive, wanted)
}

func extractFromTarGz(archive []byte, wanted string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("archive is not valid gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if filepath.Base(header.Name) != wanted || header.Typeflag != tar.TypeReg {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, maxArchiveBytes))
	}
	return nil, fmt.Errorf("archive contains no %s", wanted)
}

func extractFromZip(archive []byte, wanted string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("archive is not a valid zip: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) != wanted {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(io.LimitReader(rc, maxArchiveBytes))
	}
	return nil, fmt.Errorf("archive contains no %s", wanted)
}

// installTarget resolves execPath to the file an update must actually write.
//
// A package manager typically puts a symlink on PATH and keeps the binary in a
// directory of its own. Renaming over the symlink would replace it with a
// regular file and leave the target behind at the old version, so every path
// that writes or probes has to follow it first.
//
// An unresolvable path is returned unchanged: the caller's own error is a
// better report than one invented here.
func installTarget(execPath string) string {
	resolved, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return execPath
	}
	return resolved
}

// replaceExecutable swaps binary in for the file at execPath, following a
// symlink to whatever it points at.
//
// The new binary is written beside the target first, so the rename is within
// one filesystem and therefore atomic: either the old binary or the new one is
// there, never a half-written file. On Unix this works on a running executable
// because the rename replaces the directory entry while the running process
// keeps its open inode.
func replaceExecutable(execPath string, binary []byte) error {
	execPath = installTarget(execPath)
	dir := filepath.Dir(execPath)

	tmp, err := os.CreateTemp(dir, ".claudeops-new-*")
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotWritable, dir)
	}
	tmpName := tmp.Name()
	// Any early return below must not leave a stray file next to the binary.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write new binary: %w", err)
	}
	// Flush to disk before the rename: a crash in between would otherwise leave
	// the directory entry pointing at a file with no contents.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flush new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close new binary: %w", err)
	}

	mode := os.FileMode(0o755)
	if info, err := os.Stat(execPath); err == nil {
		// Keep whatever the existing binary had, so a deliberately restrictive
		// installation is not silently widened.
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("set permissions on new binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		// Windows refuses to replace a running executable, but it does allow
		// renaming it. Move it aside, put the new one in place, and delete the
		// old one — which fails while it is still running, so it is left for the
		// next update to clear.
		old := execPath + ".old"
		_ = os.Remove(old)
		if err := os.Rename(execPath, old); err != nil {
			return fmt.Errorf("could not move the running binary aside: %w", err)
		}
		if err := os.Rename(tmpName, execPath); err != nil {
			_ = os.Rename(old, execPath) // put it back
			return fmt.Errorf("install new binary: %w", err)
		}
		_ = os.Remove(old)
		return nil
	}

	if err := os.Rename(tmpName, execPath); err != nil {
		return fmt.Errorf("%w: could not replace %s: %v", ErrNotWritable, execPath, err)
	}
	return nil
}
