package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

// ErrCredentialsNotPersisted indicates a rotated token could not be written
// back to the shared credentials file, so it is lost once this process exits.
var ErrCredentialsNotPersisted = errors.New("refreshed oauth tokens could not be saved")

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
	UsageURL   string
	RefreshURL string
	UserAgent  string
	CredsPath  string
	HTTP       *http.Client
	CacheTTL   time.Duration
	// DefaultBackoff is used when the server returns 429/5xx without a
	// Retry-After header. Anthropic's undocumented usage endpoint is shared
	// with Claude Code itself, so the safest default is conservative.
	DefaultBackoff time.Duration

	// flight serializes the whole fetch/refresh path so N concurrent callers
	// (the TUI dispatches refreshCmd on every 2s tick) cause one refresh.
	flight sync.Mutex

	mu             sync.Mutex
	cached         *Snapshot
	cachedErr      error
	cachedErrUntil time.Time
}

// backoffError is returned while the negative cache window is active. It names
// the endpoint that failed and the real status (0 for transport failures) so
// the TUI never tells the user to re-login over a transient outage.
type backoffError struct {
	endpoint   string
	status     int
	cause      error
	retryAfter time.Duration
}

func (e *backoffError) Error() string {
	var what string
	switch {
	case e.status == http.StatusTooManyRequests:
		what = fmt.Sprintf("rate-limited (HTTP %d)", e.status)
	case e.status > 0:
		what = fmt.Sprintf("unavailable (HTTP %d)", e.status)
	default:
		what = fmt.Sprintf("unreachable (%v)", e.cause)
	}
	return fmt.Sprintf("%s %s; retrying in %s", e.endpoint, what, e.retryAfter.Round(time.Second))
}

func (e *backoffError) Unwrap() error { return e.cause }

// backoff negative-caches err for its retry window and returns it, so every
// caller in the window gets the same answer without touching the network.
func (c *Client) backoff(err *backoffError) error {
	if err.retryAfter <= 0 {
		err.retryAfter = c.DefaultBackoff
	}
	c.mu.Lock()
	c.cachedErr = err
	c.cachedErrUntil = time.Now().Add(err.retryAfter)
	c.mu.Unlock()
	return err
}

// New builds a Client with sensible defaults. Pass the path to
// ~/.claude/.credentials.json.
func New(credsPath string) *Client {
	return &Client{
		UsageURL:       DefaultUsageURL,
		RefreshURL:     DefaultRefreshURL,
		UserAgent:      "claudeops/0.1",
		CredsPath:      credsPath,
		HTTP:           &http.Client{Timeout: 10 * time.Second},
		CacheTTL:       5 * time.Minute,
		DefaultBackoff: 5 * time.Minute,
	}
}

// Get returns the latest snapshot, using the CacheTTL cache. Refreshes the
// OAuth token if expired or on a 401 response. Concurrent calls are
// single-flighted: the losers return whatever the winner cached.
func (c *Client) Get(ctx context.Context) (Snapshot, error) {
	if snap, err, ok := c.cachedResult(); ok {
		return snap, err
	}
	c.flight.Lock()
	defer c.flight.Unlock()
	// Re-check: a concurrent caller may have populated the cache while we
	// waited for the single-flight guard.
	if snap, err, ok := c.cachedResult(); ok {
		return snap, err
	}

	creds, err := c.credentials(ctx, "")
	if errors.Is(err, ErrNoOAuth) {
		return Snapshot{}, ErrUsageUnavailable
	}
	if err != nil {
		return Snapshot{}, err
	}

	snap, status, retryAfter, err := c.fetch(ctx, creds.ClaudeAiOauth.AccessToken)
	if err != nil {
		return Snapshot{}, c.backoff(&backoffError{endpoint: "usage endpoint", cause: err})
	}
	if status == http.StatusUnauthorized {
		// Retry once with a token refreshed under the file lock; another
		// process may already have rotated it while we were fetching.
		stale := creds.ClaudeAiOauth.AccessToken
		creds, err = c.credentials(ctx, stale)
		if err != nil {
			return Snapshot{}, err
		}
		snap, status, retryAfter, err = c.fetch(ctx, creds.ClaudeAiOauth.AccessToken)
		if err != nil {
			return Snapshot{}, c.backoff(&backoffError{endpoint: "usage endpoint", cause: err})
		}
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		// Negative-cache the failure so we stop hammering the endpoint.
		// Honor Retry-After when present; fall back to DefaultBackoff.
		return Snapshot{}, c.backoff(&backoffError{
			endpoint: "usage endpoint", status: status, retryAfter: retryAfter,
		})
	}
	if status >= 400 {
		return Snapshot{}, fmt.Errorf("usage endpoint returned HTTP %d", status)
	}

	snap.FetchedAt = time.Now()
	c.mu.Lock()
	c.cached = &snap
	// Clear any prior negative cache on success.
	c.cachedErr = nil
	c.cachedErrUntil = time.Time{}
	c.mu.Unlock()
	return snap, nil
}

// cachedResult reports the cached snapshot, or the negative-cached failure
// while its backoff window is still open.
func (c *Client) cachedResult() (Snapshot, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil && time.Since(c.cached.FetchedAt) < c.CacheTTL {
		return *c.cached, nil, true
	}
	// If a recent call hit 429/5xx or a transport failure, do not retry until
	// the backoff window has elapsed. This prevents us from compounding the
	// rate-limit (Anthropic's endpoint is shared with Claude Code itself).
	if c.cachedErr != nil && time.Now().Before(c.cachedErrUntil) {
		return Snapshot{}, c.cachedErr, true
	}
	return Snapshot{}, nil, false
}

// refreshSkew is how early we proactively refresh before the token expires.
const refreshSkew = 30 * time.Second

// credentials loads the credentials and, when needed, refreshes them — the
// whole load→refresh→save sequence runs under an exclusive file lock so we
// never clobber a rotation performed by Claude Code or a second claudeops.
//
// staleToken is the access token that just failed with a 401, or "" for the
// ordinary path. When the token on disk already differs from staleToken,
// another writer refreshed it for us and no network call is made.
func (c *Client) credentials(ctx context.Context, staleToken string) (*Credentials, error) {
	var creds *Credentials
	err := withFileLock(c.CredsPath, func() error {
		var err error
		creds, err = LoadCredentials(c.CredsPath)
		if err != nil {
			return err
		}
		if staleToken != "" {
			if creds.ClaudeAiOauth.AccessToken != staleToken {
				return nil
			}
			return c.refresh(ctx, creds)
		}
		if creds.IsExpired(refreshSkew) {
			return c.refresh(ctx, creds)
		}
		return nil
	})
	return creds, err
}

// fetch performs a single HTTP GET. It returns the parsed snapshot (on 200),
// the HTTP status, the parsed Retry-After duration (0 if absent), and any
// transport-level error.
func (c *Client) fetch(ctx context.Context, token string) (Snapshot, int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.UsageURL, nil)
	if err != nil {
		return Snapshot{}, 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", AnthropicBetaHeader)
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Snapshot{}, 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")), nil
	}
	var s Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return Snapshot{}, resp.StatusCode, 0, err
	}
	return s, resp.StatusCode, 0, nil
}

// parseRetryAfter accepts either delta-seconds or an HTTP-date and returns
// the resulting duration. Returns 0 when the header is missing or unparseable.
func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
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
		return c.backoff(&backoffError{endpoint: "token endpoint", cause: err})
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		// Only the OAuth error statuses mean the grant is really gone;
		// everything else is transient and must be backed off instead of
		// telling the user to re-login.
		switch resp.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
			return ErrAuthExpired
		}
		return c.backoff(&backoffError{
			endpoint:   "token endpoint",
			status:     resp.StatusCode,
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		})
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
		creds.ClaudeAiOauth.SetExpiresAt(time.Now().Add(time.Duration(r.ExpiresIn) * time.Second))
	}
	if err := SaveCredentials(c.CredsPath, creds); err != nil {
		// The server has already rotated the grant, so the tokens we just
		// received are the only valid ones and we could not persist them.
		// Say so loudly: the user may have to re-login.
		return fmt.Errorf("%w: %s (%v); run `claude /login` if Claude Code stops working",
			ErrCredentialsNotPersisted, c.CredsPath, err)
	}
	return nil
}
