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

func TestGenericAvailable(t *testing.T) {
	t.Setenv("CLAUDEOPS_TEST_TOKEN", "sk-xyz")

	tests := []struct {
		name string
		cfg  GenericConfig
		want bool
	}{
		{
			name: "token from env + url present",
			cfg:  GenericConfig{Name: "X", URL: "http://x", TokenEnv: "CLAUDEOPS_TEST_TOKEN"},
			want: true,
		},
		{
			name: "missing env token",
			cfg:  GenericConfig{Name: "X", URL: "http://x", TokenEnv: "CLAUDEOPS_MISSING_TOKEN"},
			want: false,
		},
		{
			name: "no url",
			cfg:  GenericConfig{Name: "X", TokenEnv: "CLAUDEOPS_TEST_TOKEN"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGeneric(tt.cfg)
			g.Now = fixedNow
			if got := g.Available(); got != tt.want {
				t.Errorf("Available() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenericTokenFromFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, []byte("  file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGeneric(GenericConfig{Name: "X", URL: "http://x", TokenFile: keyPath})
	tok, err := g.token()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok != "file-token" {
		t.Errorf("token = %q, want file-token (trimmed)", tok)
	}
}

func TestGenericFetchUsedLimit(t *testing.T) {
	// OpenRouter-style credits: utilization from used/limit.
	const payload = `{"data": {"total_credits": 100.0, "total_usage": 40.0}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-xyz" {
			t.Errorf("Authorization = %q, want Bearer sk-xyz", got)
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	t.Setenv("CLAUDEOPS_TEST_TOKEN", "sk-xyz")
	g := NewGeneric(GenericConfig{
		Name:     "OpenRouter",
		URL:      srv.URL,
		TokenEnv: "CLAUDEOPS_TEST_TOKEN",
		Windows: []GenericWindowSpec{
			{Label: "credits", UsedPath: "data.total_usage", LimitPath: "data.total_credits"},
		},
	})
	g.HTTP = srv.Client()
	g.Now = fixedNow

	u, err := g.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if u.Provider != "OpenRouter" {
		t.Errorf("Provider = %q, want OpenRouter", u.Provider)
	}
	if len(u.Windows) != 1 || u.Windows[0].Utilization != 40 {
		t.Fatalf("got %+v, want credits/40", u.Windows)
	}
}

func TestGenericFetchRemainAndReset(t *testing.T) {
	const payload = `{"quota": {"remaining_fraction": 0.3, "reset": "2026-07-11T18:00:00Z"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	t.Setenv("CLAUDEOPS_TEST_TOKEN", "sk-xyz")
	g := NewGeneric(GenericConfig{
		Name:     "Custom",
		URL:      srv.URL,
		TokenEnv: "CLAUDEOPS_TEST_TOKEN",
		Windows: []GenericWindowSpec{
			{Label: "monthly", RemainPath: "quota.remaining_fraction", ResetPath: "quota.reset"},
		},
	})
	g.HTTP = srv.Client()
	g.Now = fixedNow

	u, err := g.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(u.Windows) != 1 {
		t.Fatalf("got %d windows, want 1", len(u.Windows))
	}
	// remaining 0.3 -> utilization 70
	if u.Windows[0].Utilization != 70 {
		t.Errorf("Utilization = %v, want 70", u.Windows[0].Utilization)
	}
	wantReset := time.Date(2026, 7, 11, 18, 0, 0, 0, time.UTC)
	if !u.Windows[0].ResetsAt.Equal(wantReset) {
		t.Errorf("ResetsAt = %v, want %v", u.Windows[0].ResetsAt, wantReset)
	}
}

func TestLoadGeneric(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.toml")
	const cfg = `
[[provider]]
name = "OpenRouter"
url = "https://openrouter.ai/api/v1/credits"
token_env = "OPENROUTER_API_KEY"

  [[provider.window]]
  label = "credits"
  used_path = "data.total_usage"
  limit_path = "data.total_credits"

[[provider]]
name = "DeepSeek"
url = "https://api.deepseek.com/user/balance"
token_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	gens, err := LoadGeneric(path)
	if err != nil {
		t.Fatalf("LoadGeneric: %v", err)
	}
	if len(gens) != 2 {
		t.Fatalf("got %d providers, want 2", len(gens))
	}
	if gens[0].Name() != "OpenRouter" || gens[1].Name() != "DeepSeek" {
		t.Errorf("names = %q, %q; want OpenRouter, DeepSeek", gens[0].Name(), gens[1].Name())
	}
	if len(gens[0].Config.Windows) != 1 {
		t.Errorf("OpenRouter windows = %d, want 1", len(gens[0].Config.Windows))
	}
}

func TestLoadGenericMissingFileIsNil(t *testing.T) {
	gens, err := LoadGeneric(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("LoadGeneric on missing file: %v", err)
	}
	if gens != nil {
		t.Errorf("got %v, want nil for missing file", gens)
	}
}

func TestDotGet(t *testing.T) {
	root := map[string]any{
		"data": []any{
			map[string]any{"balance": 12.5},
		},
	}
	v, ok := dotGetFloat(root, "data.0.balance")
	if !ok || v != 12.5 {
		t.Errorf("dotGetFloat = %v, %v; want 12.5, true", v, ok)
	}
	if _, ok := dotGet(root, "data.5.balance"); ok {
		t.Error("expected out-of-range index to fail")
	}
}
