package familiars

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	compressed := gzip.NewWriter(&out)
	archive := tar.NewWriter(compressed)
	for name, content := range files {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	_ = archive.Close()
	_ = compressed.Close()
	return out.Bytes()
}

func zipped(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	for name, content := range files {
		w, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	_ = archive.Close()
	return out.Bytes()
}

// release serves one version of familiard the way GitHub does.
func release(t *testing.T, asset string, data []byte, sums string) *httptest.Server {
	t.Helper()
	if sums == "" {
		sum := sha256.Sum256(data)
		sums = hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/latest":
			fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
		case r.URL.Path == "/files/v9.9.9/SHA256SUMS":
			fmt.Fprint(w, sums)
		case r.URL.Path == "/files/v9.9.9/"+asset:
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func installer(server *httptest.Server, dir, goos, goarch string) Installer {
	return Installer{
		Dir: dir, GOOS: goos, GOARCH: goarch, HTTP: server.Client(), Log: io.Discard,
		LatestURL: server.URL + "/latest", FilesURL: server.URL + "/files",
	}
}

const linuxAsset = "familiard-v9.9.9-x86_64-unknown-linux-gnu.tar.gz"

func TestInstallPutsTheProgramInItsFolderAndAddsTheDemo(t *testing.T) {
	data := tarGz(t, map[string]string{
		"familiard-v9.9.9-x/familiard":          "the program",
		"familiard-v9.9.9-x/README.md":          "ignored",
		"familiard-v9.9.9-x/pets/demo/pet.json": "{}",
		"familiard-v9.9.9-x/pets/demo/sheet":    "sheet",
	})
	dir := filepath.Join(t.TempDir(), "bin")
	in := installer(release(t, linuxAsset, data, ""), dir, "linux", "amd64")
	var demoFiles []string
	in.AddDemo = func(_ context.Context, program, folder string) error {
		entries, _ := os.ReadDir(folder)
		for _, entry := range entries {
			demoFiles = append(demoFiles, entry.Name())
		}
		if program != filepath.Join(dir, "familiard") {
			t.Errorf("program = %s", program)
		}
		return nil
	}
	got, err := in.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(got)
	if string(content) != "the program" {
		t.Errorf("content = %q", content)
	}
	info, _ := os.Stat(got)
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("not executable: %v", info.Mode())
	}
	if strings.Join(demoFiles, ",") != "pet.json,sheet" {
		t.Errorf("demo = %v", demoFiles)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("only the program belongs here: %v", entries)
	}
}

func TestInstallOnWindowsReadsTheZip(t *testing.T) {
	asset := "familiard-v9.9.9-x86_64-pc-windows-msvc.zip"
	data := zipped(t, map[string]string{"familiard-v9.9.9-x/familiard.exe": "exe"})
	dir := t.TempDir()
	got, err := installer(release(t, asset, data, ""), dir, "windows", "amd64").Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "familiard.exe" {
		t.Errorf("got %s", got)
	}
}

func TestAFailedDownloadInstallsNothingAndKeepsWhatWasThere(t *testing.T) {
	good := tarGz(t, map[string]string{"r/familiard": "new"})
	tests := []struct {
		name string
		data []byte
		sums string
		want string
	}{
		{"wrong checksum", good, strings.Repeat("0", 64) + "  " + linuxAsset + "\n", "does not match"},
		{"no checksum listed", good, "abc  other-file\n", "no checksum"},
		{"no program inside", tarGz(t, map[string]string{"r/README.md": "x"}), "", "has no familiard"},
		{"not an archive", []byte("html error page"), "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			existing := filepath.Join(dir, "familiard")
			if err := os.WriteFile(existing, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := installer(release(t, linuxAsset, tt.data, tt.sums), dir, "linux", "amd64").Install(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v", err)
			}
			content, _ := os.ReadFile(existing)
			if string(content) != "old" {
				t.Errorf("the old program was touched: %q", content)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Errorf("left behind: %v", entries)
			}
		})
	}
}

func TestNoEntryNameCanChooseWhereItEndsUp(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "evil")
	data := tarGz(t, map[string]string{
		"r/familiard":                "program",
		"../familiard":               "escaped",
		"r/../../evil":               "escaped",
		outside:                      "absolute",
		"r/pets/demo/../../../evil2": "escaped",
		"r/deep/er/familiard":        "elsewhere",
	})
	files, err := unpack(data, "tar.gz", "familiard")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || string(files["familiard"]) != "program" {
		t.Errorf("got %v", files)
	}
}

func TestLinksInTheArchiveAreIgnored(t *testing.T) {
	var out bytes.Buffer
	compressed := gzip.NewWriter(&out)
	archive := tar.NewWriter(compressed)
	_ = archive.WriteHeader(&tar.Header{Name: "r/familiard", Typeflag: tar.TypeSymlink, Linkname: "/bin/sh"})
	_ = archive.Close()
	_ = compressed.Close()
	files, err := unpack(out.Bytes(), "tar.gz", "familiard")
	if err != nil || len(files) != 0 {
		t.Errorf("got %v, %v", files, err)
	}
}

func TestDownloadsHaveALimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxDownload+1))
	}))
	defer server.Close()
	_, err := Installer{HTTP: server.Client()}.fetch(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "too big") {
		t.Errorf("got %v", err)
	}
}

func TestAStrangeReleaseNameIsRefused(t *testing.T) {
	for _, tag := range []string{"../../evil", "latest", "v1.2", ""} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"tag_name":%q}`, tag)
		}))
		in := installer(server, t.TempDir(), "linux", "amd64")
		if _, err := in.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "unexpected release") {
			t.Errorf("%q: got %v", tag, err)
		}
		server.Close()
	}
}

func TestUnsupportedSystemsSayWhich(t *testing.T) {
	_, err := Installer{GOOS: "plan9", GOARCH: "386"}.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "plan9/386") {
		t.Errorf("got %v", err)
	}
}

func TestAFailedDemoDoesNotFailTheInstall(t *testing.T) {
	data := tarGz(t, map[string]string{"r/familiard": "program", "r/pets/demo/pet.json": "{}"})
	var log bytes.Buffer
	in := installer(release(t, linuxAsset, data, ""), t.TempDir(), "linux", "amd64")
	in.Log = &log
	in.AddDemo = func(context.Context, string, string) error { return fmt.Errorf("gallery says no") }
	if _, err := in.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "gallery says no") {
		t.Errorf("log = %q", log.String())
	}
}
