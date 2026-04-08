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

func TestSnapshotHandlesNullBuckets(t *testing.T) {
	dir := t.TempDir()
	credsPath := writeCreds(t, dir, validCreds(time.Now().Add(time.Hour)))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Real shape observed on a Sonnet-only Max plan: opus is null,
		// sonnet is populated, plus the noisy null fields Anthropic returns.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"five_hour":{"utilization":17.0,"resets_at":"2026-04-08T17:00:00Z"},
			"seven_day":{"utilization":34.0,"resets_at":"2026-04-13T17:00:00Z"},
			"seven_day_oauth_apps":null,
			"seven_day_opus":null,
			"seven_day_sonnet":{"utilization":13.0,"resets_at":"2026-04-13T18:00:00Z"},
			"seven_day_cowork":null,
			"iguana_necktie":null,
			"extra_usage":{"is_enabled":false,"monthly_limit":null,"used_credits":null,"utilization":null}
		}`))
	}))
	defer srv.Close()

	c := New(credsPath)
	c.UsageURL = srv.URL
	snap, err := c.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.FiveHour == nil || snap.FiveHour.Utilization != 17.0 {
		t.Errorf("five_hour wrong: %+v", snap.FiveHour)
	}
	if snap.SevenDay == nil || snap.SevenDay.Utilization != 34.0 {
		t.Errorf("seven_day wrong: %+v", snap.SevenDay)
	}
	if snap.SevenDayOpus != nil {
		t.Errorf("seven_day_opus should be nil, got %+v", snap.SevenDayOpus)
	}
	if snap.SevenDaySonnet == nil || snap.SevenDaySonnet.Utilization != 13.0 {
		t.Errorf("seven_day_sonnet wrong: %+v", snap.SevenDaySonnet)
	}
	pmb := snap.PerModelBuckets()
	if len(pmb) != 1 || pmb[0].Label != "7d (sonnet)" {
		t.Errorf("PerModelBuckets: want one sonnet entry, got %+v", pmb)
	}
	if snap.ExtraUsage == nil || snap.ExtraUsage.IsEnabled {
		t.Errorf("extra_usage decoded wrong: %+v", snap.ExtraUsage)
	}
}

func TestRateLimitNegativeCache(t *testing.T) {
	dir := t.TempDir()
	credsPath := writeCreds(t, dir, validCreds(time.Now().Add(time.Hour)))

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(credsPath)
	c.UsageURL = srv.URL

	// First call: hits the server, gets 429, populates negative cache.
	_, err := c.Get(context.Background())
	if err == nil {
		t.Fatal("expected rate-limit error on first call")
	}
	if !contains(err.Error(), "rate-limited") {
		t.Errorf("error message: %v", err)
	}

	// Subsequent calls within the backoff window must NOT hit the server.
	for i := 0; i < 5; i++ {
		if _, err := c.Get(context.Background()); err == nil {
			t.Fatal("expected cached rate-limit error")
		}
	}
	if calls.Load() != 1 {
		t.Errorf("server hit %d times; want 1 (negative cache should suppress retries)", calls.Load())
	}

	// Verify Retry-After was honored (~120s window).
	c.mu.Lock()
	remaining := time.Until(c.cachedErrUntil)
	c.mu.Unlock()
	if remaining < 110*time.Second || remaining > 121*time.Second {
		t.Errorf("Retry-After window: got %s, want ~120s", remaining)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
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
