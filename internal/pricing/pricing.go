// Package pricing loads an editable TOML price table and computes the cost
// of an event using the four token classes Anthropic charges separately.
package pricing

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

//go:embed pricing.seed.toml
var SeedTOML []byte

// litellmPricesJSON is a compact snapshot of the LiteLLM pricing dataset
// (github.com/BerriAI/litellm), keyed by bare model name → [input, output,
// cache_read, cache_create] in EUR per 1M tokens (USD × 0.92). It provides
// comprehensive multi-provider coverage as a fallback beneath the editable
// TOML table, the way CodexBar prices from LiteLLM.
//
//go:embed litellm_prices.json
var litellmPricesJSON []byte

var (
	litellmOnce  sync.Once
	litellmTable map[string]ModelPrice
)

// litellmFallback lazily parses the embedded LiteLLM snapshot. Entries with no
// real input price are skipped so genuinely-unpriced models still warn.
func litellmFallback() map[string]ModelPrice {
	litellmOnce.Do(func() {
		litellmTable = map[string]ModelPrice{}
		var raw map[string][4]float64
		if err := json.Unmarshal(litellmPricesJSON, &raw); err != nil {
			return
		}
		for name, v := range raw {
			if v[0] == 0 && v[1] == 0 {
				continue
			}
			litellmTable[name] = ModelPrice{Input: v[0], Output: v[1], CacheRead: v[2], CacheCreate: v[3]}
		}
	})
	return litellmTable
}

// ModelPrice is the EUR cost per 1,000,000 tokens for each class.
type ModelPrice struct {
	Input       float64 `toml:"input"`
	Output      float64 `toml:"output"`
	CacheRead   float64 `toml:"cache_read"`
	CacheCreate float64 `toml:"cache_create"`
}

// Table is the parsed pricing.toml.
type Table struct {
	Updated  string                `toml:"updated"`
	Currency string                `toml:"currency"`
	Models   map[string]ModelPrice `toml:"models"`
}

// Load reads and parses a pricing TOML file.
func Load(path string) (*Table, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(b)
}

// LoadOrSeed loads `path`; if it does not exist, writes the embedded seed
// to `path` (mode 0644) and loads it. If the file already exists, any missing
// seed models are merged in without overwriting existing user-customized values.
func LoadOrSeed(path string) (*Table, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, SeedTOML, 0o644); err != nil {
			return nil, err
		}
	}

	current, err := Load(path)
	if err != nil {
		return nil, err
	}
	seed, err := parse(SeedTOML)
	if err != nil {
		return nil, err
	}
	merged, changed := mergeMissingModels(current, seed)
	corrected := correctStalePrices(merged, seed)
	if corrected && seed.Updated != "" {
		merged.Updated = seed.Updated
	}
	if changed || corrected {
		if err := os.WriteFile(path, encodeTable(merged), 0o644); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// staleSeedPrices lists per-model values we shipped incorrectly in earlier
// seeds (the pre-4.5 Opus tier at $15/$75, and Haiku 4.5 priced as Haiku 3.5).
// On load we replace a user's value ONLY if it still exactly matches the wrong
// value we shipped — i.e. the user never customized it. Any other value is
// treated as a deliberate customization and left untouched.
var staleSeedPrices = map[string]ModelPrice{
	"claude-opus-4-7":           {Input: 13.80, Output: 69.00, CacheRead: 1.38, CacheCreate: 17.25},
	"claude-opus-4-6":           {Input: 13.80, Output: 69.00, CacheRead: 1.38, CacheCreate: 17.25},
	"claude-opus-4":             {Input: 13.80, Output: 69.00, CacheRead: 1.38, CacheCreate: 17.25},
	"claude-haiku-4-5-20251001": {Input: 0.736, Output: 3.68, CacheRead: 0.0736, CacheCreate: 0.92},
	"claude-haiku-4-5":          {Input: 0.736, Output: 3.68, CacheRead: 0.0736, CacheCreate: 0.92},
}

// correctStalePrices overwrites any model whose local value still equals the
// known-wrong shipped value with the current seed price, so existing installs
// pick up corrections that mergeMissingModels (add-only) would otherwise miss.
// Returns true if any model was corrected.
func correctStalePrices(current, seed *Table) bool {
	changed := false
	for name, wrong := range staleSeedPrices {
		cur, ok := current.Models[name]
		if !ok || cur != wrong {
			continue
		}
		newv, ok := seed.Models[name]
		if !ok {
			continue
		}
		current.Models[name] = newv
		changed = true
	}
	return changed
}

func parse(b []byte) (*Table, error) {
	var t Table
	if err := toml.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	if t.Models == nil {
		t.Models = map[string]ModelPrice{}
	}
	if t.Currency == "" {
		t.Currency = "EUR"
	}
	return &t, nil
}

func mergeMissingModels(current, seed *Table) (*Table, bool) {
	merged := &Table{
		Updated:  current.Updated,
		Currency: current.Currency,
		Models:   make(map[string]ModelPrice, len(current.Models)+len(seed.Models)),
	}
	for name, price := range current.Models {
		merged.Models[name] = price
	}

	changed := false
	for name, price := range seed.Models {
		if _, ok := merged.Models[name]; ok {
			continue
		}
		merged.Models[name] = price
		changed = true
	}

	if merged.Currency == "" {
		merged.Currency = seed.Currency
		if merged.Currency == "" {
			merged.Currency = "EUR"
		}
	}
	if changed && seed.Updated != "" {
		merged.Updated = seed.Updated
	}
	return merged, changed
}

func encodeTable(t *Table) []byte {
	var buf bytes.Buffer
	buf.WriteString("# claudeops pricing table.\n")
	buf.WriteString("#\n")
	buf.WriteString("# Prices are in EUR per 1,000,000 tokens, split into the four token classes\n")
	buf.WriteString("# Anthropic charges separately. Edit this file as Anthropic updates pricing.\n")
	buf.WriteString("#\n")
	buf.WriteString("# Source: https://www.anthropic.com/pricing  (verify before trusting € numbers)\n")
	buf.WriteString("# Currency: EUR. Adjust if you want USD — the calculator does not convert.\n\n")
	fmt.Fprintf(&buf, "updated = %q\n", t.Updated)
	fmt.Fprintf(&buf, "currency = %q\n\n", t.Currency)

	models := make([]string, 0, len(t.Models))
	for name := range t.Models {
		models = append(models, name)
	}
	sort.Strings(models)

	for i, name := range models {
		price := t.Models[name]
		fmt.Fprintf(&buf, "[models.%q]\n", name)
		fmt.Fprintf(&buf, "input         = %5.4f\n", price.Input)
		fmt.Fprintf(&buf, "output        = %5.4f\n", price.Output)
		fmt.Fprintf(&buf, "cache_read    = %5.4f\n", price.CacheRead)
		fmt.Fprintf(&buf, "cache_create  = %5.4f\n", price.CacheCreate)
		if i < len(models)-1 {
			buf.WriteString("\n")
		}
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

// Calculator is a thin wrapper that warns once per unknown model and
// computes a per-event cost from a Table.
type Calculator struct {
	t       *Table
	mu      sync.Mutex
	missing map[string]bool
	OnWarn  func(model string) // optional, called once per missing model
}

// NewCalculator wraps a Table.
func NewCalculator(t *Table) *Calculator {
	return &Calculator{t: t, missing: map[string]bool{}}
}

// CostFor computes the EUR cost for an event with the given token classes.
// Returns nil if the model is unknown (and triggers OnWarn once).
//
// Model IDs with a bracket suffix — e.g. "claude-fable-5[1m]", the 1M-context
// variant Claude Code reports — fall back to the base ID's price when no
// explicit entry exists for the suffixed form.
//
// Provider-qualified IDs — e.g. opencode's "openai/gpt-5" or "google/gemini-2.5-pro"
// — fall back to the bare model name ("gpt-5", "gemini-2.5-pro") so entries can
// be keyed uniformly regardless of which source emitted them.
func (c *Calculator) CostFor(model string, in, out, cacheRead, cacheCreate int64) *float64 {
	mp, ok := lookupPrice(c.t.Models, model)
	if !ok {
		// Fall back to the embedded LiteLLM snapshot for models the editable
		// table doesn't carry (the user's table always wins when present).
		mp, ok = lookupPrice(litellmFallback(), model)
	}
	if !ok {
		c.mu.Lock()
		if !c.missing[model] {
			c.missing[model] = true
			if c.OnWarn != nil {
				c.OnWarn(model)
			} else {
				fmt.Fprintf(os.Stderr, "claudeops: pricing has no entry for model %q\n", model)
			}
		}
		c.mu.Unlock()
		return nil
	}
	cost := perMillion(in, mp.Input) +
		perMillion(out, mp.Output) +
		perMillion(cacheRead, mp.CacheRead) +
		perMillion(cacheCreate, mp.CacheCreate)
	return &cost
}

// lookupPrice finds a model's price in `table`, trying progressively looser
// forms of the id:
//   - exact match
//   - bracket suffix stripped ("claude-fable-5[1m]" → "claude-fable-5")
//   - provider prefix stripped ("openai/gpt-5" → "gpt-5")
//   - for Claude ids, dots normalized to dashes ("claude-opus-4.6" →
//     "claude-opus-4-6"), which is how some sources qualify Anthropic models.
func lookupPrice(table map[string]ModelPrice, model string) (ModelPrice, bool) {
	for _, k := range priceCandidates(model) {
		if mp, ok := table[k]; ok {
			return mp, true
		}
	}
	return ModelPrice{}, false
}

// priceCandidates returns the ordered lookup keys for a model id.
func priceCandidates(model string) []string {
	cands := []string{model}
	if base, ok := stripBracket(model); ok {
		cands = append(cands, base)
	}
	if i := strings.LastIndexByte(model, '/'); i >= 0 && i+1 < len(model) {
		bare := model[i+1:]
		cands = append(cands, bare)
		if base, ok := stripBracket(bare); ok {
			cands = append(cands, base)
		}
	}
	// Claude ids with dot-separated versions ("claude-opus-4.6").
	for _, c := range append([]string{}, cands...) {
		if strings.HasPrefix(c, "claude") && strings.Contains(c, ".") {
			cands = append(cands, strings.ReplaceAll(c, ".", "-"))
		}
	}
	return cands
}

func stripBracket(s string) (string, bool) {
	if i := strings.IndexByte(s, '['); i > 0 && strings.HasSuffix(s, "]") {
		return s[:i], true
	}
	return "", false
}

// Updated returns the table's updated date for the dashboard footer.
func (c *Calculator) Updated() string { return c.t.Updated }

func perMillion(tokens int64, pricePerMillion float64) float64 {
	return float64(tokens) * pricePerMillion / 1_000_000.0
}
