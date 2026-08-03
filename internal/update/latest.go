package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The updater used to find out what it was installing only after it had
// installed it: `go install @latest` ran unconditionally, and the version was
// compared afterwards. That has two costs. Every check is a full download even
// when nothing changed, and a proxy serving something older silently replaces
// your binary before anyone can object — which is how a build with a feature
// got swapped for a release without it.
//
// The module proxy answers the question directly, so ask first.

// DefaultProxyURL is the public Go module proxy.
const DefaultProxyURL = "https://proxy.golang.org"

// ModulePath is the module whose latest version is queried.
const ModulePath = "github.com/fullfran/claudeops-tui"

// Release is what the proxy reports for @latest.
type Release struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

// LatestFetcher resolves the newest published version. Injectable for tests.
type LatestFetcher interface {
	Latest(ctx context.Context) (Release, error)
}

// ProxyFetcher asks a Go module proxy.
type ProxyFetcher struct {
	BaseURL string
	Module  string
	HTTP    *http.Client
}

func (f ProxyFetcher) base() string {
	if f.BaseURL != "" {
		return strings.TrimRight(f.BaseURL, "/")
	}
	return DefaultProxyURL
}

func (f ProxyFetcher) module() string {
	if f.Module != "" {
		return f.Module
	}
	return ModulePath
}

func (f ProxyFetcher) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Latest returns the newest published version.
//
// It asks @v/list rather than @latest, because @latest is a derived answer the
// proxy caches and @v/list is the list of tags it already knows. Minutes after
// v0.13.0 was tagged the proxy served v0.13.0 from @v/list and v0.12.0 from
// @latest, so the updater told everyone they were up to date on the version it
// had just superseded — and because it asks before installing, it never got as
// far as the GOPROXY=direct install that would have corrected it.
//
// @latest remains the fallback: it is the only answer for a module with no
// tagged versions at all, and it costs nothing to keep.
//
// The proxy path is lowercase by convention; module paths with capitals use
// !-escaping, which this module does not need.
func (f ProxyFetcher) Latest(ctx context.Context) (Release, error) {
	rel, listErr := f.latestFromList(ctx)
	if listErr == nil {
		return rel, nil
	}
	rel, err := f.latestFromAtLatest(ctx)
	if err != nil {
		// Report the list failure too: it is the one that mattered.
		return Release{}, fmt.Errorf("%w (version list: %v)", err, listErr)
	}
	return rel, nil
}

// latestFromList reads @v/list and returns the highest stable version.
//
// Prereleases are skipped so a tagged release candidate is never offered as an
// upgrade. The endpoint carries no timestamps, so Release.Time stays zero;
// nothing consumes it.
func (f ProxyFetcher) latestFromList(ctx context.Context) (Release, error) {
	body, err := f.get(ctx, "@v/list")
	if err != nil {
		return Release{}, err
	}
	best := ""
	for _, line := range strings.Fields(string(body)) {
		v := strings.TrimSpace(line)
		if v == "" || strings.ContainsAny(v, "-+") {
			continue // prerelease or build metadata
		}
		if _, ok := parseSemver(v); !ok {
			continue
		}
		if best == "" || semverLT(best, v) {
			best = v
		}
	}
	if best == "" {
		return Release{}, fmt.Errorf("module proxy listed no stable versions")
	}
	return Release{Version: best}, nil
}

// latestFromAtLatest reads the proxy's own @latest answer.
func (f ProxyFetcher) latestFromAtLatest(ctx context.Context) (Release, error) {
	body, err := f.get(ctx, "@latest")
	if err != nil {
		return Release{}, err
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return Release{}, err
	}
	if rel.Version == "" {
		return Release{}, fmt.Errorf("module proxy returned no version")
	}
	return rel, nil
}

// get fetches one proxy endpoint for this module.
func (f ProxyFetcher) get(ctx context.Context, endpoint string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/%s", f.base(), f.module(), endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("module proxy returned HTTP %d for %s", resp.StatusCode, endpoint)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
