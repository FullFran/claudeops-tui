package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeCopilotApps(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write apps.json: %v", err)
	}
	return path
}

func TestCopilotAvailable(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"token present", `{"github.com:Iv1.abc":{"oauth_token":"gho_x"}}`, true},
		{"empty token", `{"github.com:Iv1.abc":{"oauth_token":""}}`, false},
		{"no entries", `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Copilot{AppsPath: writeCopilotApps(t, tt.body), Now: fixedNow}
			if got := c.Available(); got != tt.want {
				t.Errorf("Available() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCopilotFetch(t *testing.T) {
	const payload = `{
		"copilot_plan": "business",
		"quota_reset_date": "2026-08-01",
		"quota_snapshots": {
			"chat":                 {"percent_remaining": 80.0},
			"premium_interactions": {"percent_remaining": 40.0},
			"completions":          {"unlimited": true, "percent_remaining": 100.0}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token gho_x" {
			t.Errorf("Authorization = %q, want token gho_x", got)
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := &Copilot{
		AppsPath: writeCopilotApps(t, `{"github.com:Iv1.abc":{"oauth_token":"gho_x"}}`),
		UsageURL: srv.URL,
		HTTP:     srv.Client(),
		Now:      fixedNow,
	}
	u, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if u.Provider != "Copilot" {
		t.Errorf("Provider = %q, want Copilot", u.Provider)
	}
	if u.Note != "plan: business" {
		t.Errorf("Note = %q, want plan: business", u.Note)
	}
	// completions is unlimited -> skipped. premium_interactions first (priority).
	if len(u.Windows) != 2 {
		t.Fatalf("got %d windows, want 2: %+v", len(u.Windows), u.Windows)
	}
	if u.Windows[0].Label != "premium_interactions" || u.Windows[0].Utilization != 60 {
		t.Errorf("window[0] = %+v, want premium_interactions/60", u.Windows[0])
	}
	if u.Windows[1].Label != "chat" || u.Windows[1].Utilization != 20 {
		t.Errorf("window[1] = %+v, want chat/20", u.Windows[1])
	}
}

func TestCopilotFetchUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &Copilot{
		AppsPath: writeCopilotApps(t, `{"github.com:x":{"oauth_token":"gho_x"}}`),
		UsageURL: srv.URL,
		HTTP:     srv.Client(),
		Now:      fixedNow,
	}
	if _, err := c.Fetch(context.Background()); err != ErrCopilotAuthExpired {
		t.Errorf("Fetch err = %v, want ErrCopilotAuthExpired", err)
	}
}
