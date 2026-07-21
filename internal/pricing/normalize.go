package pricing

import "strings"

// Model ids reach us decorated in provider-specific ways: opencode qualifies
// them with the gateway ("openai/gpt-5"), Ollama appends a tag
// ("kimi-k2.5:cloud"), Antigravity prefixes the product onto the underlying
// model ("antigravity-gemini-3-pro"), gateways mark free tiers ("-free"), and
// Claude Code appends a context-window bracket ("claude-fable-5[1m]").
//
// NormalizeModelID strips all of those decorations down to the bare model id,
// and ModelIDCandidates returns every intermediate form so a price table keyed
// at any level still matches. Both are pure functions on the id alone — they
// never consult a price table — so other packages (the store's per-model
// aggregation) can reuse them for grouping.

// modelIDTransforms are the decoration strippers, applied in order. Each
// returns (result, true) only when it actually changed the id.
var modelIDTransforms = []func(string) (string, bool){
	stripBracketSuffix,
	stripProviderPrefix,
	stripTagSuffix,
	stripFreeSuffix,
	stripVendorDecoration,
	claudeDotsToDashes,
}

// NormalizeModelID reduces a decorated model id to its bare canonical form by
// applying every stripper until the id stops changing. It is idempotent:
// NormalizeModelID(NormalizeModelID(x)) == NormalizeModelID(x).
func NormalizeModelID(model string) string {
	cur := model
	for changed := true; changed; {
		changed = false
		for _, f := range modelIDTransforms {
			if v, ok := f(cur); ok {
				cur, changed = v, true
			}
		}
	}
	return cur
}

// ModelIDCandidates returns the ordered lookup keys for a model id: the id
// itself first, then progressively less decorated forms, ending with the fully
// normalized id. The result is deduplicated, so a plain id yields just itself.
func ModelIDCandidates(model string) []string {
	out := []string{model}
	seen := map[string]bool{model: true}
	for i := 0; i < len(out); i++ {
		for _, f := range modelIDTransforms {
			v, ok := f(out[i])
			if !ok || v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// IsFreeModelID reports whether the id names an explicitly free variant of a
// model ("minimax-m2.5-free", "…:free"). Those tiers bill nothing, so they are
// priced at €0 rather than at the paid model's rate — pricing them from the
// base model would invent spend that never happened, and leaving them NULL
// would hide them from cost aggregates entirely.
func IsFreeModelID(model string) bool {
	for _, c := range ModelIDCandidates(model) {
		if strings.HasSuffix(c, "-free") || strings.HasSuffix(c, ":free") {
			return true
		}
	}
	return false
}

// vendorDecorations are product prefixes some vendors bolt onto the underlying
// model id ("google/antigravity-gemini-3-pro" is Gemini 3 Pro billed as Gemini
// 3 Pro).
var vendorDecorations = []string{"antigravity-"}

func stripBracketSuffix(s string) (string, bool) {
	if i := strings.IndexByte(s, '['); i > 0 && strings.HasSuffix(s, "]") {
		return s[:i], true
	}
	return s, false
}

func stripProviderPrefix(s string) (string, bool) {
	if i := strings.LastIndexByte(s, '/'); i >= 0 && i+1 < len(s) {
		return s[i+1:], true
	}
	return s, false
}

func stripTagSuffix(s string) (string, bool) {
	if i := strings.LastIndexByte(s, ':'); i > 0 && i+1 < len(s) {
		return s[:i], true
	}
	return s, false
}

func stripFreeSuffix(s string) (string, bool) {
	if base, ok := strings.CutSuffix(s, "-free"); ok && base != "" {
		return base, true
	}
	return s, false
}

func stripVendorDecoration(s string) (string, bool) {
	for _, d := range vendorDecorations {
		if base, ok := strings.CutPrefix(s, d); ok && base != "" {
			return base, true
		}
	}
	return s, false
}

// claudeDotsToDashes rewrites dot-separated Claude versions the way our table
// keys them ("claude-opus-4.6" → "claude-opus-4-6").
func claudeDotsToDashes(s string) (string, bool) {
	if !strings.HasPrefix(s, "claude") || !strings.Contains(s, ".") {
		return s, false
	}
	return strings.ReplaceAll(s, ".", "-"), true
}
