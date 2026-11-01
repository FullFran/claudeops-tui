package familiars

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// LatestRelease says which release is current.
	LatestRelease = "https://api.github.com/repos/FullFran/familiard/releases/latest"
	// ReleaseFiles is where a release's files are.
	ReleaseFiles = "https://github.com/FullFran/familiard/releases/download"
	// maxDownload is far more than a release needs, so that a bad answer cannot fill the disk.
	maxDownload = 64 << 20
)

var versionTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Installer downloads a familiard release for this machine. It only ever writes inside Dir.
type Installer struct {
	Dir       string // where the program goes, normally ~/.claudeops/bin
	GOOS      string
	GOARCH    string
	HTTP      *http.Client
	LatestURL string
	FilesURL  string
	Log       io.Writer
	// AddDemo gives an empty gallery its first pet. Failing is not fatal: the program works.
	AddDemo func(ctx context.Context, program, demoDir string) error
}

// target names the release built for a system, and how it is packed.
func target(goos, goarch string) (name, archive string, err error) {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "x86_64-unknown-linux-gnu", "tar.gz", nil
	case "darwin/arm64":
		return "aarch64-apple-darwin", "tar.gz", nil
	case "darwin/amd64":
		return "x86_64-apple-darwin", "tar.gz", nil
	case "windows/amd64":
		return "x86_64-pc-windows-msvc", "zip", nil
	}
	return "", "", fmt.Errorf("familiard has no release for %s/%s", goos, goarch)
}

// Install fetches the latest release into Dir and returns the installed program's path. Nothing
// reaches Dir unless the download matches the release's checksums.
func (in Installer) Install(ctx context.Context) (string, error) {
	name, archive, err := target(in.GOOS, in.GOARCH)
	if err != nil {
		return "", err
	}
	tag, err := in.latestTag(ctx)
	if err != nil {
		return "", err
	}
	asset := fmt.Sprintf("familiard-%s-%s.%s", tag, name, archive)
	sums, err := in.fetch(ctx, in.FilesURL+"/"+tag+"/SHA256SUMS")
	if err != nil {
		return "", err
	}
	want, err := checksumOf(sums, asset)
	if err != nil {
		return "", err
	}
	_, _ = fmt.Fprintf(in.Log, "downloading %s\n", asset)
	data, err := in.fetch(ctx, in.FilesURL+"/"+tag+"/"+asset)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%s does not match its checksum, nothing was installed", asset)
	}
	program := programNameFor(in.GOOS)
	files, err := unpack(data, archive, program)
	if err != nil {
		return "", err
	}
	binary, ok := files[program]
	if !ok {
		return "", fmt.Errorf("%s has no %s in it", asset, program)
	}
	if err := os.MkdirAll(in.Dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(in.Dir, program)
	if err := writeExecutable(target, binary); err != nil {
		return "", err
	}
	in.addDemo(ctx, target, files)
	return target, nil
}

func programNameFor(goos string) string {
	if goos == "windows" {
		return "familiard.exe"
	}
	return "familiard"
}

func (in Installer) latestTag(ctx context.Context) (string, error) {
	data, err := in.fetch(ctx, in.LatestURL)
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("reading the latest release: %w", err)
	}
	if !versionTag.MatchString(release.TagName) {
		return "", fmt.Errorf("unexpected release name %q", release.TagName)
	}
	return release.TagName, nil
}

func (in Installer) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := in.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("%s is too big", url)
	}
	return data, nil
}

func checksumOf(sums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("the release lists no checksum for %s", asset)
}

// wanted says what an archive entry is, going only by its exact place in the release's layout:
// <root>/familiard for the program and <root>/pets/demo/<file> for the demo pet. Anything else is
// ignored, so no entry's name can choose where it ends up.
func wanted(entry, program string) (string, bool) {
	parts := strings.Split(path.Clean(strings.ReplaceAll(entry, `\`, "/")), "/")
	switch {
	case len(parts) == 2 && parts[0] != ".." && parts[1] == program:
		return program, true
	case len(parts) == 4 && parts[0] != ".." && parts[1] == "pets" && parts[2] == "demo" && parts[3] != "..":
		return "demo/" + parts[3], true
	}
	return "", false
}

func unpack(data []byte, archive, program string) (map[string][]byte, error) {
	if archive == "zip" {
		return unpackZip(data, program)
	}
	return unpackTar(data, program)
}

func unpackTar(data []byte, program string) (map[string][]byte, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	reader := tar.NewReader(compressed)
	files := map[string][]byte{}
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		name, ok := wanted(header.Name, program)
		if !ok || header.Typeflag != tar.TypeReg {
			continue
		}
		content, err := io.ReadAll(io.LimitReader(reader, maxDownload+1))
		if err != nil {
			return nil, err
		}
		if total += int64(len(content)); total > maxDownload {
			return nil, errors.New("the release unpacks to too much")
		}
		files[name] = content
	}
}

func unpackZip(data []byte, program string) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	var total int64
	for _, entry := range reader.File {
		name, ok := wanted(entry.Name, program)
		if !ok || !entry.Mode().IsRegular() {
			continue
		}
		opened, err := entry.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(opened, maxDownload+1))
		_ = opened.Close()
		if err != nil {
			return nil, err
		}
		if total += int64(len(content)); total > maxDownload {
			return nil, errors.New("the release unpacks to too much")
		}
		files[name] = content
	}
	return files, nil
}

// writeExecutable replaces the program whole, so that a failure leaves the old one working.
func writeExecutable(target string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".familiard-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporary.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), target)
}

func (in Installer) addDemo(ctx context.Context, program string, files map[string][]byte) {
	if in.AddDemo == nil {
		return
	}
	folder, err := os.MkdirTemp("", "familiard-demo-")
	if err != nil {
		_, _ = fmt.Fprintf(in.Log, "the demo pet was not added: %v\n", err)
		return
	}
	defer func() { _ = os.RemoveAll(folder) }()
	for name, content := range files {
		base, isDemo := strings.CutPrefix(name, "demo/")
		if !isDemo {
			continue
		}
		if err := os.WriteFile(filepath.Join(folder, base), content, 0o600); err != nil {
			_, _ = fmt.Fprintf(in.Log, "the demo pet was not added: %v\n", err)
			return
		}
	}
	if err := in.AddDemo(ctx, program, folder); err != nil {
		_, _ = fmt.Fprintf(in.Log, "the demo pet was not added: %v\n", err)
	}
}
