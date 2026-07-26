package update

import (
	"context"
	"encoding/json"
	"fmt"
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
// The proxy path is lowercase by convention; module paths with capitals use
// !-escaping, which this module does not need.
func (f ProxyFetcher) Latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("%s/%s/@latest", f.base(), f.module())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("module proxy returned HTTP %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, err
	}
	if rel.Version == "" {
		return Release{}, fmt.Errorf("module proxy returned no version")
	}
	return rel, nil
}
