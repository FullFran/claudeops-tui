package pricing

import (
	"math"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadOrSeedMergesMissingSeedModelsWithoutOverwritingExistingValues(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pricing.toml")

	const stale = `
updated = "2026-04-01"
currency = "EUR"

[models."claude-opus-4-6"]
input        = 99.0
output       = 88.0
cache_read   = 77.0
cache_create = 66.0
`
	if err := os.WriteFile(p, []byte(strings.TrimSpace(stale)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := LoadOrSeed(p)
	if err != nil {
		t.Fatal(err)
	}

	got := tbl.Models["claude-opus-4-6"]
	if got.Input != 99.0 || got.Output != 88.0 || got.CacheRead != 77.0 || got.CacheCreate != 66.0 {
		t.Fatalf("existing model overwritten: %+v", got)
	}
	if _, ok := tbl.Models["claude-opus-4-7"]; !ok {
		t.Fatal("missing merged seed model claude-opus-4-7")
	}
	if tbl.Updated != "2026-04-17" {
		t.Fatalf("updated = %q, want seed date after merge", tbl.Updated)
	}

	reloaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Models["claude-opus-4-7"]; !ok {
		t.Fatal("merged model not persisted to disk")
	}
	if reloaded.Models["claude-opus-4-6"].Input != 99.0 {
		t.Fatalf("persisted custom value lost: %+v", reloaded.Models["claude-opus-4-6"])
	}
}

func TestLoadOrSeedKeepsCurrentFileUntouchedWhenSeedModelsAlreadyExist(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pricing.toml")
	original := string(SeedTOML)
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrSeed(p); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatal("expected current pricing file to remain untouched when no merge is needed")
	}
}
