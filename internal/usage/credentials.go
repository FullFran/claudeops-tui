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

// OAuthBlock holds the access/refresh tokens. Fields we do not model (scopes,
// subscriptionType, rateLimitTier, ...) are kept verbatim in Other so a refresh
// never strips them from the file Claude Code shares with us.
type OAuthBlock struct {
	AccessToken  string
	RefreshToken string
	// ExpiresAt is the raw on-disk value, whose unit is given by Millis.
	// Claude Code writes milliseconds; older claudeops installs wrote seconds.
	ExpiresAt int64
	Millis    bool
	Other     map[string]json.RawMessage
}

// millisThreshold separates epoch seconds from epoch milliseconds: any value
// above it is far beyond a plausible seconds timestamp (year 5138).
const millisThreshold = 1e11

// UnmarshalJSON decodes the block, detecting the expiresAt unit and retaining
// every other field untouched.
func (o *OAuthBlock) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*o = OAuthBlock{Other: map[string]json.RawMessage{}, Millis: true}
	for k, v := range raw {
		switch k {
		case "accessToken":
			if err := json.Unmarshal(v, &o.AccessToken); err != nil {
				return err
			}
		case "refreshToken":
			if err := json.Unmarshal(v, &o.RefreshToken); err != nil {
				return err
			}
		case "expiresAt":
			if err := json.Unmarshal(v, &o.ExpiresAt); err != nil {
				return err
			}
			o.Millis = o.ExpiresAt >= millisThreshold
		default:
			o.Other[k] = v
		}
	}
	return nil
}

// MarshalJSON re-assembles the block, overwriting only the fields we own.
func (o OAuthBlock) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	for k, v := range o.Other {
		out[k] = v
	}
	for k, v := range map[string]any{
		"accessToken":  o.AccessToken,
		"refreshToken": o.RefreshToken,
		"expiresAt":    o.ExpiresAt,
	} {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out[k] = b
	}
	return json.Marshal(out)
}

// ExpiresAtTime converts the raw expiresAt value using its detected unit.
func (o *OAuthBlock) ExpiresAtTime() time.Time {
	if o.Millis {
		return time.UnixMilli(o.ExpiresAt)
	}
	return time.Unix(o.ExpiresAt, 0)
}

// SetExpiresAt stores t in whatever unit the file already used, so we never
// hand Claude Code a value in a unit it does not expect.
func (o *OAuthBlock) SetExpiresAt(t time.Time) {
	if o.Millis {
		o.ExpiresAt = t.UnixMilli()
		return
	}
	o.ExpiresAt = t.Unix()
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
	return time.Now().Add(skew).After(c.ClaudeAiOauth.ExpiresAtTime())
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
