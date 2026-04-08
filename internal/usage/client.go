package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultUsageURL is the undocumented endpoint Claude Code's /usage uses.
const DefaultUsageURL = "https://api.anthropic.com/api/oauth/usage"

// DefaultRefreshURL is the OAuth refresh endpoint.
const DefaultRefreshURL = "https://console.anthropic.com/v1/oauth/token"

// AnthropicBetaHeader is the beta header required by the OAuth endpoints.
const AnthropicBetaHeader = "oauth-2025-04-20"

// ErrUsageUnavailable indicates the user cannot use the usage endpoint
// (e.g., they are using ANTHROPIC_API_KEY instead of OAuth).
var ErrUsageUnavailable = errors.New("usage endpoint unavailable (no OAuth)")

// ErrAuthExpired indicates refresh failed and the user must re-login.
var ErrAuthExpired = errors.New("oauth refresh failed; run `claude /login`")

// Bucket is one quota window. Anthropic returns null for buckets that do
// not apply to the current plan, so callers must check for nil.
type Bucket struct {
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// ExtraUsage describes the optional pay-as-you-go credit pool.
type ExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

// Snapshot is the parsed response from the usage endpoint.
//
// The Anthropic OAuth /api/oauth/usage endpoint returns a struct of named
// buckets, any of which may be null for plans that don't have that quota.
// Per-model buckets (opus, sonnet, ...) are nil when the plan doesn't track
// them separately.
type Snapshot struct {
	FiveHour       *Bucket     `json:"five_hour"`
	SevenDay       *Bucket     `json:"seven_day"`
	SevenDayOpus   *Bucket     `json:"seven_day_opus"`
	SevenDaySonnet *Bucket     `json:"seven_day_sonnet"`
	ExtraUsage     *ExtraUsage `json:"extra_usage"`
	FetchedAt      time.Time
}

// PerModelBuckets returns the non-nil per-model 7-day buckets in display order.
// Used by the TUI to render only the buckets that actually apply to the user.
func (s Snapshot) PerModelBuckets() []NamedBucket {
	out := []NamedBucket{}
	if s.SevenDayOpus != nil {
		out = append(out, NamedBucket{Label: "7d (opus)", Bucket: *s.SevenDayOpus})
	}
	if s.SevenDaySonnet != nil {
		out = append(out, NamedBucket{Label: "7d (sonnet)", Bucket: *s.SevenDaySonnet})
	}
	return out
}

// NamedBucket pairs a Bucket with a display label.
type NamedBucket struct {
	Label  string
	Bucket Bucket
}

// Client fetches usage data with caching and OAuth refresh.
type Client struct {
	UsageURL    string
	RefreshURL  string
	UserAgent   string
	CredsPath   string
	HTTP        *http.Client
	CacheTTL    time.Duration

	mu       sync.Mutex
	cached   *Snapshot
}

// New builds a Client with sensible defaults. Pass the path to
// ~/.claude/.credentials.json.
func New(credsPath string) *Client {
	return &Client{
		UsageURL:   DefaultUsageURL,
		RefreshURL: DefaultRefreshURL,
		UserAgent:  "claudeops/0.1",
		CredsPath:  credsPath,
		HTTP:       &http.Client{Timeout: 10 * time.Second},
		CacheTTL:   60 * time.Second,
	}
}

// Get returns the latest snapshot, using a 60s cache. Refreshes the OAuth
// token if expired or on a 401 response.
func (c *Client) Get(ctx context.Context) (Snapshot, error) {
	c.mu.Lock()
	if c.cached != nil && time.Since(c.cached.FetchedAt) < c.CacheTTL {
		s := *c.cached
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()

	creds, err := LoadCredentials(c.CredsPath)
	if errors.Is(err, ErrNoOAuth) {
		return Snapshot{}, ErrUsageUnavailable
	}
	if err != nil {
		return Snapshot{}, err
	}

	// Proactive refresh if close to expiry.
	if creds.IsExpired(30 * time.Second) {
		if err := c.refresh(ctx, creds); err != nil {
			return Snapshot{}, err
		}
	}

	snap, status, err := c.fetch(ctx, creds.ClaudeAiOauth.AccessToken)
	if err != nil {
		return Snapshot{}, err
	}
	if status == http.StatusUnauthorized {
		// Retry once after refresh.
		if err := c.refresh(ctx, creds); err != nil {
			return Snapshot{}, err
		}
		snap, status, err = c.fetch(ctx, creds.ClaudeAiOauth.AccessToken)
		if err != nil {
			return Snapshot{}, err
		}
	}
	if status >= 400 {
		return Snapshot{}, fmt.Errorf("usage endpoint returned HTTP %d", status)
	}

	snap.FetchedAt = time.Now()
	c.mu.Lock()
	c.cached = &snap
	c.mu.Unlock()
	return snap, nil
}

func (c *Client) fetch(ctx context.Context, token string) (Snapshot, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.UsageURL, nil)
	if err != nil {
		return Snapshot{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", AnthropicBetaHeader)
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Snapshot{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, resp.StatusCode, nil
	}
	var s Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return Snapshot{}, resp.StatusCode, err
	}
	return s, resp.StatusCode, nil
}

type refreshResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (c *Client) refresh(ctx context.Context, creds *Credentials) error {
	body := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": creds.ClaudeAiOauth.RefreshToken,
	}
	bs, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RefreshURL, strings.NewReader(string(bs)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", AnthropicBetaHeader)
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return ErrAuthExpired
	}
	var r refreshResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if r.AccessToken == "" {
		return ErrAuthExpired
	}
	creds.ClaudeAiOauth.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		creds.ClaudeAiOauth.RefreshToken = r.RefreshToken
	}
	if r.ExpiresIn > 0 {
		creds.ClaudeAiOauth.ExpiresAt = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second).Unix()
	}
	if err := SaveCredentials(c.CredsPath, creds); err != nil {
		return err
	}
	return nil
}
