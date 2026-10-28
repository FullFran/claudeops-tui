// Package pricing loads an editable TOML price table and computes the cost
// of an event using the four token classes Anthropic charges separately.
package pricing

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

//go:embed pricing.seed.toml
var SeedTOML []byte

// EURPerUSD is the conversion factor every price in this package is expressed
// in. It must stay in sync with EUR_FACTOR in scripts/update-pricing.sh, which
// applies it when regenerating the embedded LiteLLM snapshot.
//
// It is exported because a source can report the amount it was actually billed
// (opencode records a per-message `cost` in USD), and that figure has to land
// in the same currency as the table-derived estimates it replaces.
const EURPerUSD = 0.92

// EURFromUSD converts a provider-reported USD amount into the EUR this package
// prices in.
func EURFromUSD(usd float64) float64 { return usd * EURPerUSD }

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

// litellmFallback lazily parses the embedded LiteLLM snapshot. An entry whose
// four classes are all zero (e.g. "codestral-latest") means LiteLLM publishes
// no price for it, not that the model is free, so it is skipped and the event
// keeps cost_eur = NULL with the usual unknown-model warning. Genuinely free
// tiers are recognized by their id instead — see IsFreeModelID.
func litellmFallback() map[string]ModelPrice {
	litellmOnce.Do(func() {
		litellmTable = map[string]ModelPrice{}
		var raw map[string][4]float64
		if err := json.Unmarshal(litellmPricesJSON, &raw); err != nil {
			return
		}
		for name, v := range raw {
			if v[0] == 0 && v[1] == 0 && v[2] == 0 && v[3] == 0 {
				continue
			}
			litellmTable[name] = ModelPrice{Input: v[0], Output: v[1], CacheRead: v[2], CacheCreate: v[3]}
		}
	})
	return litellmTable
}

// ModelPrice is the EUR cost per 1,000,000 tokens for each class.
// CacheCreate is the 5-minute-TTL cache-write rate; CacheCreate1h is the
// 1-hour-TTL one and is optional — when absent it derives from Input (see
// cacheCreate1hRate).
// Tiers and Schedule are optional extensions for context-dependent and
// date-scheduled pricing.
type ModelPrice struct {
	Input         float64         `toml:"input"`
	Output        float64         `toml:"output"`
	CacheRead     float64         `toml:"cache_read"`
	CacheCreate   float64         `toml:"cache_create"`
	CacheCreate1h float64         `toml:"cache_create_1h,omitempty"`
	Tiers         []PriceTier     `toml:"tiers,omitempty"`
	Schedule      []PriceSchedule `toml:"schedule,omitempty"`
}

// PriceTier defines an optional pricing tier when prompt context exceeds
// Threshold tokens.
type PriceTier struct {
	Threshold     int64   `toml:"threshold"`
	EffectiveDate string  `toml:"effective_date,omitempty"`
	Input         float64 `toml:"input"`
	Output        float64 `toml:"output"`
	CacheRead     float64 `toml:"cache_read"`
	CacheCreate   float64 `toml:"cache_create"`
	CacheCreate1h float64 `toml:"cache_create_1h,omitempty"`
}

// PriceSchedule defines an optional scheduled rate effective at and after
// a UTC date/timestamp.
type PriceSchedule struct {
	EffectiveDate string  `toml:"effective_date"`
	Input         float64 `toml:"input"`
	Output        float64 `toml:"output"`
	CacheRead     float64 `toml:"cache_read"`
	CacheCreate   float64 `toml:"cache_create"`
	CacheCreate1h float64 `toml:"cache_create_1h,omitempty"`
}

// Equal reports whether two ModelPrices have identical rates, tiers, and schedules.
func (mp ModelPrice) Equal(other ModelPrice) bool {
	if mp.Input != other.Input || mp.Output != other.Output ||
		mp.CacheRead != other.CacheRead || mp.CacheCreate != other.CacheCreate ||
		mp.CacheCreate1h != other.CacheCreate1h {
		return false
	}
	if len(mp.Tiers) != len(other.Tiers) || len(mp.Schedule) != len(other.Schedule) {
		return false
	}
	for i := range mp.Tiers {
		if mp.Tiers[i] != other.Tiers[i] {
			return false
		}
	}
	for i := range mp.Schedule {
		if mp.Schedule[i] != other.Schedule[i] {
			return false
		}
	}
	return true
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
	enriched := enrichUntouchedSeedModels(merged, seed)
	corrected := correctStalePrices(merged, seed)
	if (enriched || corrected) && seed.Updated != "" {
		merged.Updated = seed.Updated
	}
	if changed || enriched || corrected {
		if err := merged.Validate(); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, encodeTable(merged), 0o644); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// exactOldSeedModels lists model prices from previous seeds that were flat.
// We enrich them with tiers and schedules only if the local file has the EXACT
// old seed values and no custom tiers/schedules.
var exactOldSeedModels = map[string]ModelPrice{
	"gpt-6-astra":      {Input: 9.20, Output: 46.00, CacheRead: 0.92, CacheCreate: 11.50},
	"gpt-6-sol":        {Input: 1.84, Output: 9.20, CacheRead: 0.184, CacheCreate: 2.30},
	"gpt-6-luna":       {Input: 0.092, Output: 0.46, CacheRead: 0.0092, CacheCreate: 0.115},
	"gemini-3.8-flash": {Input: 0.69, Output: 3.45, CacheRead: 0.069, CacheCreate: 0.00},
}

// enrichUntouchedSeedModels adds new tiers/schedules to models that still match
// the exact old seed, preserving all user-customized fields.
func enrichUntouchedSeedModels(current, seed *Table) bool {
	changed := false
	for name, oldPrice := range exactOldSeedModels {
		cur, ok := current.Models[name]
		if !ok {
			continue
		}
		if cur.Input != oldPrice.Input ||
			cur.Output != oldPrice.Output ||
			cur.CacheRead != oldPrice.CacheRead ||
			cur.CacheCreate != oldPrice.CacheCreate ||
			cur.CacheCreate1h != oldPrice.CacheCreate1h ||
			len(cur.Tiers) > 0 ||
			len(cur.Schedule) > 0 {
			continue
		}
		seedPrice, inSeed := seed.Models[name]
		if !inSeed {
			continue
		}
		if len(seedPrice.Tiers) > 0 || len(seedPrice.Schedule) > 0 {
			cur.Tiers = seedPrice.Tiers
			cur.Schedule = seedPrice.Schedule
			current.Models[name] = cur
			changed = true
		}
	}
	return changed
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
		if !ok || !cur.Equal(wrong) {
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

// Validate checks that all model prices, tiers, and schedules have valid non-negative
// rates, positive thresholds, and valid date formats.
func (t *Table) Validate() error {
	for name, mp := range t.Models {
		if err := validatePrice(name, mp); err != nil {
			return err
		}
	}
	return nil
}

func isNonFinite(f float64) bool {
	return math.IsNaN(f) || math.IsInf(f, 0)
}

func validatePrice(name string, mp ModelPrice) error {
	if mp.Input < 0 || mp.Output < 0 || mp.CacheRead < 0 || mp.CacheCreate < 0 || mp.CacheCreate1h < 0 {
		return fmt.Errorf("pricing: model %q has negative rate", name)
	}
	for i, tier := range mp.Tiers {
		if tier.Threshold <= 0 {
			return fmt.Errorf("pricing: model %q tier[%d] has non-positive threshold %d", name, i, tier.Threshold)
		}
		if isNonFinite(tier.Input) || isNonFinite(tier.Output) || isNonFinite(tier.CacheRead) || isNonFinite(tier.CacheCreate) || isNonFinite(tier.CacheCreate1h) {
			return fmt.Errorf("pricing: model %q tier[%d] has non-finite rate", name, i)
		}
		if tier.Input < 0 || tier.Output < 0 || tier.CacheRead < 0 || tier.CacheCreate < 0 || tier.CacheCreate1h < 0 {
			return fmt.Errorf("pricing: model %q tier[%d] has negative rate", name, i)
		}
		if tier.EffectiveDate != "" {
			if _, err := parseCutoffDate(tier.EffectiveDate); err != nil {
				return fmt.Errorf("pricing: model %q tier[%d] has invalid effective_date %q: %w", name, i, tier.EffectiveDate, err)
			}
		}
	}
	for i, s := range mp.Schedule {
		if s.EffectiveDate == "" {
			return fmt.Errorf("pricing: model %q schedule[%d] missing effective_date", name, i)
		}
		if _, err := parseCutoffDate(s.EffectiveDate); err != nil {
			return fmt.Errorf("pricing: model %q schedule[%d] has invalid effective_date %q: %w", name, i, s.EffectiveDate, err)
		}
		if isNonFinite(s.Input) || isNonFinite(s.Output) || isNonFinite(s.CacheRead) || isNonFinite(s.CacheCreate) || isNonFinite(s.CacheCreate1h) {
			return fmt.Errorf("pricing: model %q schedule[%d] has non-finite rate", name, i)
		}
		if s.Input < 0 || s.Output < 0 || s.CacheRead < 0 || s.CacheCreate < 0 || s.CacheCreate1h < 0 {
			return fmt.Errorf("pricing: model %q schedule[%d] has negative rate", name, i)
		}
	}
	return nil
}

func parseCutoffDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q (expected YYYY-MM-DD or RFC3339)", s)
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
	if err := t.Validate(); err != nil {
		return nil, err
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

func formatRate(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
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
		fmt.Fprintf(&buf, "input         = %s\n", formatRate(price.Input))
		fmt.Fprintf(&buf, "output        = %s\n", formatRate(price.Output))
		fmt.Fprintf(&buf, "cache_read    = %s\n", formatRate(price.CacheRead))
		fmt.Fprintf(&buf, "cache_create  = %s\n", formatRate(price.CacheCreate))
		if price.CacheCreate1h > 0 {
			fmt.Fprintf(&buf, "cache_create_1h = %s\n", formatRate(price.CacheCreate1h))
		}
		for _, tier := range price.Tiers {
			buf.WriteString("\n")
			fmt.Fprintf(&buf, "[[models.%q.tiers]]\n", name)
			fmt.Fprintf(&buf, "threshold     = %d\n", tier.Threshold)
			if tier.EffectiveDate != "" {
				fmt.Fprintf(&buf, "effective_date = %q\n", tier.EffectiveDate)
			}
			fmt.Fprintf(&buf, "input         = %s\n", formatRate(tier.Input))
			fmt.Fprintf(&buf, "output        = %s\n", formatRate(tier.Output))
			fmt.Fprintf(&buf, "cache_read    = %s\n", formatRate(tier.CacheRead))
			fmt.Fprintf(&buf, "cache_create  = %s\n", formatRate(tier.CacheCreate))
			if tier.CacheCreate1h > 0 {
				fmt.Fprintf(&buf, "cache_create_1h = %s\n", formatRate(tier.CacheCreate1h))
			}
		}
		for _, sched := range price.Schedule {
			buf.WriteString("\n")
			fmt.Fprintf(&buf, "[[models.%q.schedule]]\n", name)
			fmt.Fprintf(&buf, "effective_date = %q\n", sched.EffectiveDate)
			fmt.Fprintf(&buf, "input         = %s\n", formatRate(sched.Input))
			fmt.Fprintf(&buf, "output        = %s\n", formatRate(sched.Output))
			fmt.Fprintf(&buf, "cache_read    = %s\n", formatRate(sched.CacheRead))
			fmt.Fprintf(&buf, "cache_create  = %s\n", formatRate(sched.CacheCreate))
			if sched.CacheCreate1h > 0 {
				fmt.Fprintf(&buf, "cache_create_1h = %s\n", formatRate(sched.CacheCreate1h))
			}
		}
		if i < len(models)-1 {
			buf.WriteString("\n")
		}
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

// nonBillableModels are pseudo-model ids that carry no API usage — Claude Code
// reports "<synthetic>" for locally generated messages. They cost nothing and
// have no price entry, so they stay NULL without a missing-model warning.
var nonBillableModels = map[string]bool{"<synthetic>": true}

// Calculator is a thin wrapper that warns once per unknown model and
// computes a per-event cost from a Table.
// The table may be swapped at runtime via Reload, so every read of `t` goes
// through mu.
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
// Decorated model IDs — "claude-fable-5[1m]", "openai/gpt-5",
// "google/antigravity-gemini-3-pro", "ollama/kimi-k2.5:cloud" — fall back to
// the progressively less decorated forms ModelIDCandidates derives, so the
// table can be keyed by bare model name regardless of which source emitted the
// event. Explicitly free variants ("…-free", "…:free") cost €0 unless the
// table carries an exact entry for them.
func (c *Calculator) CostFor(model string, in, out, cacheRead, cacheCreate int64) *float64 {
	return c.CostForCacheTTL(model, in, out, cacheRead, cacheCreate, 0)
}

// CostForCacheTTL is CostFor with the 1-hour-TTL portion of the cache writes
// split out. cacheCreate is the total number of cache-write tokens and
// cacheCreate1h the part of it written with a 1h TTL, which Anthropic bills at
// roughly 1.6x the 5m rate. When cacheCreate1h exceeds cacheCreate it is capped
// at the total, so a malformed line can never bill more tokens than it reported.
func (c *Calculator) CostForCacheTTL(model string, in, out, cacheRead, cacheCreate, cacheCreate1h int64) *float64 {
	return c.CostForEvent(model, in, out, cacheRead, cacheCreate, cacheCreate1h, time.Time{})
}

// CostForEvent computes the EUR cost for an event with the given token classes
// and event timestamp.
// If ts is zero, deterministic base rates apply without date schedules.
// Otherwise UTC date schedules and prompt context tiers are evaluated.
func (c *Calculator) CostForEvent(model string, in, out, cacheRead, cacheCreate, cacheCreate1h int64, ts time.Time) *float64 {
	if nonBillableModels[model] {
		return nil
	}
	models := c.table().Models
	if _, exact := models[model]; !exact && IsFreeModelID(model) {
		zero := 0.0
		return &zero
	}
	mp, ok := lookupPrice(models, model)
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

	if cacheCreate1h > cacheCreate {
		cacheCreate1h = cacheCreate
	}

	effectiveMP := mp

	// 1. Evaluate scheduled rates when timestamp is non-zero
	if !ts.IsZero() && len(mp.Schedule) > 0 {
		var latestSched *PriceSchedule
		var latestCutoff time.Time
		utcTS := ts.UTC()
		for _, s := range mp.Schedule {
			cutoff, err := parseCutoffDate(s.EffectiveDate)
			if err != nil {
				continue
			}
			if !utcTS.Before(cutoff) { // utcTS >= cutoff
				if latestSched == nil || cutoff.After(latestCutoff) {
					sCopy := s
					latestSched = &sCopy
					latestCutoff = cutoff
				}
			}
		}
		if latestSched != nil {
			effectiveMP.Input = latestSched.Input
			effectiveMP.Output = latestSched.Output
			effectiveMP.CacheRead = latestSched.CacheRead
			effectiveMP.CacheCreate = latestSched.CacheCreate
			effectiveMP.CacheCreate1h = latestSched.CacheCreate1h
		}
	}

	// 2. Evaluate context tiers (prompt context threshold)
	if len(mp.Tiers) > 0 {
		isHaiku := strings.Contains(NormalizeModelID(model), "haiku") || strings.Contains(strings.ToLower(model), "haiku")
		promptTokens := in + cacheRead
		if isHaiku {
			promptTokens += cacheCreate
		}

		var activeTier *PriceTier
		var activeCutoff time.Time
		for _, tier := range mp.Tiers {
			var cutoff time.Time
			if tier.EffectiveDate != "" {
				if ts.IsZero() {
					continue
				}
				var err error
				cutoff, err = parseCutoffDate(tier.EffectiveDate)
				if err != nil || ts.UTC().Before(cutoff) {
					continue
				}
			}
			if promptTokens > tier.Threshold {
				if activeTier == nil ||
					tier.Threshold > activeTier.Threshold ||
					(tier.Threshold == activeTier.Threshold && cutoff.After(activeCutoff)) {
					tCopy := tier
					activeTier = &tCopy
					activeCutoff = cutoff
				}
			}
		}
		if activeTier != nil {
			effectiveMP.Input = activeTier.Input
			effectiveMP.Output = activeTier.Output
			effectiveMP.CacheRead = activeTier.CacheRead
			effectiveMP.CacheCreate = activeTier.CacheCreate
			effectiveMP.CacheCreate1h = activeTier.CacheCreate1h
		}
	}

	cacheCreate5m := cacheCreate - cacheCreate1h
	cost := perMillion(in, effectiveMP.Input) +
		perMillion(out, effectiveMP.Output) +
		perMillion(cacheRead, effectiveMP.CacheRead) +
		perMillion(cacheCreate5m, effectiveMP.CacheCreate) +
		perMillion(cacheCreate1h, cacheCreate1hRate(effectiveMP))
	return &cost
}

// cacheCreate1hRate returns the 1-hour-TTL cache-write rate. Anthropic prices
// it at input x 2 (against input x 1.25 for 5m), so when the table carries no
// explicit cache_create_1h we derive it from input, falling back to 1.6x the
// 5m rate for tables that only price the cache classes.
func cacheCreate1hRate(mp ModelPrice) float64 {
	switch {
	case mp.CacheCreate1h > 0:
		return mp.CacheCreate1h
	case mp.Input > 0:
		return mp.Input * 2
	default:
		return mp.CacheCreate * 1.6
	}
}

// lookupPrice finds a model's price in `table`, trying every form
// ModelIDCandidates derives from the id, from the exact id down to the fully
// normalized one.
func lookupPrice(table map[string]ModelPrice, model string) (ModelPrice, bool) {
	for _, k := range ModelIDCandidates(model) {
		if mp, ok := table[k]; ok {
			return mp, true
		}
	}
	return ModelPrice{}, false
}

// Reload swaps in a freshly parsed table so price edits apply without a
// restart. The missing-model memo is cleared, so a model that is still unpriced
// warns once more against the new table.
func (c *Calculator) Reload(t *Table) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
	c.missing = map[string]bool{}
}

// ReloadFrom loads `path` and swaps it in. The current table is kept on error.
func (c *Calculator) ReloadFrom(path string) error {
	t, err := Load(path)
	if err != nil {
		return err
	}
	c.Reload(t)
	return nil
}

func (c *Calculator) table() *Table {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Updated returns the table's updated date for the dashboard footer.
func (c *Calculator) Updated() string { return c.table().Updated }

func perMillion(tokens int64, pricePerMillion float64) float64 {
	return float64(tokens) * pricePerMillion / 1_000_000.0
}
