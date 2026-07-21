package pricing

import (
	"reflect"
	"testing"
)

func TestNormalizeModelID(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{"plain id", "gemini-3-pro-preview", "gemini-3-pro-preview"},
		{"bracket suffix", "claude-fable-5[1m]", "claude-fable-5"},
		{"provider prefix", "openai/gpt-5", "gpt-5"},
		{"vendor decoration", "google/antigravity-gemini-3-pro", "gemini-3-pro"},
		{"tag suffix", "ollama/kimi-k2.5:cloud", "kimi-k2.5"},
		{"free suffix", "opencode/minimax-m2.5-free", "minimax-m2.5"},
		{"free tag", "openrouter/minimax-m2.5:free", "minimax-m2.5"},
		{"claude dots", "anthropic/claude-opus-4.6", "claude-opus-4-6"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeModelID(tt.model); got != tt.want {
				t.Errorf("NormalizeModelID(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestModelIDCandidatesStartsWithTheRawIDAndIsDeduped(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		contains []string
	}{
		{"decorated", "google/antigravity-gemini-3-pro", []string{"google/antigravity-gemini-3-pro", "antigravity-gemini-3-pro", "gemini-3-pro"}},
		{"tagged", "ollama/kimi-k2.5:cloud", []string{"ollama/kimi-k2.5:cloud", "kimi-k2.5:cloud", "kimi-k2.5"}},
		{"plain", "gpt-5", []string{"gpt-5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ModelIDCandidates(tt.model)
			if len(got) == 0 || got[0] != tt.model {
				t.Fatalf("candidates(%q) = %v, want first element %q", tt.model, got, tt.model)
			}
			seen := map[string]bool{}
			for _, c := range got {
				if seen[c] {
					t.Errorf("duplicate candidate %q in %v", c, got)
				}
				seen[c] = true
			}
			for _, want := range tt.contains {
				if !seen[want] {
					t.Errorf("candidates(%q) = %v, missing %q", tt.model, got, want)
				}
			}
		})
	}
}

func TestModelIDCandidatesOnPlainIDIsExactlyItself(t *testing.T) {
	if got := ModelIDCandidates("gpt-5"); !reflect.DeepEqual(got, []string{"gpt-5"}) {
		t.Errorf("candidates = %v, want [gpt-5]", got)
	}
}

// Real-world ids taken from the maintainer's opencode rows that ingested with
// cost_eur = NULL before the normalizer landed.
func TestRealWorldOpencodeModelIDsResolveToAPrice(t *testing.T) {
	tbl, err := parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)
	c.OnWarn = func(m string) { t.Errorf("unexpected warning for %q", m) }

	tests := []struct {
		name  string
		model string
		free  bool
	}{
		{"antigravity gemini", "google/antigravity-gemini-3-pro", false},
		{"ollama tagged kimi", "ollama/kimi-k2.5:cloud", false},
		{"opencode free minimax", "opencode/minimax-m2.5-free", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.CostFor(tt.model, 1000, 1000, 1000, 1000)
			if got == nil {
				t.Fatalf("CostFor(%q) = nil, want a price", tt.model)
			}
			if tt.free && *got != 0 {
				t.Errorf("CostFor(%q) = %v, want 0 for an explicitly free variant", tt.model, *got)
			}
			if !tt.free && *got <= 0 {
				t.Errorf("CostFor(%q) = %v, want > 0", tt.model, *got)
			}
		})
	}
}

// An all-zero LiteLLM entry means "price unknown", not "free": it must stay
// unpriced so the row keeps cost_eur = NULL instead of silently reading €0.
func TestAllZeroLitellmEntriesStayUnpriced(t *testing.T) {
	tbl, err := parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)
	c.OnWarn = func(string) {}

	if got := c.CostFor("mistral/codestral-latest", 1000, 1000, 0, 0); got != nil {
		t.Errorf("CostFor(codestral-latest) = %v, want nil (unknown price)", *got)
	}
}
