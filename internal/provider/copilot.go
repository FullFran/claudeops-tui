package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DefaultCopilotUsageURL is GitHub's internal Copilot usage/quota endpoint.
const DefaultCopilotUsageURL = "https://api.github.com/copilot_internal/user"

// ErrCopilotAuthExpired indicates the stored Copilot OAuth token was rejected.
var ErrCopilotAuthExpired = errors.New("copilot oauth token rejected; re-authenticate GitHub Copilot")

// copilotQuota is one quota lane in the Copilot user response. GitHub reports
// percent_remaining; we invert it to utilization for the bars.
type copilotQuota struct {
	PercentRemaining *float64 `json:"percent_remaining"`
	Unlimited        bool     `json:"unlimited"`
	Remaining        *float64 `json:"remaining"`
}

// copilotUserResp is the /copilot_internal/user payload (permissive subset).
type copilotUserResp struct {
	CopilotPlan    string                  `json:"copilot_plan"`
	QuotaResetDate string                  `json:"quota_reset_date"`
	QuotaSnapshots map[string]copilotQuota `json:"quota_snapshots"`
}

// Copilot fetches GitHub Copilot quota using the device-flow OAuth token that
// the Copilot editor/CLI stores under ~/.config/github-copilot.
type Copilot struct {
	// AppsPath is the path to apps.json (default ~/.config/github-copilot/apps.json).
	AppsPath string
	UsageURL string
	HTTP     *http.Client
	Now      func() time.Time
}

// NewCopilot builds a Copilot provider using the conventional token location.
func NewCopilot() *Copilot {
	return &Copilot{
		AppsPath: copilotAppsPath(),
		UsageURL: DefaultCopilotUsageURL,
		HTTP:     &http.Client{Timeout: 10 * time.Second},
		Now:      time.Now,
	}
}

// copilotAppsPath resolves apps.json honoring $XDG_CONFIG_HOME, then ~/.config.
func copilotAppsPath() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "github-copilot", "apps.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "github-copilot", "apps.json")
	}
	return filepath.Join(home, ".config", "github-copilot", "apps.json")
}

// Name implements Provider.
func (c *Copilot) Name() string { return "Copilot" }

// Available reports whether a Copilot OAuth token is present on disk.
func (c *Copilot) Available() bool {
	tok, _ := c.token()
	return tok != ""
}

// token extracts the first non-empty oauth_token from apps.json (keys look
// like "github.com:Iv1.xxx"). Falls back to the legacy hosts.json shape.
func (c *Copilot) token() (string, error) {
	path := c.AppsPath
	if path == "" {
		path = copilotAppsPath()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var apps map[string]struct {
		OAuthToken string `json:"oauth_token"`
	}
	if err := json.Unmarshal(b, &apps); err != nil {
		return "", err
	}
	// Deterministic order: sort keys so the same token is picked each run.
	keys := make([]string, 0, len(apps))
	for k := range apps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if apps[k].OAuthToken != "" {
			return apps[k].OAuthToken, nil
		}
	}
	return "", ErrCopilotAuthExpired
}

// Fetch retrieves and normalizes the Copilot quota snapshot.
func (c *Copilot) Fetch(ctx context.Context) (Usage, error) {
	tok, err := c.token()
	if err != nil {
		return Usage{}, err
	}

	url := c.UsageURL
	if url == "" {
		url = DefaultCopilotUsageURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "token "+tok)
	req.Header.Set("User-Agent", "claudeops/0.1")
	req.Header.Set("Accept", "application/json")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return Usage{}, ErrCopilotAuthExpired
	}
	if resp.StatusCode != http.StatusOK {
		return Usage{}, fmt.Errorf("copilot usage endpoint returned HTTP %d", resp.StatusCode)
	}

	var body copilotUserResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Usage{}, err
	}
	return c.toUsage(body), nil
}

func (c *Copilot) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// copilotLanePriority orders the well-known lanes first; extras follow sorted.
var copilotLanePriority = []string{"premium_interactions", "chat", "completions"}

func (c *Copilot) toUsage(body copilotUserResp) Usage {
	var resets time.Time
	if body.QuotaResetDate != "" {
		if t, err := time.Parse("2006-01-02", body.QuotaResetDate); err == nil {
			resets = t
		}
	}

	seen := map[string]bool{}
	windows := make([]Window, 0, len(body.QuotaSnapshots))
	appendLane := func(name string, q copilotQuota) {
		if q.Unlimited || q.PercentRemaining == nil {
			return
		}
		windows = append(windows, Window{
			Label:       name,
			Utilization: 100 - *q.PercentRemaining,
			ResetsAt:    resets,
		})
	}

	for _, name := range copilotLanePriority {
		if q, ok := body.QuotaSnapshots[name]; ok {
			appendLane(name, q)
			seen[name] = true
		}
	}
	// Any remaining lanes, in sorted order for determinism.
	extra := make([]string, 0)
	for name := range body.QuotaSnapshots {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		appendLane(name, body.QuotaSnapshots[name])
	}

	u := Usage{Provider: "Copilot", Windows: windows, FetchedAt: c.now()}
	if body.CopilotPlan != "" {
		u.Note = "plan: " + body.CopilotPlan
	}
	return u
}
