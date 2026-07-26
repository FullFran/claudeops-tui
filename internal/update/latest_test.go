package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeFetcher struct {
	version string
	err     error
	calls   int
}

func (f *fakeFetcher) Latest(context.Context) (Release, error) {
	f.calls++
	if f.err != nil {
		return Release{}, f.err
	}
	return Release{Version: f.version}, nil
}

// installRunner records whether `go install` was ever reached.
type installRunner struct {
	Runner
	installed bool
	execPath  string
}

func (r *installRunner) Executable() (string, error)           { return r.execPath, nil }
func (r *installRunner) EvalSymlinks(p string) (string, error) { return p, nil }
func (r *installRunner) LookPath(string) (string, error)       { return "/usr/bin/go", nil }
func (r *installRunner) GoEnv(context.Context) (Env, error)    { return Env{GOBIN: "/gobin"}, nil }
func (r *installRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "go" && len(args) > 0 && args[0] == "install" {
		r.installed = true
		return []byte(""), nil
	}
	return []byte("claudeops 9.9.9"), nil
}

func updaterFor(version string, f LatestFetcher, r *installRunner) Updater {
	return Updater{Runner: r, Version: version, Target: InstallTarget, Binary: "claudeops", Fetcher: f}
}

func TestDecideReportsAnAvailableUpdate(t *testing.T) {
	r := &installRunner{execPath: "/gobin/claudeops"}
	d, err := updaterFor("0.9.0", &fakeFetcher{version: "v0.10.0"}, r).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.LatestVersion != "0.10.0" {
		t.Errorf("LatestVersion = %q want 0.10.0", d.LatestVersion)
	}
	if d.UpToDate || d.Downgrade {
		t.Errorf("a newer release is neither up-to-date nor a downgrade: %+v", d)
	}
}

func TestDecideDetectsUpToDate(t *testing.T) {
	r := &installRunner{execPath: "/gobin/claudeops"}
	d, _ := updaterFor("0.10.0", &fakeFetcher{version: "v0.10.0"}, r).Decide(context.Background())
	if !d.UpToDate || d.Downgrade {
		t.Errorf("same version should be up to date and not a downgrade: %+v", d)
	}
}

func TestDecideDetectsADowngrade(t *testing.T) {
	// The case that matters: a build made between releases. The published
	// version is older, and installing it would remove whatever this build has.
	r := &installRunner{execPath: "/gobin/claudeops"}
	d, _ := updaterFor("0.11.0", &fakeFetcher{version: "v0.10.0"}, r).Decide(context.Background())
	if !d.Downgrade {
		t.Errorf("an older published version is a downgrade: %+v", d)
	}
}

func TestUpToDateSkipsTheInstallEntirely(t *testing.T) {
	// It used to install first and compare afterwards, so every check was a
	// full download to arrive back where it started.
	r := &installRunner{execPath: "/gobin/claudeops"}
	d, err := updaterFor("0.10.0", &fakeFetcher{version: "v0.10.0"}, r).Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.installed {
		t.Error("nothing to install, yet `go install` ran")
	}
	if !d.UpToDate {
		t.Error("expected UpToDate")
	}
}

func TestDowngradeRefusesBeforeTouchingTheBinary(t *testing.T) {
	// The old guard compared versions *after* installing, so the binary was
	// already replaced by the time anyone objected.
	r := &installRunner{execPath: "/gobin/claudeops"}
	_, err := updaterFor("0.11.0", &fakeFetcher{version: "v0.10.0"}, r).Update(context.Background())
	if !errors.Is(err, ErrStaleRelease) {
		t.Fatalf("got %v want ErrStaleRelease", err)
	}
	if r.installed {
		t.Error("refused the downgrade but installed anyway")
	}
	if !strings.Contains(err.Error(), "0.10.0") || !strings.Contains(err.Error(), "0.11.0") {
		t.Errorf("the error should name both versions: %v", err)
	}
}

func TestNewerVersionStillInstalls(t *testing.T) {
	r := &installRunner{execPath: "/gobin/claudeops"}
	if _, err := updaterFor("0.9.0", &fakeFetcher{version: "v0.10.0"}, r).Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.installed {
		t.Error("a newer release should have been installed")
	}
}

func TestUnreachableProxyDoesNotBlockTheUpdate(t *testing.T) {
	// Losing the proxy must not stop someone updating; it only removes the
	// ability to pre-empt the install.
	r := &installRunner{execPath: "/gobin/claudeops"}
	f := &fakeFetcher{err: errors.New("network down")}
	d, err := updaterFor("0.9.0", f, r).Update(context.Background())
	if err != nil {
		t.Fatalf("an unreachable proxy is not fatal: %v", err)
	}
	if d.LatestVersion != "" {
		t.Errorf("LatestVersion should be empty, got %q", d.LatestVersion)
	}
	if !r.installed {
		t.Error("should have installed blind, as before")
	}
}

func TestLatestIsResolvedEvenWhenAutoUpdateIsImpossible(t *testing.T) {
	// "There is a new version, here is the command" is still the useful answer
	// for someone who installed the binary by hand.
	r := &installRunner{execPath: "/somewhere/else/claudeops"}
	f := &fakeFetcher{version: "v0.10.0"}
	d, _ := updaterFor("0.9.0", f, r).Decide(context.Background())
	if d.CanAuto {
		t.Fatal("this fixture should not be auto-updatable")
	}
	if d.LatestVersion != "0.10.0" {
		t.Errorf("the proxy should still have been asked, got %q", d.LatestVersion)
	}
	if f.calls != 1 {
		t.Errorf("expected one proxy call, got %d", f.calls)
	}
}

func TestProxyFetcherParsesTheResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/@latest") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"Version":"v1.2.3","Time":"2026-07-26T15:40:20Z"}`))
	}))
	defer srv.Close()

	rel, err := ProxyFetcher{BaseURL: srv.URL, Module: "example.com/m", HTTP: srv.Client()}.
		Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v1.2.3" {
		t.Errorf("got %q want v1.2.3", rel.Version)
	}
}

func TestProxyFetcherReportsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ProxyFetcher{BaseURL: srv.URL, Module: "example.com/m", HTTP: srv.Client()}.
		Latest(context.Background())
	if err == nil {
		t.Error("an HTTP 500 should be reported, not silently treated as a version")
	}
}
