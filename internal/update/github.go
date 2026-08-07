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
// `go install` keeps using the proxy: that is the source it installs from, so
// for that method the proxy is the correct authority.

// DefaultGitHubAPI is the GitHub REST API root.
const DefaultGitHubAPI = "https://api.github.com"

// Repository is the owner/name whose releases carry the published archives.
const Repository = "fullfran/claudeops-tui"

// GitHubFetcher resolves the newest release that actually has archives
// attached.
type GitHubFetcher struct {
	BaseURL string
	Repo    string
	HTTP    *http.Client
}

func (f GitHubFetcher) base() string {
	if f.BaseURL != "" {
		return strings.TrimRight(f.BaseURL, "/")
	}
	return DefaultGitHubAPI
}

func (f GitHubFetcher) repo() string {
	if f.Repo != "" {
		return f.Repo
	}
	return Repository
}

func (f GitHubFetcher) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// githubRelease is the subset of the releases payload that matters here.
type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// Latest returns the newest published release carrying a checksums.txt.
//
// checksums.txt is the file the installer verifies against, so a release
// without one cannot be installed no matter what its tag says. Requiring it
// here means an incomplete or legacy release is skipped rather than offered
// and then failed on.
func (f GitHubFetcher) Latest(ctx context.Context) (Release, error) {
	body, err := f.get(ctx, "/repos/"+f.repo()+"/releases?per_page=20")
	if err != nil {
		return Release{}, err
	}

	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return Release{}, fmt.Errorf("could not read the release list: %w", err)
	}

	best := ""
	for _, rel := range releases {
		if rel.Draft || rel.Prerelease || !hasChecksums(rel) {
			continue
		}
		v := strings.TrimPrefix(rel.TagName, "v")
		if _, ok := parseSemver(v); !ok {
			continue
		}
		if best == "" || semverLT(best, v) {
			best = v
		}
	}
	if best == "" {
		return Release{}, fmt.Errorf("no published release carries downloadable archives")
	}
	return Release{Version: best}, nil
}

func hasChecksums(rel githubRelease) bool {
	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			return true
		}
	}
	return false
}

func (f GitHubFetcher) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.base()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github returned HTTP %d for %s", resp.StatusCode, path)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
