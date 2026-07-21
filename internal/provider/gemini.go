package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultGeminiQuotaURL is the Code Assist quota endpoint used by the Gemini CLI.
const DefaultGeminiQuotaURL = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"

// ErrGeminiAuthExpired indicates the stored Gemini OAuth token was rejected.
var ErrGeminiAuthExpired = errors.New("gemini oauth token rejected; run `gemini` to re-authenticate")

// geminiCreds mirrors ~/.gemini/oauth_creds.json.
type geminiCreds struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	ExpiryDate  int64  `json:"expiry_date"` // unix millis
}

// geminiBucket is one per-model quota bucket in the retrieveUserQuota response.
type geminiBucket struct {
	ModelID           string   `json:"modelId"`
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
}

// geminiQuotaResp accepts either the `quotaBuckets` or bare `buckets` array.
type geminiQuotaResp struct {
	QuotaBuckets []geminiBucket `json:"quotaBuckets"`
	Buckets      []geminiBucket `json:"buckets"`
}

// Gemini fetches Google Gemini (Code Assist) quota using the Gemini CLI's
// OAuth credentials.
type Gemini struct {
	// CredsPath is the path to oauth_creds.json (default ~/.gemini/oauth_creds.json).
	CredsPath string
	// OpencodeAuthPath is the fallback credential source: opencode's auth.json,
	// which may hold a `google` OAuth session. Empty uses the conventional path.
	OpencodeAuthPath string
	QuotaURL         string
	HTTP             *http.Client
	Now              func() time.Time
}

// NewGemini builds a Gemini provider using the conventional creds location.
func NewGemini() *Gemini {
	return &Gemini{
		CredsPath: geminiCredsPath(),
		QuotaURL:  DefaultGeminiQuotaURL,
		HTTP:      &http.Client{Timeout: 10 * time.Second},
		Now:       time.Now,
	}
}

// geminiCredsPath resolves oauth_creds.json honoring $GEMINI_HOME, then ~/.gemini.
func geminiCredsPath() string {
	if v := os.Getenv("GEMINI_HOME"); v != "" {
		return filepath.Join(v, "oauth_creds.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".gemini", "oauth_creds.json")
	}
	return filepath.Join(home, ".gemini", "oauth_creds.json")
}

// Name implements Provider.
func (g *Gemini) Name() string { return "Gemini" }

// Available reports whether Gemini OAuth credentials are present on disk.
func (g *Gemini) Available() bool {
	creds, err := g.loadCreds()
	return err == nil && creds.AccessToken != ""
}

func (g *Gemini) loadCreds() (*geminiCreds, error) {
	path := g.CredsPath
	if path == "" {
		path = geminiCredsPath()
	}
	if b, err := os.ReadFile(path); err == nil {
		var c geminiCreds
		if err := json.Unmarshal(b, &c); err == nil && c.AccessToken != "" {
			return &c, nil
		}
	}
	// Fallback: reuse a `google` OAuth session logged in via opencode.
	if oc, err := LoadOpencodeAuth(g.OpencodeAuthPath); err == nil {
		if e, ok := oc["google"]; ok && e.Type == "oauth" && e.Access != "" {
			return &geminiCreds{AccessToken: e.Access, ExpiryDate: e.Expires}, nil
		}
	}
	return nil, os.ErrNotExist
}

// Fetch retrieves and normalizes the Gemini quota snapshot.
func (g *Gemini) Fetch(ctx context.Context) (Usage, error) {
	creds, err := g.loadCreds()
	if err != nil {
		return Usage{}, err
	}
	if creds.AccessToken == "" {
		return Usage{}, ErrGeminiAuthExpired
	}

	url := g.QuotaURL
	if url == "" {
		url = DefaultGeminiQuotaURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claudeops/0.1")

	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return Usage{}, ErrGeminiAuthExpired
	}
	if resp.StatusCode != http.StatusOK {
		return Usage{}, fmt.Errorf("gemini quota endpoint returned HTTP %d", resp.StatusCode)
	}

	var body geminiQuotaResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Usage{}, err
	}
	return g.toUsage(body), nil
}

func (g *Gemini) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *Gemini) toUsage(body geminiQuotaResp) Usage {
	buckets := body.QuotaBuckets
	if len(buckets) == 0 {
		buckets = body.Buckets
	}

	windows := make([]Window, 0, len(buckets))
	for _, b := range buckets {
		if b.RemainingFraction == nil {
			continue
		}
		label := b.ModelID
		if label == "" {
			label = "quota"
		}
		var resets time.Time
		if b.ResetTime != "" {
			if t, err := time.Parse(time.RFC3339, b.ResetTime); err == nil {
				resets = t
			}
		}
		windows = append(windows, Window{
			Label:       label,
			Utilization: (1 - *b.RemainingFraction) * 100,
			ResetsAt:    resets,
		})
	}
	return Usage{Provider: "Gemini", Windows: windows, FetchedAt: g.now()}
}
