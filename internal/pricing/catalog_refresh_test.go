package pricing

import (
	"math"
	"testing"
)

// Rates verified against LiteLLM model_prices_and_context_window.json, in USD per MTok.
func TestCatalogRefreshNewModels(t *testing.T) {
	for _, tt := range []struct {
		model string
		usd   [4]float64
	}{
		{"gpt-6-astra", [4]float64{10, 50, 1, 12.5}},
		{"openai/gpt-6-astra", [4]float64{10, 50, 1, 12.5}},
		{"claude-fable-5-1", [4]float64{10, 50, 0.25, 12.5}},
		{"gemini-3.8-flash", [4]float64{0.75, 3.75, 0.075, 0}},
		{"claude-opus-5-5", [4]float64{4, 20, 0.20, 5}},
		{"claude-opus-5", [4]float64{5, 25, 0.50, 6.25}},
		{"gpt-6-sol", [4]float64{2, 10, 0.20, 2.5}},
		{"gpt-6-luna", [4]float64{0.10, 0.50, 0.01, 0.125}},
		{"gemini-3.1-pro", [4]float64{2, 12, 0, 0}},
		{"gpt-oss-120b", [4]float64{0.15, 0.60, 0, 0}},
	} {
		t.Run(tt.model, func(t *testing.T) {
			calc := NewCalculator(&Table{Models: map[string]ModelPrice{}})
			for class, usd := range tt.usd {
				tokens := [4]int64{}
				tokens[class] = 1_000_000
				got := calc.CostFor(tt.model, tokens[0], tokens[1], tokens[2], tokens[3])
				if got == nil {
					t.Fatal("verified model has no price")
				}
				if want := usd * EURPerUSD; math.Abs(*got-want) > 1e-9 {
					t.Errorf("token class %d: got %v, want %v", class, *got, want)
				}
			}
		})
	}
}

func TestCatalogRefreshOverrideAndUnknown(t *testing.T) {
	calc := NewCalculator(&Table{Models: map[string]ModelPrice{
		"gpt-6-astra": {Input: 123},
	}})
	if got := calc.CostFor("openai/gpt-6-astra", 1_000_000, 0, 0, 0); got == nil || *got != 123 {
		t.Fatalf("user override must win, got %v", got)
	}
	// No exact gpt-6 entry exists in the verified source; do not invent an alias.
	if got := calc.CostFor("gpt-6", 1_000_000, 0, 0, 0); got != nil {
		t.Fatalf("unverified model must remain unpriced, got %v", *got)
	}
}
