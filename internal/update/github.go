package update

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// A binary update downloads archives from GitHub Releases, so GitHub Releases
// is the only source that can answer which version it is able to install.
//
// The module proxy cannot. It indexes git tags, and a tag exists from the
// moment it is pushed — minutes before the workflow that builds its archives
// finishes, and permanently for any tag whose release failed or predates the
// archive format. Asking the proxy and downloading from GitHub means the
// updater regularly resolves a version whose assets do not exist: v0.14.0 was
// published while the proxy still listed v0.13.0, and v0.13.1 has a tag but no
// checksums.txt, so a binary install was told to update to a release it could
// only 404 on.
//
// The same applies to `go install`, which runs with GOPROXY=direct and so
// installs from tags rather than from what the proxy has cached — the proxy
// under-reports what it can already install.

// ReleasesLatestURL redirects to the newest published release.
const ReleasesLatestURL = "https://github.com/" + Repository + "/releases/latest"

// Repository is the owner/name whose releases carry the published archives.
const Repository = "fullfran/claudeops-tui"

// GitHubFetcher resolves the newest published release.
//
// It reads the redirect from /releases/latest rather than calling the REST API.
// The API allows 60 unauthenticated requests per hour per IP address, which a
// CLI cannot spend and which is shared by everyone behind the same NAT — the
// first thing this fetcher did in the wild was exhaust it and silently report
// "already up to date". The redirect is plain HTTP with no such budget.
//
// /releases/latest is also exactly the right question: GitHub excludes drafts
// and prereleases from it, so a tagged release candidate is never offered as
// an upgrade.
type GitHubFetcher struct {
	// URL overrides the redirect endpoint. Empty uses ReleasesLatestURL.
	URL  string
	HTTP *http.Client
}

func (f GitHubFetcher) url() string {
	if f.URL != "" {
		return f.URL
	}
	return ReleasesLatestURL
}

// client returns a client that reports the redirect instead of following it.
// The redirect target is the answer; following it would fetch a web page.
func (f GitHubFetcher) client() *http.Client {
	c := &http.Client{Timeout: 10 * time.Second}
	if f.HTTP != nil {
		copied := *f.HTTP
		c = &copied
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return c
}

// Latest returns the version the releases page redirects to.
func (f GitHubFetcher) Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, f.url(), nil)
	if err != nil {
		return Release{}, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return Release{}, fmt.Errorf("release index returned HTTP %d, expected a redirect", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	version := tagFromReleaseURL(location)
	if version == "" {
		return Release{}, fmt.Errorf("could not read a version from %q", location)
	}
	if _, ok := parseSemver(version); !ok {
		return Release{}, fmt.Errorf("release index reported %q, which is not a version", version)
	}
	return Release{Version: version}, nil
}

// tagFromReleaseURL pulls the tag out of ".../releases/tag/vX.Y.Z".
//
// Only that shape is accepted. A repository with no releases at all redirects
// to the releases index, whose last path segment would otherwise be read as a
// version.
func tagFromReleaseURL(u string) string {
	marker := "/releases/tag/"
	i := strings.Index(u, marker)
	if i < 0 {
		return ""
	}
	tag := u[i+len(marker):]
	if i := strings.IndexAny(tag, "?#/"); i >= 0 {
		tag = tag[:i]
	}
	return strings.TrimPrefix(tag, "v")
}
