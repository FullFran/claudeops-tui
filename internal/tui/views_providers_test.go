package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/provider"
)

// TestRenderProviders covers the multi-provider subscription section: each
// provider is labeled, its windows rendered, and per-provider errors shown
// inline without hiding the others.
func TestRenderProviders(t *testing.T) {
	reset := time.Now().Add(time.Hour)

	tests := []struct {
		name       string
		results    []provider.Result
		wantSubstr []string
		notWant    []string
	}{
		{
			name:       "no providers yields empty section",
			results:    nil,
			wantSubstr: nil,
		},
		{
			name: "provider windows render with labels",
			results: []provider.Result{
				{Name: "Codex", Usage: provider.Usage{
					Provider: "Codex",
					Windows: []provider.Window{
						{Label: "5h", Utilization: 12.5, ResetsAt: reset},
						{Label: "7d", Utilization: 48.0, ResetsAt: reset},
					},
				}},
			},
			wantSubstr: []string{"Codex", "5h", "7d", "12.5%", "48.0%"},
		},
		{
			name: "per-provider error shown, others still render",
			results: []provider.Result{
				{Name: "Codex", Err: errors.New("token rejected")},
				{Name: "Cursor", Usage: provider.Usage{
					Provider: "Cursor",
					Windows:  []provider.Window{{Label: "monthly", Utilization: 5.0}},
				}},
			},
			wantSubstr: []string{"Codex", "token rejected", "Cursor", "monthly"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderProviders(tt.results)
			for _, want := range tt.wantSubstr {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
			for _, no := range tt.notWant {
				if strings.Contains(out, no) {
					t.Errorf("output unexpectedly contains %q:\n%s", no, out)
				}
			}
			if len(tt.results) == 0 && out != "" {
				t.Errorf("expected empty output for no providers, got %q", out)
			}
		})
	}
}
