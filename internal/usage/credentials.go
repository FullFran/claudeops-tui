// Package usage provides a client for Anthropic's undocumented
// /api/oauth/usage endpoint, including OAuth token refresh and atomic
// credential I/O on the shared ~/.claude/.credentials.json file.
package usage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Credentials mirrors the on-disk shape of ~/.claude/.credentials.json.
// We preserve unknown fields by round-tripping through json.RawMessage.
type Credentials struct {
	ClaudeAiOauth *OAuthBlock                `json:"claudeAiOauth,omitempty"`
	Other         map[string]json.RawMessage `json:"-"`
}

// OAuthBlock holds the access/refresh tokens.
type OAuthBlock struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"` // unix seconds
}

// ErrNoOAuth means the user is not logged into a Pro/Max plan via OAuth.
var ErrNoOAuth = errors.New("no claudeAiOauth block in credentials")

// LoadCredentials reads and decodes the credentials file.
func LoadCredentials(path string) (*Credentials, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	c := &Credentials{Other: map[string]json.RawMessage{}}
	for k, v := range raw {
		if k == "claudeAiOauth" {
			var ob OAuthBlock
			if err := json.Unmarshal(v, &ob); err != nil {
				return nil, err
			}
			c.ClaudeAiOauth = &ob
			continue
		}
		c.Other[k] = v
	}
	if c.ClaudeAiOauth == nil {
		return c, ErrNoOAuth
	}
	return c, nil
}

// IsExpired reports whether the access token is expired or expires within `skew`.
func (c *Credentials) IsExpired(skew time.Duration) bool {
	if c.ClaudeAiOauth == nil {
		return true
	}
	exp := time.Unix(c.ClaudeAiOauth.ExpiresAt, 0)
	return time.Now().Add(skew).After(exp)
}

// SaveCredentials writes credentials atomically: temp file in the same dir,
// fsync, rename. Mode 0600 is preserved. The original file is left untouched
// on any error.
func SaveCredentials(path string, c *Credentials) error {
	// Re-assemble the JSON object preserving unknown keys.
	out := map[string]json.RawMessage{}
	for k, v := range c.Other {
		out[k] = v
	}
	if c.ClaudeAiOauth != nil {
		b, err := json.Marshal(c.ClaudeAiOauth)
		if err != nil {
			return err
		}
		out["claudeAiOauth"] = b
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".credentials.tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() { _ = os.Remove(tmp) }

	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
