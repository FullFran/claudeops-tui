package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeGeminiCreds(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth_creds.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write oauth_creds.json: %v", err)
	}
	return path
}

func TestGeminiAvailable(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"access token present", `{"access_token":"ya29.x"}`, true},
		{"empty access token", `{"access_token":""}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gemini{
				CredsPath:        writeGeminiCreds(t, tt.body),
				OpencodeAuthPath: filepath.Join(t.TempDir(), "no-opencode.json"),
				Now:              fixedNow,
			}
			if got := g.Available(); got != tt.want {
				t.Errorf("Available() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGeminiFetch(t *testing.T) {
	const payload = `{
		"quotaBuckets": [
			{"modelId": "gemini-2.5-pro", "remainingFraction": 0.25, "resetTime": "2026-07-11T18:00:00Z"},
			{"modelId": "gemini-2.5-flash", "remainingFraction": 0.90}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ya29.x" {
			t.Errorf("Authorization = %q, want Bearer ya29.x", got)
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	g := &Gemini{
		CredsPath: writeGeminiCreds(t, `{"access_token":"ya29.x"}`),
		QuotaURL:  srv.URL,
		HTTP:      srv.Client(),
		Now:       fixedNow,
	}
	u, err := g.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if u.Provider != "Gemini" {
		t.Errorf("Provider = %q, want Gemini", u.Provider)
	}
	if len(u.Windows) != 2 {
		t.Fatalf("got %d windows, want 2", len(u.Windows))
	}
	// remainingFraction 0.25 -> utilization 75
	if u.Windows[0].Label != "gemini-2.5-pro" || u.Windows[0].Utilization != 75 {
		t.Errorf("window[0] = %+v, want gemini-2.5-pro/75", u.Windows[0])
	}
	wantReset := time.Date(2026, 7, 11, 18, 0, 0, 0, time.UTC)
	if !u.Windows[0].ResetsAt.Equal(wantReset) {
		t.Errorf("window[0].ResetsAt = %v, want %v", u.Windows[0].ResetsAt, wantReset)
	}
	// remainingFraction 0.90 -> utilization ~10
	if u.Windows[1].Label != "gemini-2.5-flash" || u.Windows[1].Utilization < 9.99 || u.Windows[1].Utilization > 10.01 {
		t.Errorf("window[1] = %+v, want gemini-2.5-flash/~10", u.Windows[1])
	}
}

func TestGeminiFetchBareBuckets(t *testing.T) {
	const payload = `{"buckets": [{"modelId": "gemini-2.5-pro", "remainingFraction": 0.5}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()
	g := &Gemini{
		CredsPath: writeGeminiCreds(t, `{"access_token":"ya29.x"}`),
		QuotaURL:  srv.URL,
		HTTP:      srv.Client(),
		Now:       fixedNow,
	}
	u, err := g.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(u.Windows) != 1 || u.Windows[0].Utilization != 50 {
		t.Fatalf("got %+v, want single 50%% window", u.Windows)
	}
}
