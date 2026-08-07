package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// redirectServer stands in for github.com/OWNER/REPO/releases/latest.
func redirectServer(t *testing.T, status int, location string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGitHubFetcherLatest(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		location string
		want     string
		wantErr  string
	}{
		{
			name:     "reads the tag from the redirect",
			status:   http.StatusFound,
			location: "https://github.com/fullfran/claudeops-tui/releases/tag/v0.14.1",
			want:     "0.14.1",
		},
		{
			name:     "a permanent redirect is equally valid",
			status:   http.StatusMovedPermanently,
			location: "https://github.com/fullfran/claudeops-tui/releases/tag/v1.0.0",
			want:     "1.0.0",
		},
		{
			name:     "a tag without the v prefix",
			status:   http.StatusFound,
			location: "https://github.com/fullfran/claudeops-tui/releases/tag/0.14.1",
			want:     "0.14.1",
		},
		{
			// A repository with no releases redirects to the releases index.
			// Reading its last path segment would yield "releases".
			name:     "no releases at all is an error, not a version",
			status:   http.StatusFound,
			location: "https://github.com/fullfran/claudeops-tui/releases",
			wantErr:  "could not read a version",
		},
		{
			name:     "a tag that is not a version",
			status:   http.StatusFound,
			location: "https://github.com/fullfran/claudeops-tui/releases/tag/nightly",
			wantErr:  "not a version",
		},
		{
			// The REST API answers 200 with JSON. Reaching this fetcher means
			// something other than the redirect endpoint replied.
			name:    "a non-redirect response is an error",
			status:  http.StatusOK,
			wantErr: "expected a redirect",
		},
		{
			name:    "server failure",
			status:  http.StatusServiceUnavailable,
			wantErr: "503",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := redirectServer(t, tt.status, tt.location)
			rel, err := GitHubFetcher{URL: server.URL}.Latest(context.Background())

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error, got version %q", rel.Version)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rel.Version != tt.want {
				t.Fatalf("version = %q, want %q", rel.Version, tt.want)
			}
		})
	}
}

// The redirect target IS the answer. Following it fetches an HTML page and
// loses the tag.
func TestGitHubFetcherDoesNotFollowTheRedirect(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/landing" {
			_, _ = w.Write([]byte("<html>a release page</html>"))
			return
		}
		w.Header().Set("Location", "/releases/tag/v0.14.1")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(server.Close)

	rel, err := GitHubFetcher{URL: server.URL}.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.14.1" {
		t.Fatalf("version = %q, want 0.14.1", rel.Version)
	}
	if hits != 1 {
		t.Fatalf("made %d requests, want exactly 1 (the redirect must not be followed)", hits)
	}
}

func TestTagFromReleaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://github.com/o/r/releases/tag/v1.2.3", "1.2.3"},
		{"https://github.com/o/r/releases/tag/v1.2.3?foo=bar", "1.2.3"},
		{"/releases/tag/v0.1.0", "0.1.0"},
		{"https://github.com/o/r/releases", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := tagFromReleaseURL(tt.in); got != tt.want {
				t.Fatalf("tagFromReleaseURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
