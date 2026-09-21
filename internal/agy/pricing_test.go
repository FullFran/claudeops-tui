package agy

import (
	"testing"

	"github.com/fullfran/claudeops-tui/internal/pricing"
)

// TestNormalizeModelResolvesToKnownPrice proves NormalizeModel's output
// actually prices, not just that it matches an expected string. An empty
// custom table forces the Calculator onto the embedded LiteLLM snapshot, so
// this only passes if the normalized id is a real key in that catalog.
func TestNormalizeModelResolvesToKnownPrice(t *testing.T) {
	calc := pricing.NewCalculator(&pricing.Table{Models: map[string]pricing.ModelPrice{}})

	tests := []struct {
		name string
		raw  string
	}{
		{"exp variant", "gemini-3.8-flash-exp-a"},
		{"reasoning effort variant", "gemini-3.1-pro-high"},
		{"already canonical", "claude-sonnet-4-6"},
		{"reasoning effort on a different model", "gpt-oss-120b-medium"},
		{"agy 1.2.7 Gemini Pro alias", "gemini-pro-default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NormalizeModel(tt.raw)
			cost := calc.CostFor(model, 1000, 500, 0, 0)
			if cost == nil {
				t.Fatalf("CostFor(NormalizeModel(%q)=%q) = nil, want a resolved price", tt.raw, model)
			}
		})
	}
}
