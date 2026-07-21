package usage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// realCredentials mirrors a full ~/.claude/.credentials.json as written by
// Claude Code: expiresAt in milliseconds plus the extra claudeAiOauth fields
// claudeops does not model.
const realCredentials = `{
  "claudeAiOauth": {
    "accessToken": "sk-ant-oat01-old",
    "refreshToken": "sk-ant-ort01-old",
    "expiresAt": 1781194533744,
    "rateLimitTier": "default_max_20x",
    "scopes": ["user:inference", "user:profile"],
    "subscriptionType": "max",
    "refreshTokenExpiresAt": 1783786533744
  },
  "mcpOAuth": {
    "some-server": {"accessToken": "mcp-token"}
  }
}`

func TestExpiresAtUnitDetection(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		expiresAt  int64
		wantMillis bool
		wantTime   time.Time
	}{
		{
			name:       "13-digit value is milliseconds",
			expiresAt:  now.Add(time.Hour).UnixMilli(),
			wantMillis: true,
			wantTime:   now.Add(time.Hour).Truncate(time.Millisecond),
		},
		{
			name:       "10-digit value is seconds",
			expiresAt:  now.Add(time.Hour).Unix(),
			wantMillis: false,
			wantTime:   now.Add(time.Hour).Truncate(time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ob OAuthBlock
			raw := []byte(`{"accessToken":"a","refreshToken":"r","expiresAt":` +
				strconv.FormatInt(tt.expiresAt, 10) + `}`)
			if err := json.Unmarshal(raw, &ob); err != nil {
				t.Fatal(err)
			}
			if ob.Millis != tt.wantMillis {
				t.Errorf("Millis = %v, want %v", ob.Millis, tt.wantMillis)
			}
			if got := ob.ExpiresAtTime().UTC(); !got.Equal(tt.wantTime) {
				t.Errorf("ExpiresAtTime() = %s, want %s", got, tt.wantTime)
			}
		})
	}
}

func TestIsExpiredHandlesMilliseconds(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt int64
		millis    bool
		want      bool
	}{
		{name: "past millisecond timestamp is expired", expiresAt: time.Now().Add(-time.Hour).UnixMilli(), millis: true, want: true},
		{name: "future millisecond timestamp is not expired", expiresAt: time.Now().Add(time.Hour).UnixMilli(), millis: true, want: false},
		{name: "past second timestamp is expired", expiresAt: time.Now().Add(-time.Hour).Unix(), millis: false, want: true},
		{name: "future second timestamp is not expired", expiresAt: time.Now().Add(time.Hour).Unix(), millis: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Credentials{ClaudeAiOauth: &OAuthBlock{ExpiresAt: tt.expiresAt, Millis: tt.millis}}
			if got := c.IsExpired(30 * time.Second); got != tt.want {
				t.Errorf("IsExpired = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetExpiresAtPreservesUnit(t *testing.T) {
	at := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		millis bool
		want   int64
	}{
		{name: "milliseconds file stays in milliseconds", millis: true, want: at.UnixMilli()},
		{name: "seconds file stays in seconds", millis: false, want: at.Unix()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ob := &OAuthBlock{Millis: tt.millis}
			ob.SetExpiresAt(at)
			if ob.ExpiresAt != tt.want {
				t.Errorf("ExpiresAt = %d, want %d", ob.ExpiresAt, tt.want)
			}
		})
	}
}

func TestSaveCredentialsPreservesOAuthFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, []byte(realCredentials), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	c.ClaudeAiOauth.AccessToken = "sk-ant-oat01-new"
	if err := SaveCredentials(path, c); err != nil {
		t.Fatal(err)
	}

	oauth, top := readOAuthBlock(t, path)
	tests := []struct {
		key  string
		want string
	}{
		{key: "rateLimitTier", want: `"default_max_20x"`},
		{key: "scopes", want: `["user:inference","user:profile"]`},
		{key: "subscriptionType", want: `"max"`},
		{key: "refreshTokenExpiresAt", want: `1783786533744`},
		{key: "expiresAt", want: `1781194533744`},
		{key: "refreshToken", want: `"sk-ant-ort01-old"`},
		{key: "accessToken", want: `"sk-ant-oat01-new"`},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, ok := oauth[tt.key]
			if !ok {
				t.Fatalf("claudeAiOauth.%s was dropped", tt.key)
			}
			if compact(t, got) != tt.want {
				t.Errorf("claudeAiOauth.%s = %s, want %s", tt.key, compact(t, got), tt.want)
			}
		})
	}
	if _, ok := top["mcpOAuth"]; !ok {
		t.Error("top-level mcpOAuth was dropped")
	}
}

// readOAuthBlock returns the claudeAiOauth object and the top-level object of
// the credentials file at path, both as raw JSON fields.
func readOAuthBlock(t *testing.T, path string) (map[string]json.RawMessage, map[string]json.RawMessage) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	var oauth map[string]json.RawMessage
	if err := json.Unmarshal(top["claudeAiOauth"], &oauth); err != nil {
		t.Fatal(err)
	}
	return oauth, top
}

func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
