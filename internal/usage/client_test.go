package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func writeCreds(t *testing.T, dir string, c Credentials) string {
	t.Helper()
	p := filepath.Join(dir, ".credentials.json")
	if err := SaveCredentials(p, &c); err != nil {
		t.Fatal(err)
	}
	return p
}

func validCreds(exp time.Time) Credentials {
	return Credentials{
		ClaudeAiOauth: &OAuthBlock{
			AccessToken:  "sk-ant-oat01-old",
			RefreshToken: "sk-ant-ort01-rt",
			ExpiresAt:    exp.Unix(),
		},
		Other: map[string]json.RawMessage{},
	}
}

func TestGetHappyPath(t *testing.T) {
	dir := t.TempDir()
	credsPath := writeCreds(t, dir, validCreds(time.Now().Add(time.Hour)))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-ant-oat01-old" {
			t.Errorf("bad auth: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("anthropic-beta") != AnthropicBetaHeader {
			t.Errorf("missing beta header")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"five_hour":      map[string]any{"utilization": 6.0, "resets_at": "2026-04-08T18:59:59Z"},
			"seven_day":      map[string]any{"utilization": 35.0, "resets_at": "2026-04-14T16:59:59Z"},
			"seven_day_opus": map[string]any{"utilization": 12.0, "resets_at": "2026-04-14T17:59:59Z"},
		})
	}))
	defer srv.Close()

	c := New(credsPath)
	c.UsageURL = srv.URL
	snap, err := c.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.FiveHour.Utilization != 6.0 || snap.SevenDay.Utilization != 35.0 || snap.SevenDayOpus.Utilization != 12.0 {
		t.Errorf("snapshot wrong: %+v", snap)
	}
}

func TestGetCaches(t *testing.T) {
	dir := t.TempDir()
	credsPath := writeCreds(t, dir, validCreds(time.Now().Add(time.Hour)))
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"five_hour": map[string]any{"utilization": 1.0, "resets_at": "2026-04-08T18:59:59Z"},
			"seven_day": map[string]any{"utilization": 2.0, "resets_at": "2026-04-14T16:59:59Z"},
			"seven_day_opus": map[string]any{"utilization": 3.0, "resets_at": "2026-04-14T17:59:59Z"},
		})
	}))
	defer srv.Close()

	c := New(credsPath)
	c.UsageURL = srv.URL
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("calls: want 1 (cache) got %d", calls.Load())
	}
}

func TestRefreshOn401(t *testing.T) {
	dir := t.TempDir()
	credsPath := writeCreds(t, dir, validCreds(time.Now().Add(time.Hour)))

	var usageCalls atomic.Int64
	usage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := usageCalls.Add(1)
		if n == 1 {
			http.Error(w, "expired", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"five_hour":      map[string]any{"utilization": 9.0, "resets_at": "2026-04-08T18:59:59Z"},
			"seven_day":      map[string]any{"utilization": 9.0, "resets_at": "2026-04-14T16:59:59Z"},
			"seven_day_opus": map[string]any{"utilization": 9.0, "resets_at": "2026-04-14T17:59:59Z"},
		})
	}))
	defer usage.Close()

	refresh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "sk-ant-oat01-new",
			"refresh_token": "sk-ant-ort01-rt2",
			"expires_in":    3600,
		})
	}))
	defer refresh.Close()

	c := New(credsPath)
	c.UsageURL = usage.URL
	c.RefreshURL = refresh.URL
	snap, err := c.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.FiveHour.Utilization != 9.0 {
		t.Errorf("snap after refresh: %+v", snap)
	}
	if usageCalls.Load() != 2 {
		t.Errorf("expected 2 usage calls, got %d", usageCalls.Load())
	}

	// Credential file should now contain the new token, atomically replaced.
	c2, err := LoadCredentials(credsPath)
	if err != nil {
		t.Fatal(err)
	}
	if c2.ClaudeAiOauth.AccessToken != "sk-ant-oat01-new" {
		t.Errorf("token not refreshed on disk: %q", c2.ClaudeAiOauth.AccessToken)
	}
}

func TestNoOAuthDegradesGracefully(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".credentials.json")
	_ = os.WriteFile(p, []byte(`{"unrelated":"value"}`), 0o600)

	c := New(p)
	if _, err := c.Get(context.Background()); err != ErrUsageUnavailable {
		t.Errorf("want ErrUsageUnavailable, got %v", err)
	}
}

func TestSaveCredentialsAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".credentials.json")
	if err := SaveCredentials(p, &Credentials{
		ClaudeAiOauth: &OAuthBlock{AccessToken: "a", RefreshToken: "r", ExpiresAt: 1},
		Other:         map[string]json.RawMessage{"keep": json.RawMessage(`"yes"`)},
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode: want 0600 got %v", info.Mode().Perm())
	}
	c, err := LoadCredentials(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Other["keep"] == nil || string(c.Other["keep"]) != `"yes"` {
		t.Errorf("unknown keys not preserved")
	}
}
