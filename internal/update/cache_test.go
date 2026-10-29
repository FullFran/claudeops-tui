package update

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckResult_HasUpdate(t *testing.T) {
	tests := []struct {
		name string
		res  CheckResult
		want bool
	}{
		{
			name: "update available",
			res: CheckResult{
				CurrentVersion: "0.16.0",
				LatestVersion:  "0.17.0",
				UpToDate:       false,
				Downgrade:      false,
			},
			want: true,
		},
		{
			name: "already up to date",
			res: CheckResult{
				CurrentVersion: "0.16.0",
				LatestVersion:  "0.16.0",
				UpToDate:       true,
				Downgrade:      false,
			},
			want: false,
		},
		{
			name: "ahead of release (downgrade)",
			res: CheckResult{
				CurrentVersion: "0.17.0",
				LatestVersion:  "0.16.0",
				UpToDate:       true,
				Downgrade:      true,
			},
			want: false,
		},
		{
			name: "empty latest version (offline)",
			res: CheckResult{
				CurrentVersion: "0.16.0",
				LatestVersion:  "",
				UpToDate:       false,
				Downgrade:      false,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.res.HasUpdate(); got != tt.want {
				t.Errorf("HasUpdate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCache_ReadWrite(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update-cache.json")

	// Missing file returns error
	if _, err := ReadCache(cachePath); err == nil {
		t.Errorf("expected error reading missing cache, got nil")
	}

	res := CheckResult{
		CheckedAt:      time.Now().Truncate(time.Second),
		CurrentVersion: "0.16.0",
		LatestVersion:  "0.17.0",
		UpToDate:       false,
		Method:         MethodBinary,
		InstallCommand: "claudeops update",
	}

	if err := WriteCache(cachePath, res); err != nil {
		t.Fatalf("WriteCache failed: %v", err)
	}

	read, err := ReadCache(cachePath)
	if err != nil {
		t.Fatalf("ReadCache failed: %v", err)
	}

	if read.CurrentVersion != res.CurrentVersion || read.LatestVersion != res.LatestVersion {
		t.Errorf("read cache mismatch: got %+v, want %+v", read, res)
	}
	if !read.HasUpdate() {
		t.Errorf("expected HasUpdate to be true")
	}
}

type cacheMockRunner struct {
	execPath string
}

func (m cacheMockRunner) Executable() (string, error)               { return m.execPath, nil }
func (m cacheMockRunner) EvalSymlinks(p string) (string, error)      { return p, nil }
func (m cacheMockRunner) LookPath(file string) (string, error)       { return "/bin/go", nil }
func (m cacheMockRunner) GoEnv(ctx context.Context) (Env, error)     { return Env{GOBIN: "/bin"}, nil }
func (m cacheMockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return nil, nil
}

func TestCheckWithCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update-cache.json")

	u := Updater{
		Version:        "0.16.0",
		Target:         InstallTarget,
		Binary:         "claudeops",
		Runner:         cacheMockRunner{execPath: "/bin/claudeops"},
		ReleaseFetcher: staticFetcher{version: "v0.17.0"},
	}

	ctx := context.Background()

	// First check: should call Decide and write cache
	res, err := CheckWithCache(ctx, u, cachePath, 24*time.Hour)
	if err != nil {
		t.Fatalf("CheckWithCache failed: %v", err)
	}
	if !res.HasUpdate() || res.LatestVersion != "0.17.0" {
		t.Errorf("expected update to 0.17.0, got %+v", res)
	}

	// Change fetcher to newer version, but because cache is fresh, CheckWithCache
	// must return the cached version (0.17.0) rather than calling Decide again.
	u.ReleaseFetcher = staticFetcher{version: "v0.18.0"}
	cachedRes, err := CheckWithCache(ctx, u, cachePath, 24*time.Hour)
	if err != nil {
		t.Fatalf("second CheckWithCache failed: %v", err)
	}
	if cachedRes.LatestVersion != "0.17.0" {
		t.Errorf("expected cached version 0.17.0, got %s", cachedRes.LatestVersion)
	}

	// With TTL = 0 (or expired), it should bypass cache and fetch new version
	freshRes, err := CheckWithCache(ctx, u, cachePath, -1*time.Second)
	if err != nil {
		t.Fatalf("expired CheckWithCache failed: %v", err)
	}
	if freshRes.LatestVersion != "0.18.0" {
		t.Errorf("expected fresh version 0.18.0, got %s", freshRes.LatestVersion)
	}
}
