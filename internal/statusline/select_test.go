package statusline

import (
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

func codexUsage(util float64) provider.Usage {
	return provider.Usage{
		Provider: "Codex",
		Windows:  []provider.Window{{Label: "5h", Utilization: util, ResetsAt: fixedNow.Add(time.Hour)}},
		Note:     "plan: plus",
	}
}

func TestSelectProvider(t *testing.T) {
	snap := usage.Snapshot{FiveHour: bucket(6, time.Hour), SevenDay: bucket(29, 0)}
	providers := []provider.Usage{codexUsage(12)}

	cases := []struct {
		name string
		want string
		out  string
	}{
		{name: "claude only", want: "claude", out: "5h 6% · 7d 29%"},
		{name: "codex only", want: "codex", out: "5h 12%"},
		// The provider name is matched case-insensitively: the registry reports
		// "Codex" but a config file or tmux option will say "codex".
		{name: "codex capitalised", want: "Codex", out: "5h 12%"},
		{name: "all", want: "all", out: "5h 6% · 7d 29% │ 5h 12%"},
		{name: "empty behaves like all", want: "", out: "5h 6% · 7d 29% │ 5h 12%"},
		// A name with no credentials renders nothing rather than an error: the
		// same outcome as a provider you do not use.
		{name: "unknown renders empty", want: "gemini", out: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Render(snap, providers, Options{Provider: tc.want, Now: fixedNow})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.out {
				t.Errorf("got %q want %q", got, tc.out)
			}
		})
	}
}

func TestSelectWithLabels(t *testing.T) {
	snap := usage.Snapshot{FiveHour: bucket(6, time.Hour)}
	got, err := Render(snap, []provider.Usage{codexUsage(12)},
		Options{Provider: "all", ShowLabels: true, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	want := "claude 5h 6% │ codex 5h 12%"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSelectSkipsProvidersWithNoWindows(t *testing.T) {
	// A provider that answered but reported nothing must not leave a stray
	// separator in the bar.
	snap := usage.Snapshot{FiveHour: bucket(6, time.Hour)}
	empty := provider.Usage{Provider: "Copilot"}
	got, err := Render(snap, []provider.Usage{empty}, Options{Provider: "all", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if got != "5h 6%" {
		t.Errorf("got %q want %q", got, "5h 6%")
	}
}

func TestSelectClaudeAbsentLeavesOnlyProviders(t *testing.T) {
	// No Anthropic credentials, but Codex is logged in: the bar shows Codex
	// rather than nothing.
	got, err := Render(usage.Snapshot{}, []provider.Usage{codexUsage(12)},
		Options{Provider: "all", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if got != "5h 12%" {
		t.Errorf("got %q want %q", got, "5h 12%")
	}
}

func TestPlainIncludesProviderNote(t *testing.T) {
	got, err := Render(usage.Snapshot{}, []provider.Usage{codexUsage(12)},
		Options{Format: FormatPlain, Provider: "codex", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "plan: plus") {
		t.Errorf("plain output dropped the provider note:\n%s", got)
	}
}

func TestClaudeNoteCarriesCredits(t *testing.T) {
	// The pay-as-you-go balance is a currency amount, so it belongs in the note
	// rather than among the percentage windows.
	snap := usage.Snapshot{
		FiveHour: bucket(6, time.Hour),
		ExtraUsage: &usage.ExtraUsage{
			IsEnabled: true, Utilization: ptr(25.0),
			UsedCredits: ptr(12.5), MonthlyLimit: ptr(50.0),
		},
	}
	got, err := Render(snap, nil, Options{Format: FormatPlain, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "$12.50 of $50.00") {
		t.Errorf("plain output dropped the credit balance:\n%s", got)
	}
}
