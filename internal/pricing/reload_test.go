package pricing

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReloadFromPicksUpEditedPrices(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pricing.toml")
	const before = `
updated = "2026-07-01"
currency = "EUR"

[models."m"]
input        = 10.0
output       = 0.0
cache_read   = 0.0
cache_create = 0.0
`
	const after = `
updated = "2026-07-21"
currency = "EUR"

[models."m"]
input        = 20.0
output       = 0.0
cache_read   = 0.0
cache_create = 0.0
`
	if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)

	if got := c.CostFor("m", 1_000_000, 0, 0, 0); got == nil || *got != 10.0 {
		t.Fatalf("cost before reload = %v, want 10", got)
	}
	if err := os.WriteFile(p, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.ReloadFrom(p); err != nil {
		t.Fatal(err)
	}
	if got := c.CostFor("m", 1_000_000, 0, 0, 0); got == nil || *got != 20.0 {
		t.Fatalf("cost after reload = %v, want 20", got)
	}
	if c.Updated() != "2026-07-21" {
		t.Errorf("Updated() = %q, want 2026-07-21", c.Updated())
	}
}

func TestReloadClearsTheMissingModelMemo(t *testing.T) {
	tbl, err := parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)
	warns := 0
	c.OnWarn = func(string) { warns++ }

	c.CostFor("still-unknown", 1, 1, 1, 1)
	c.Reload(tbl)
	c.CostFor("still-unknown", 1, 1, 1, 1)
	if warns != 2 {
		t.Errorf("warns = %d, want 2 (memo cleared on reload)", warns)
	}
}

func TestReloadIsSafeWhileCosting(t *testing.T) {
	tbl, err := parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)
	c.OnWarn = func(string) {}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				c.CostFor("claude-opus-4-6", 1, 1, 1, 1)
				c.Updated()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			c.Reload(tbl)
		}
	}()
	wg.Wait()
}
