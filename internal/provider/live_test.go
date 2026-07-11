package provider

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveCodexOpencode exercises the real Codex usage endpoint using whatever
// credentials are on disk (Codex CLI or opencode). It is skipped unless
// CLAUDEOPS_LIVE_CODEX=1, so normal runs never make network calls.
//
//	CLAUDEOPS_LIVE_CODEX=1 go test -run TestLiveCodexOpencode -v ./internal/provider/
func TestLiveCodexOpencode(t *testing.T) {
	if os.Getenv("CLAUDEOPS_LIVE_CODEX") == "" {
		t.Skip("set CLAUDEOPS_LIVE_CODEX=1 to run the live Codex check")
	}
	c := NewCodex()
	cr, err := c.creds()
	if err != nil {
		t.Fatalf("no Codex credentials resolved (Codex CLI or opencode): %v", err)
	}
	t.Logf("credential source: %s (account=%q, token len=%d)", cr.Source, cr.AccountID, len(cr.AccessToken))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	u, err := c.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	t.Logf("provider=%s windows=%d", u.Provider, len(u.Windows))
	for _, w := range u.Windows {
		t.Logf("  %-16s %.1f%%  resets=%s", w.Label, w.Utilization, w.ResetsAt.Format(time.RFC3339))
	}
}
