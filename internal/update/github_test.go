package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func githubServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/repos/fullfran/claudeops-tui/releases") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGitHubFetcherLatest(t *testing.T) {
	const withSums = `{"name":"checksums.txt"}`

	tests := []struct {
		name    string
		payload string
		want    string
		wantErr bool
	}{
		{
			name: "newest release wins regardless of list order",
			payload: `[{"tag_name":"v0.13.0","assets":[` + withSums + `]},
			           {"tag_name":"v0.14.0","assets":[` + withSums + `]}]`,
			want: "0.14.0",
		},
		{
			// A tag exists from the moment it is pushed; its archives do not.
			// Offering such a release means a guaranteed 404 on download.
			name: "a release without checksums is not installable",
			payload: `[{"tag_name":"v0.14.0","assets":[]},
			           {"tag_name":"v0.13.0","assets":[` + withSums + `]}]`,
			want: "0.13.0",
		},
		{
			name: "drafts are not published",
			payload: `[{"tag_name":"v0.15.0","draft":true,"assets":[` + withSums + `]},
			           {"tag_name":"v0.14.0","assets":[` + withSums + `]}]`,
			want: "0.14.0",
		},
		{
			// A tagged release candidate must never be offered as an upgrade to
			// someone on a stable version.
			name: "prereleases are skipped",
			payload: `[{"tag_name":"v0.15.0-rc.1","prerelease":true,"assets":[` + withSums + `]},
			           {"tag_name":"v0.14.0","assets":[` + withSums + `]}]`,
			want: "0.14.0",
		},
		{
			name:    "nothing installable is an error, not an empty version",
			payload: `[{"tag_name":"v0.13.1","assets":[]}]`,
			wantErr: true,
		},
		{
			name:    "no releases at all",
			payload: `[]`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := githubServer(t, tt.payload)
			f := GitHubFetcher{BaseURL: server.URL}

			rel, err := f.Latest(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got version %q", rel.Version)
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

func TestGitHubFetcherReportsAnHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	_, err := GitHubFetcher{BaseURL: server.URL}.Latest(context.Background())
	if err == nil {
		t.Fatal("expected an error for HTTP 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error should name the status, got: %v", err)
	}
}
