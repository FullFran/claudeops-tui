package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultCodexUsageURL is the ChatGPT backend endpoint the Codex CLI polls for
// rate-limit / quota state. Same endpoint CodexBar uses.
const DefaultCodexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// ErrCodexAuthExpired indicates the stored Codex OAuth token was rejected and
// the user must re-authenticate the Codex CLI.
var ErrCodexAuthExpired = errors.New("codex oauth token rejected; run `codex login`")

// codexAuth mirrors the relevant parts of ~/.codex/auth.json.
type codexAuth struct {
	OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	Tokens       *struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

// codexWindow is one rate-limit lane as reported by the ChatGPT backend.
// The live /wham/usage payload uses limit_window_seconds / reset_after_seconds
// / reset_at; the *_minutes / resets_in_seconds aliases are kept for
// forward/backward compatibility since the endpoint is undocumented.
type codexWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"` // unix epoch seconds
	WindowMinutes      int     `json:"window_minutes"`
	ResetsInSecs       int64   `json:"resets_in_seconds"`
	Name               string  `json:"name"`
	Label              string  `json:"label"`
}

// codexRateLimit carries the primary/secondary lanes plus any model-specific
// extras. It accepts both the `*_window` and short naming variants.
type codexRateLimit struct {
	PrimaryWindow   *codexWindow  `json:"primary_window"`
	SecondaryWindow *codexWindow  `json:"secondary_window"`
	Primary         *codexWindow  `json:"primary"`
	Secondary       *codexWindow  `json:"secondary"`
	Additional      []codexWindow `json:"additional_rate_limits"`
}

// codexUsageResp is the /wham/usage payload. The lanes may be wrapped in a
// `rate_limit` object or inlined at the top level, so we accept both.
type codexUsageResp struct {
	RateLimit *codexRateLimit `json:"rate_limit"`
	PlanType  string          `json:"plan_type"`
	codexRateLimit
}

// Codex fetches ChatGPT-plan usage for the OpenAI Codex CLI.
type Codex struct {
	// AuthPath is the path to auth.json (default ~/.codex/auth.json or
	// $CODEX_HOME/auth.json).
	AuthPath string
	// OpencodeAuthPath is the fallback credential source: opencode's auth.json,
	// which may hold an `openai` OAuth session the user logged into via
	// opencode. Empty uses the conventional ~/.local/share/opencode location.
	OpencodeAuthPath string
	// UsageURL overrides the endpoint (for tests).
	UsageURL string
	// HTTP is the client used for the request.
	HTTP *http.Client
	// Now supplies the current time; injectable for deterministic tests.
	Now func() time.Time
}

// codexCreds is the resolved credential used to call the usage endpoint,
// regardless of which on-disk source it came from.
type codexCreds struct {
	AccessToken string
	AccountID   string
	Source      string // "codex-cli" | "opencode"
}

// NewCodex builds a Codex provider using the conventional auth.json location,
// with opencode's auth.json as a fallback credential source.
func NewCodex() *Codex {
	return &Codex{
		AuthPath: codexAuthPath(),
		UsageURL: DefaultCodexUsageURL,
		HTTP:     &http.Client{Timeout: 10 * time.Second},
		Now:      time.Now,
	}
}

// creds resolves the Codex access token, preferring the native Codex CLI
// auth.json and falling back to an `openai` OAuth session stored by opencode.
func (c *Codex) creds() (codexCreds, error) {
	if auth, err := c.loadAuth(); err == nil && auth.Tokens != nil && auth.Tokens.AccessToken != "" {
		return codexCreds{
			AccessToken: auth.Tokens.AccessToken,
			AccountID:   auth.Tokens.AccountID,
			Source:      "codex-cli",
		}, nil
	}
	if oc, err := LoadOpencodeAuth(c.OpencodeAuthPath); err == nil {
		if e, ok := oc["openai"]; ok && e.Type == "oauth" && e.Access != "" {
			return codexCreds{
				AccessToken: e.Access,
				AccountID:   e.AccountID,
				Source:      "opencode",
			}, nil
		}
	}
	return codexCreds{}, os.ErrNotExist
}

// codexAuthPath resolves auth.json honoring $CODEX_HOME, then ~/.codex.
func codexAuthPath() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return filepath.Join(v, "auth.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".codex", "auth.json")
	}
	return filepath.Join(home, ".codex", "auth.json")
}

// Name implements Provider.
func (c *Codex) Name() string { return "Codex" }

// Available reports whether an OAuth (ChatGPT-plan) token is present in either
// the Codex CLI or opencode credential store. API-key only installs have no
// subscription quota endpoint and are skipped.
func (c *Codex) Available() bool {
	cr, err := c.creds()
	return err == nil && cr.AccessToken != ""
}

// Fetch retrieves and normalizes the Codex usage snapshot.
func (c *Codex) Fetch(ctx context.Context) (Usage, error) {
	cr, err := c.creds()
	if err != nil || cr.AccessToken == "" {
		return Usage{}, ErrCodexAuthExpired
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.usageURL(), nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cr.AccessToken)
	// The ChatGPT backend scopes usage to the account; send the id when known.
	if cr.AccountID != "" {
		req.Header.Set("chatgpt-account-id", cr.AccountID)
	}
	req.Header.Set("User-Agent", "claudeops/0.1")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return Usage{}, ErrCodexAuthExpired
	}
	if resp.StatusCode != http.StatusOK {
		return Usage{}, fmt.Errorf("codex usage endpoint returned HTTP %d", resp.StatusCode)
	}

	var body codexUsageResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Usage{}, err
	}
	return c.toUsage(body), nil
}

func (c *Codex) loadAuth() (*codexAuth, error) {
	path := c.AuthPath
	if path == "" {
		path = codexAuthPath()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a codexAuth
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (c *Codex) usageURL() string {
	if c.UsageURL != "" {
		return c.UsageURL
	}
	return DefaultCodexUsageURL
}

func (c *Codex) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Codex) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// toUsage flattens the rate-limit lanes into normalized windows.
func (c *Codex) toUsage(body codexUsageResp) Usage {
	rl := body.RateLimit
	if rl == nil {
		rl = &body.codexRateLimit
	}

	windows := make([]Window, 0, 4)
	add := func(w *codexWindow, fallbackLabel string) {
		if w == nil {
			return
		}
		windows = append(windows, c.window(w, fallbackLabel))
	}

	// Accept either naming variant for the two primary lanes.
	if rl.PrimaryWindow != nil {
		add(rl.PrimaryWindow, "session")
	} else {
		add(rl.Primary, "session")
	}
	if rl.SecondaryWindow != nil {
		add(rl.SecondaryWindow, "weekly")
	} else {
		add(rl.Secondary, "weekly")
	}
	for i := range rl.Additional {
		add(&rl.Additional[i], "extra")
	}

	u := Usage{Provider: "Codex", Windows: windows, FetchedAt: c.now()}
	if body.PlanType != "" {
		u.Note = "plan: " + body.PlanType
	}
	return u
}

// window normalizes one lane, preferring an explicit label/name and falling
// back to a human window size derived from the window length.
func (c *Codex) window(w *codexWindow, fallback string) Window {
	label := firstNonEmpty(w.Label, w.Name)
	if label == "" {
		minutes := w.WindowMinutes
		if minutes == 0 && w.LimitWindowSeconds > 0 {
			minutes = int(w.LimitWindowSeconds / 60)
		}
		label = labelForMinutes(minutes, fallback)
	}
	var resets time.Time
	switch {
	case w.ResetAt > 0:
		resets = time.Unix(w.ResetAt, 0)
	case w.ResetAfterSeconds > 0:
		resets = c.now().Add(time.Duration(w.ResetAfterSeconds) * time.Second)
	case w.ResetsInSecs > 0:
		resets = c.now().Add(time.Duration(w.ResetsInSecs) * time.Second)
	}
	return Window{Label: label, Utilization: w.UsedPercent, ResetsAt: resets}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// labelForMinutes turns a window size into a compact label, falling back to a
// provided default when the size is unknown.
func labelForMinutes(minutes int, fallback string) string {
	switch {
	case minutes <= 0:
		return fallback
	case minutes%(60*24) == 0:
		return fmt.Sprintf("%dd", minutes/(60*24))
	case minutes%60 == 0:
		return fmt.Sprintf("%dh", minutes/60)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
