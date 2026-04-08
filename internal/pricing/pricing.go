// Package pricing loads an editable TOML price table and computes the cost
// of an event using the four token classes Anthropic charges separately.
package pricing

import (
	_ "embed"
	"fmt"
	"os"
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
// to `path` (mode 0644) and loads it.
func LoadOrSeed(path string) (*Table, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, SeedTOML, 0o644); err != nil {
			return nil, err
		}
	}
	return Load(path)
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
