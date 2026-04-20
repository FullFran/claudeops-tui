// Package pricing loads an editable TOML price table and computes the cost
// of an event using the four token classes Anthropic charges separately.
package pricing

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/BurntSushi/toml"
)

//go:embed pricing.seed.toml
var SeedTOML []byte

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
	if changed {
		if err := os.WriteFile(path, encodeTable(merged), 0o644); err != nil {
			return nil, err
		}
	}
	return merged, nil
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
func (c *Calculator) CostFor(model string, in, out, cacheRead, cacheCreate int64) *float64 {
	mp, ok := c.t.Models[model]
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

// Updated returns the table's updated date for the dashboard footer.
func (c *Calculator) Updated() string { return c.t.Updated }

func perMillion(tokens int64, pricePerMillion float64) float64 {
	return float64(tokens) * pricePerMillion / 1_000_000.0
}
