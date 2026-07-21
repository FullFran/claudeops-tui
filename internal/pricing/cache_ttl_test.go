package pricing

import (
	"math"
	"testing"
)

const cacheTTLSample = `
updated = "2026-07-21"
currency = "EUR"

[models."claude-fable-5"]
input        = 9.20
output       = 69.00
cache_read   = 0.92
cache_create = 11.50

[models."explicit-1h"]
input          = 10.00
output         = 20.00
cache_read     = 1.00
cache_create   = 12.50
cache_create_1h = 30.00
`

func TestCostForCacheTTLPrices1hWritesAtThe1hRate(t *testing.T) {
	tbl, err := parse([]byte(cacheTTLSample))
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)
	c.OnWarn = func(m string) { t.Errorf("unexpected warning for %q", m) }

	tests := []struct {
		name        string
		model       string
		cacheCreate int64
		create1h    int64
		want        float64
	}{
		{
			name:        "all 5m falls back to the flat rate",
			model:       "claude-fable-5",
			cacheCreate: 20000,
			create1h:    0,
			want:        20000 * 11.50 / 1e6,
		},
		{
			name:        "all 1h uses input x 2 when no explicit rate",
			model:       "claude-fable-5",
			cacheCreate: 20000,
			create1h:    20000,
			want:        20000 * 18.40 / 1e6,
		},
		{
			name:        "mixed splits the total",
			model:       "claude-fable-5",
			cacheCreate: 20000,
			create1h:    5000,
			want:        15000*11.50/1e6 + 5000*18.40/1e6,
		},
		{
			name:        "explicit cache_create_1h wins",
			model:       "explicit-1h",
			cacheCreate: 1000,
			create1h:    400,
			want:        600*12.50/1e6 + 400*30.00/1e6,
		},
		{
			// Never bill more cache-write tokens than the source reported: the
			// 1h portion is capped at the total, not added on top of it.
			name:        "1h portion larger than the total is clamped to the total",
			model:       "claude-fable-5",
			cacheCreate: 1000,
			create1h:    4000,
			want:        1000 * 18.40 / 1e6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.CostForCacheTTL(tt.model, 0, 0, 0, tt.cacheCreate, tt.create1h)
			if got == nil {
				t.Fatal("nil cost for known model")
			}
			if math.Abs(*got-tt.want) > 1e-9 {
				t.Errorf("cost = %v, want %v", *got, tt.want)
			}
		})
	}
}

func TestCostForIsCostForCacheTTLWithoutA1hPortion(t *testing.T) {
	tbl, err := parse([]byte(cacheTTLSample))
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)

	flat := c.CostFor("claude-fable-5", 5, 7, 11, 13)
	split := c.CostForCacheTTL("claude-fable-5", 5, 7, 11, 13, 0)
	if flat == nil || split == nil {
		t.Fatal("nil cost")
	}
	if math.Abs(*flat-*split) > 1e-12 {
		t.Errorf("CostFor = %v, CostForCacheTTL = %v; want equal", *flat, *split)
	}
}

func TestEncodeTableRoundTripsCacheCreate1h(t *testing.T) {
	tbl, err := parse([]byte(cacheTTLSample))
	if err != nil {
		t.Fatal(err)
	}
	round, err := parse(encodeTable(tbl))
	if err != nil {
		t.Fatal(err)
	}
	if got := round.Models["explicit-1h"].CacheCreate1h; got != 30.00 {
		t.Errorf("cache_create_1h = %v, want 30", got)
	}
	if got := round.Models["claude-fable-5"].CacheCreate1h; got != 0 {
		t.Errorf("cache_create_1h = %v, want 0 for a model without one", got)
	}
}
