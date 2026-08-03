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

// serveProxy stands in for a Go module proxy. An empty list or atLatest body
// makes that endpoint answer 404, the way a proxy does when it knows nothing.
func serveProxy(t *testing.T, list, atLatest string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/@v/list"):
			if list == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(list))
		case strings.HasSuffix(r.URL.Path, "/@latest"):
			if atLatest == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(atLatest))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestProxyFetcherPrefersTheVersionList pins the reason Latest asks @v/list
// first. The "stale @latest" case is the one that shipped: minutes after
// v0.13.0 was tagged the proxy listed it but still derived v0.12.0 for @latest,
// so every install was told it was current on the version it had superseded.
func TestProxyFetcherPrefersTheVersionList(t *testing.T) {
	tests := []struct {
		name     string
		list     string
		atLatest string
		want     string
	}{
		{
			name:     "a stale @latest does not win",
			list:     "v0.11.0\nv0.12.0\nv0.13.0\n",
			atLatest: `{"Version":"v0.12.0"}`,
			want:     "v0.13.0",
		},
		{
			name:     "the list need not be ordered",
			list:     "v0.9.0\nv0.13.0\nv0.10.0\nv0.12.0\n",
			atLatest: `{"Version":"v0.9.0"}`,
			want:     "v0.13.0",
		},
		{
			name:     "double-digit minors sort numerically, not as text",
			list:     "v0.9.0\nv0.13.0\n",
			atLatest: `{"Version":"v0.9.0"}`,
			want:     "v0.13.0",
		},
		{
			name:     "prereleases are never offered",
			list:     "v0.12.0\nv0.13.0-rc1\n",
			atLatest: `{"Version":"v0.12.0"}`,
			want:     "v0.12.0",
		},
		{
			name:     "unparseable entries are ignored",
			list:     "not-a-version\nv0.12.0\n\n",
			atLatest: `{"Version":"v0.11.0"}`,
			want:     "v0.12.0",
		},
		{
			name:     "no list falls back to @latest",
			atLatest: `{"Version":"v1.2.3","Time":"2026-07-26T15:40:20Z"}`,
			want:     "v1.2.3",
		},
		{
			name:     "a list with nothing usable falls back to @latest",
			list:     "v0.13.0-rc1\ngarbage\n",
			atLatest: `{"Version":"v0.12.0"}`,
			want:     "v0.12.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serveProxy(t, tt.list, tt.atLatest)
			rel, err := ProxyFetcher{BaseURL: srv.URL, Module: "example.com/m", HTTP: srv.Client()}.
				Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest: %v", err)
			}
			if rel.Version != tt.want {
				t.Errorf("Latest() = %q, want %q", rel.Version, tt.want)
			}
		})
	}
}

// TestProxyFetcherReportsBothFailures keeps the diagnosis honest when neither
// endpoint answers: the list failure is the one that mattered, so it must not
// be swallowed by the fallback's error.
func TestProxyFetcherReportsBothFailures(t *testing.T) {
	srv := serveProxy(t, "", "")
	_, err := ProxyFetcher{BaseURL: srv.URL, Module: "example.com/m", HTTP: srv.Client()}.
		Latest(context.Background())
	if err == nil {
		t.Fatal("want an error when neither endpoint answers")
	}
	if !strings.Contains(err.Error(), "@latest") || !strings.Contains(err.Error(), "@v/list") {
		t.Errorf("error = %q, want it to name both endpoints it tried", err)
	}
}
