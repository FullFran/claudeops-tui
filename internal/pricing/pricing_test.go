package pricing

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

const sample = `
updated = "2026-04-08"
currency = "EUR"

[models."claude-opus-4-6"]
input        = 13.80
output       = 69.00
cache_read   =  1.38
cache_create = 17.25
`

func TestParseAndCalculate(t *testing.T) {
	tbl, err := parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if tbl.Updated != "2026-04-08" {
		t.Errorf("updated: %q", tbl.Updated)
	}
	c := NewCalculator(tbl)

	// Real numbers from one of the assistant events seen earlier:
	// in=5, cache_read=15718, cache_create=20780, out=1101, model=opus-4-6
	got := c.CostFor("claude-opus-4-6", 5, 1101, 15718, 20780)
	if got == nil {
		t.Fatal("nil cost for known model")
	}
	want := 5*13.80/1e6 + 1101*69.00/1e6 + 15718*1.38/1e6 + 20780*17.25/1e6
	if math.Abs(*got-want) > 1e-9 {
		t.Errorf("cost: got %v want %v", *got, want)
	}
}

func TestUnknownModelWarnsOnce(t *testing.T) {
	tbl, _ := parse([]byte(sample))
	c := NewCalculator(tbl)
	calls := 0
	c.OnWarn = func(string) { calls++ }

	if got := c.CostFor("nope", 1, 1, 1, 1); got != nil {
		t.Errorf("expected nil for unknown model, got %v", *got)
	}
	if got := c.CostFor("nope", 1, 1, 1, 1); got != nil {
		t.Errorf("still nil")
	}
	if calls != 1 {
		t.Errorf("warn called %d times, want 1", calls)
	}
}

func TestLoadOrSeed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pricing.toml")
	tbl, err := LoadOrSeed(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tbl.Models["claude-opus-4-6"]; !ok {
		t.Errorf("seed missing opus model")
	}
	// File now exists.
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	// Second load also works.
	if _, err := LoadOrSeed(p); err != nil {
		t.Fatal(err)
	}
}
