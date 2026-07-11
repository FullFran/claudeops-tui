package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// writeOpencodeAuth writes an opencode-shaped auth.json fixture.
func writeOpencodeAuth(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write opencode auth.json: %v", err)
	}
	return path
}

func TestLoadOpencodeAuth(t *testing.T) {
	const body = `{
		"openai": {"type": "oauth", "access": "oai-access", "refresh": "oai-refresh", "accountId": "acc-123", "expires": 111},
		"google": {"type": "oauth", "access": "goog-access", "expires": 222},
		"groq":   {"type": "api", "key": "gsk_x"}
	}`
	m, err := LoadOpencodeAuth(writeOpencodeAuth(t, body))
	if err != nil {
		t.Fatalf("LoadOpencodeAuth: %v", err)
	}
	if m["openai"].Type != "oauth" || m["openai"].Access != "oai-access" || m["openai"].AccountID != "acc-123" {
		t.Errorf("openai = %+v", m["openai"])
	}
	if m["google"].Access != "goog-access" {
		t.Errorf("google access = %q", m["google"].Access)
	}
	if m["groq"].Type != "api" || m["groq"].Key != "gsk_x" {
		t.Errorf("groq = %+v", m["groq"])
	}
}

// TestCodexUsesOpencodeFallback covers the reported use case: the user has no
// ~/.codex/auth.json but is logged into `openai` via opencode.
func TestCodexUsesOpencodeFallback(t *testing.T) {
	const payload = `{"rate_limit": {"primary_window": {"used_percent": 20.0, "window_minutes": 300, "resets_in_seconds": 120}}}`
	var gotAuth, gotAccount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	opencodeAuth := writeOpencodeAuth(t, `{"openai": {"type": "oauth", "access": "oai-access", "accountId": "acc-123"}}`)

	c := &Codex{
		AuthPath:         filepath.Join(t.TempDir(), "missing-codex-auth.json"), // no Codex CLI creds
		OpencodeAuthPath: opencodeAuth,
		UsageURL:         srv.URL,
		HTTP:             srv.Client(),
		Now:              fixedNow,
	}

	if !c.Available() {
		t.Fatal("Available() = false; want true via opencode fallback")
	}
	u, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotAuth != "Bearer oai-access" {
		t.Errorf("Authorization = %q, want Bearer oai-access", gotAuth)
	}
	if gotAccount != "acc-123" {
		t.Errorf("chatgpt-account-id = %q, want acc-123", gotAccount)
	}
	if len(u.Windows) != 1 || u.Windows[0].Utilization != 20 {
		t.Fatalf("got %+v, want single 20%% window", u.Windows)
	}
}

// TestCodexPrefersCodexCLIOverOpencode verifies the native Codex CLI creds win
// when both sources are present.
func TestCodexPrefersCodexCLIOverOpencode(t *testing.T) {
	c := &Codex{
		AuthPath:         writeAuth(t, `{"tokens":{"access_token":"codex-cli-tok","account_id":"cli-acc"}}`),
		OpencodeAuthPath: writeOpencodeAuth(t, `{"openai":{"type":"oauth","access":"opencode-tok"}}`),
		Now:              fixedNow,
	}
	cr, err := c.creds()
	if err != nil {
		t.Fatalf("creds: %v", err)
	}
	if cr.AccessToken != "codex-cli-tok" || cr.Source != "codex-cli" {
		t.Errorf("creds = %+v, want codex-cli-tok/codex-cli", cr)
	}
}

// TestGeminiUsesOpencodeFallback verifies Gemini reuses a `google` opencode session.
func TestGeminiUsesOpencodeFallback(t *testing.T) {
	const payload = `{"quotaBuckets": [{"modelId": "gemini-2.5-pro", "remainingFraction": 0.5}]}`
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	g := &Gemini{
		CredsPath:        filepath.Join(t.TempDir(), "missing-gemini.json"),
		OpencodeAuthPath: writeOpencodeAuth(t, `{"google": {"type": "oauth", "access": "goog-access"}}`),
		QuotaURL:         srv.URL,
		HTTP:             srv.Client(),
		Now:              fixedNow,
	}
	if !g.Available() {
		t.Fatal("Available() = false; want true via opencode google fallback")
	}
	u, err := g.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotAuth != "Bearer goog-access" {
		t.Errorf("Authorization = %q, want Bearer goog-access", gotAuth)
	}
	if len(u.Windows) != 1 || u.Windows[0].Utilization != 50 {
		t.Fatalf("got %+v, want single 50%% window", u.Windows)
	}
}
