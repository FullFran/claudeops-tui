package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixedNow gives the Codex provider a deterministic clock.
func fixedNow() time.Time { return time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC) }

// writeAuth writes an auth.json fixture into a temp dir and returns its path.
func writeAuth(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	return path
}

func TestCodexAvailable(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "oauth token present",
			body: `{"tokens":{"access_token":"tok"}}`,
			want: true,
		},
		{
			name: "api key only is not a subscription",
			body: `{"OPENAI_API_KEY":"sk-xxx"}`,
			want: false,
		},
		{
			name: "empty access token",
			body: `{"tokens":{"access_token":""}}`,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Codex{
				AuthPath:         writeAuth(t, tt.body),
				OpencodeAuthPath: filepath.Join(t.TempDir(), "no-opencode.json"),
				Now:              fixedNow,
			}
			if got := c.Available(); got != tt.want {
				t.Errorf("Available() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodexAvailableMissingFile(t *testing.T) {
	c := &Codex{
		AuthPath:         filepath.Join(t.TempDir(), "nope.json"),
		OpencodeAuthPath: filepath.Join(t.TempDir(), "no-opencode.json"),
		Now:              fixedNow,
	}
	if c.Available() {
		t.Error("Available() = true for missing auth.json, want false")
	}
}

func TestCodexFetch(t *testing.T) {
	const payload = `{
		"rate_limit": {
			"primary_window":   {"used_percent": 12.5, "window_minutes": 300,   "resets_in_seconds": 3600},
			"secondary_window": {"used_percent": 48.0, "window_minutes": 10080, "resets_in_seconds": 7200},
			"additional_rate_limits": [
				{"name": "gpt-5.3-codex-spark", "used_percent": 5.0, "resets_in_seconds": 600}
			]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := &Codex{
		AuthPath: writeAuth(t, `{"tokens":{"access_token":"tok"}}`),
		UsageURL: srv.URL,
		HTTP:     srv.Client(),
		Now:      fixedNow,
	}

	u, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if u.Provider != "Codex" {
		t.Errorf("Provider = %q, want Codex", u.Provider)
	}
	if len(u.Windows) != 3 {
		t.Fatalf("got %d windows, want 3", len(u.Windows))
	}

	// primary -> 5h from window_minutes=300
	if u.Windows[0].Label != "5h" || u.Windows[0].Utilization != 12.5 {
		t.Errorf("window[0] = %+v, want 5h/12.5", u.Windows[0])
	}
	wantReset := fixedNow().Add(3600 * time.Second)
	if !u.Windows[0].ResetsAt.Equal(wantReset) {
		t.Errorf("window[0].ResetsAt = %v, want %v", u.Windows[0].ResetsAt, wantReset)
	}
	// secondary -> 7d from window_minutes=10080
	if u.Windows[1].Label != "7d" {
		t.Errorf("window[1].Label = %q, want 7d", u.Windows[1].Label)
	}
	// additional -> uses its name
	if u.Windows[2].Label != "gpt-5.3-codex-spark" {
		t.Errorf("window[2].Label = %q, want gpt-5.3-codex-spark", u.Windows[2].Label)
	}
}

// TestCodexFetchLivePayload uses the real /wham/usage field names captured from
// a live ChatGPT-plan account (limit_window_seconds, reset_at, plan_type).
func TestCodexFetchLivePayload(t *testing.T) {
	const payload = `{
		"plan_type": "plus",
		"rate_limit": {
			"primary_window":   {"used_percent": 1, "limit_window_seconds": 18000,  "reset_after_seconds": 18000,  "reset_at": 1783813134},
			"secondary_window": {"used_percent": 0, "limit_window_seconds": 604800, "reset_after_seconds": 604800, "reset_at": 1784399934}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := &Codex{
		AuthPath: writeAuth(t, `{"tokens":{"access_token":"tok"}}`),
		UsageURL: srv.URL,
		HTTP:     srv.Client(),
		Now:      fixedNow,
	}
	u, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if u.Note != "plan: plus" {
		t.Errorf("Note = %q, want plan: plus", u.Note)
	}
	if len(u.Windows) != 2 {
		t.Fatalf("got %d windows, want 2", len(u.Windows))
	}
	// 18000s = 300min = 5h; 604800s = 7d.
	if u.Windows[0].Label != "5h" || u.Windows[0].Utilization != 1 {
		t.Errorf("window[0] = %+v, want 5h/1", u.Windows[0])
	}
	if u.Windows[1].Label != "7d" {
		t.Errorf("window[1].Label = %q, want 7d", u.Windows[1].Label)
	}
	// reset_at is an absolute unix timestamp.
	if !u.Windows[0].ResetsAt.Equal(time.Unix(1783813134, 0)) {
		t.Errorf("window[0].ResetsAt = %v, want %v", u.Windows[0].ResetsAt, time.Unix(1783813134, 0))
	}
}

func TestCodexFetchInlineLanes(t *testing.T) {
	// Some responses inline the lanes without the rate_limit wrapper.
	const payload = `{"primary": {"used_percent": 3.0, "window_minutes": 300, "resets_in_seconds": 60}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := &Codex{
		AuthPath: writeAuth(t, `{"tokens":{"access_token":"tok"}}`),
		UsageURL: srv.URL,
		HTTP:     srv.Client(),
		Now:      fixedNow,
	}
	u, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(u.Windows) != 1 || u.Windows[0].Utilization != 3.0 {
		t.Fatalf("got %+v, want single 3.0 window", u.Windows)
	}
}

func TestCodexFetchUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := &Codex{
		AuthPath: writeAuth(t, `{"tokens":{"access_token":"tok"}}`),
		// Pin the fallback at a path that does not exist. Left empty it resolves
		// to the real ~/.local/share/opencode/auth.json, so the test would read
		// the developer's own credentials and its result would depend on whether
		// they happen to be signed in to opencode.
		OpencodeAuthPath: filepath.Join(t.TempDir(), "absent.json"),
		UsageURL:         srv.URL,
		HTTP:             srv.Client(),
		Now:              fixedNow,
	}
	// errors.Is, not ==: the error names which sources were rejected, so it is
	// wrapped.
	if _, err := c.Fetch(context.Background()); !errors.Is(err, ErrCodexAuthExpired) {
		t.Errorf("Fetch err = %v, want ErrCodexAuthExpired", err)
	}
}

func TestCodexFallsBackWhenFirstSourceIsRejected(t *testing.T) {
	// The bug this guards: a stale ~/.codex/auth.json shadowed a working
	// opencode session. The file existed and held a token, so the fallback never
	// ran and the user was told to `codex login` while already signed in.
	const goodToken = "opencode-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":12,"limit_window_seconds":18000}}}`))
	}))
	defer srv.Close()

	ocPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(ocPath, []byte(`{"openai":{"type":"oauth","access":"`+goodToken+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &Codex{
		AuthPath:         writeAuth(t, `{"tokens":{"access_token":"stale"}}`),
		OpencodeAuthPath: ocPath,
		UsageURL:         srv.URL,
		HTTP:             srv.Client(),
		Now:              fixedNow,
	}
	u, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("a rejected first source must not fail the fetch: %v", err)
	}
	if len(u.Windows) == 0 || u.Windows[0].Utilization != 12 {
		t.Errorf("expected the opencode session's data, got %+v", u.Windows)
	}
}

func TestCodexNoCredentialsIsDistinctFromRejected(t *testing.T) {
	// "Nothing to try" and "everything was refused" need different remedies, so
	// they are different errors.
	c := &Codex{
		AuthPath:         filepath.Join(t.TempDir(), "absent.json"),
		OpencodeAuthPath: filepath.Join(t.TempDir(), "absent.json"),
		UsageURL:         "http://127.0.0.1:1",
		Now:              fixedNow,
	}
	_, err := c.Fetch(context.Background())
	if !errors.Is(err, ErrCodexNoAuth) {
		t.Errorf("got %v, want ErrCodexNoAuth", err)
	}
}

func TestLabelForMinutes(t *testing.T) {
	tests := []struct {
		minutes  int
		fallback string
		want     string
	}{
		{300, "session", "5h"},
		{10080, "weekly", "7d"},
		{60, "session", "1h"},
		{90, "session", "90m"},
		{0, "weekly", "weekly"},
	}
	for _, tt := range tests {
		if got := labelForMinutes(tt.minutes, tt.fallback); got != tt.want {
			t.Errorf("labelForMinutes(%d, %q) = %q, want %q", tt.minutes, tt.fallback, got, tt.want)
		}
	}
}
