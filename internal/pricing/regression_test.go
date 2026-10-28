package pricing_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/pricing"
)

// Blocker 1: PriceTier.EffectiveDate is parsed/encoded/validated but ignored by CostForEvent.
// Table base input1 tier threshold1 input2 effective_date2027-01-01, event2026-10-28 with 1M input costs 2, expected 1;
// at cutoff expected 2; zero timestamp should not activate future dated tier.
func TestRegression_Blocker1_TierEffectiveDate(t *testing.T) {
	tbl := &pricing.Table{
		Updated:  "2026-10-28",
		Currency: "EUR",
		Models: map[string]pricing.ModelPrice{
			"test-model": {
				Input: 1.0,
				Tiers: []pricing.PriceTier{
					{
						Threshold:     1,
						EffectiveDate: "2027-01-01",
						Input:         2.0,
					},
				},
			},
		},
	}
	calc := pricing.NewCalculator(tbl)

	t.Run("event before cutoff uses base rate", func(t *testing.T) {
		eventTime := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		cost := calc.CostForEvent("test-model", 1_000_000, 0, 0, 0, 0, eventTime)
		if cost == nil {
			t.Fatal("expected non-nil cost")
		}
		if *cost != 1.0 {
			t.Errorf("cost before cutoff = %v, want 1.0", *cost)
		}
	})

	t.Run("event at cutoff uses tier rate", func(t *testing.T) {
		cutoffTime := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		cost := calc.CostForEvent("test-model", 1_000_000, 0, 0, 0, 0, cutoffTime)
		if cost == nil {
			t.Fatal("expected non-nil cost")
		}
		if *cost != 2.0 {
			t.Errorf("cost at cutoff = %v, want 2.0", *cost)
		}
	})

	t.Run("zero timestamp does not activate future dated tier", func(t *testing.T) {
		cost := calc.CostForEvent("test-model", 1_000_000, 0, 0, 0, 0, time.Time{})
		if cost == nil {
			t.Fatal("expected non-nil cost")
		}
		if *cost != 1.0 {
			t.Errorf("cost with zero timestamp = %v, want 1.0", *cost)
		}
	})
}

// Blocker 2: explicit 0 rates ignored via >0 fallback for tier/schedule.
// Base input1 active tier input0 OR active schedule input0 with 1M input costs 1 expected 0.
// Rules should be complete rate sets, zero is meaningful; preserve documented default derivation for omitted CacheCreate1h.
func TestRegression_Blocker2_ExplicitZeroRates(t *testing.T) {
	t.Run("active tier explicit zero input", func(t *testing.T) {
		tbl := &pricing.Table{
			Updated:  "2026-10-28",
			Currency: "EUR",
			Models: map[string]pricing.ModelPrice{
				"zero-tier-model": {
					Input: 1.0,
					Tiers: []pricing.PriceTier{
						{
							Threshold: 100,
							Input:     0.0,
						},
					},
				},
			},
		}
		calc := pricing.NewCalculator(tbl)
		cost := calc.CostForEvent("zero-tier-model", 1_000_000, 0, 0, 0, 0, time.Time{})
		if cost == nil {
			t.Fatal("expected non-nil cost")
		}
		if *cost != 0.0 {
			t.Errorf("active tier with input 0: cost = %v, want 0.0", *cost)
		}
	})

	t.Run("active schedule explicit zero input", func(t *testing.T) {
		tbl := &pricing.Table{
			Updated:  "2026-10-28",
			Currency: "EUR",
			Models: map[string]pricing.ModelPrice{
				"zero-sched-model": {
					Input: 1.0,
					Schedule: []pricing.PriceSchedule{
						{
							EffectiveDate: "2026-01-01",
							Input:         0.0,
						},
					},
				},
			},
		}
		calc := pricing.NewCalculator(tbl)
		ts := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		cost := calc.CostForEvent("zero-sched-model", 1_000_000, 0, 0, 0, 0, ts)
		if cost == nil {
			t.Fatal("expected non-nil cost")
		}
		if *cost != 0.0 {
			t.Errorf("active schedule with input 0: cost = %v, want 0.0", *cost)
		}
	})

	t.Run("omitted CacheCreate1h preserves documented default derivation", func(t *testing.T) {
		tbl := &pricing.Table{
			Updated:  "2026-10-28",
			Currency: "EUR",
			Models: map[string]pricing.ModelPrice{
				"deriv-model": {
					Input:       1.0,
					CacheCreate: 0.5,
					Tiers: []pricing.PriceTier{
						{
							Threshold:     100,
							Input:         2.0,
							CacheCreate:   1.0,
							CacheCreate1h: 0.0, // omitted, should derive as Input * 2 = 4.0
						},
					},
				},
			},
		}
		calc := pricing.NewCalculator(tbl)
		// 1M 1h-cache writes with active tier:
		// cacheCreate = 1M, cacheCreate1h = 1M (5m part = 0).
		// Rate should be 2.0 * 2 = 4.0 EUR/1M tokens.
		cost := calc.CostForEvent("deriv-model", 1000, 0, 0, 1_000_000, 1_000_000, time.Time{})
		if cost == nil {
			t.Fatal("expected non-nil cost")
		}
		// input: 1000 * 2.0 / 1M = 0.002
		// 1h cache: 1_000_000 * 4.0 / 1M = 4.0
		// total = 4.002
		expected := 0.002 + 4.0
		if math.Abs(*cost-expected) > 1e-9 {
			t.Errorf("cost with derived 1h rate = %v, want %v", *cost, expected)
		}
	})
}

// Blocker 3: seed Haiku5.5 in80000/read10000/create10000/create1h20000 currently costs .04646 vs expected .009292, same as 1h10000.
// Clamp 1h subset to total BEFORE both threshold evaluation and billing, no inflated total.
func TestRegression_Blocker3_Haiku1hSubsetClamping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.toml")
	tbl, err := pricing.LoadOrSeed(path)
	if err != nil {
		t.Fatalf("LoadOrSeed failed: %v", err)
	}
	calc := pricing.NewCalculator(tbl)

	// Oversized 1h subset: create1h = 20000 > create = 10000.
	// Clamped to 10000 before threshold: promptTokens = 80000 + 10000 + 10000 = 100000 <= 100000 (base tier).
	costOversized := calc.CostForCacheTTL("claude-haiku-5-5", 80000, 0, 10000, 10000, 20000)
	if costOversized == nil {
		t.Fatal("expected non-nil cost")
	}

	// Normal 1h subset: create1h = 10000 == create = 10000.
	costNormal := calc.CostForCacheTTL("claude-haiku-5-5", 80000, 0, 10000, 10000, 10000)
	if costNormal == nil {
		t.Fatal("expected non-nil cost")
	}

	expected := 0.009292
	if math.Abs(*costOversized-expected) > 1e-6 {
		t.Errorf("oversized 1h cost = %v, want %v", *costOversized, expected)
	}
	if math.Abs(*costOversized-*costNormal) > 1e-9 {
		t.Errorf("oversized 1h cost (%v) != normal 1h cost (%v)", *costOversized, *costNormal)
	}
}

// Blocker 4: custom input0.123456 becomes 0.1235 on LoadOrSeed merge+Load; tiers 0.234567 and schedules 0.345678 likewise lossy.
// Replace fixed4dp encoding with roundtrip-safe floats across base and new rule fields preserving exact existing custom float values.
// Pin LoadOrSeed followed by Load equality via public API.
func TestRegression_Blocker4_FloatRoundTripLossless(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.toml")

	initialContent := `# custom rates
updated = "2026-10-28"
currency = "EUR"

[models."custom-precision"]
input         = 0.123456
output        = 1.234567
cache_read    = 0.012345
cache_create  = 0.023456

[[models."custom-precision".tiers]]
threshold     = 100000
input         = 0.234567
output        = 2.345678
cache_read    = 0.023456
cache_create  = 0.034567

[[models."custom-precision".schedule]]
effective_date = "2027-01-01"
input         = 0.345678
output        = 3.456789
cache_read    = 0.034567
cache_create  = 0.045678
`
	if err := os.WriteFile(path, []byte(initialContent), 0o644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	merged, err := pricing.LoadOrSeed(path)
	if err != nil {
		t.Fatalf("LoadOrSeed failed: %v", err)
	}

	loaded, err := pricing.Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	customMerged := merged.Models["custom-precision"]
	customLoaded := loaded.Models["custom-precision"]

	if !customMerged.Equal(customLoaded) {
		t.Errorf("models not equal after LoadOrSeed and Load:\nmerged: %+v\nloaded: %+v", customMerged, customLoaded)
	}

	// Verify exact float values are preserved without 4dp rounding
	if customLoaded.Input != 0.123456 {
		t.Errorf("base input = %v, want exact 0.123456", customLoaded.Input)
	}
	if len(customLoaded.Tiers) != 1 || customLoaded.Tiers[0].Input != 0.234567 {
		t.Errorf("tier input = %v, want exact 0.234567", customLoaded.Tiers[0].Input)
	}
	if len(customLoaded.Schedule) != 1 || customLoaded.Schedule[0].Input != 0.345678 {
		t.Errorf("schedule input = %v, want exact 0.345678", customLoaded.Schedule[0].Input)
	}

	// Verify nonfinite values in new rule rates are rejected by Validate()
	t.Run("nonfinite rates in tier rejected", func(t *testing.T) {
		badTier := &pricing.Table{
			Updated:  "2026-10-28",
			Currency: "EUR",
			Models: map[string]pricing.ModelPrice{
				"bad-tier": {
					Input: 1.0,
					Tiers: []pricing.PriceTier{
						{
							Threshold: 100,
							Input:     math.NaN(),
						},
					},
				},
			},
		}
		if err := badTier.Validate(); err == nil {
			t.Error("expected Validate() to reject NaN tier rate, but got nil")
		}

		badTierInf := &pricing.Table{
			Updated:  "2026-10-28",
			Currency: "EUR",
			Models: map[string]pricing.ModelPrice{
				"bad-tier-inf": {
					Input: 1.0,
					Tiers: []pricing.PriceTier{
						{
							Threshold: 100,
							Input:     math.Inf(1),
						},
					},
				},
			},
		}
		if err := badTierInf.Validate(); err == nil {
			t.Error("expected Validate() to reject Inf tier rate, but got nil")
		}
	})

	t.Run("nonfinite rates in schedule rejected", func(t *testing.T) {
		badSched := &pricing.Table{
			Updated:  "2026-10-28",
			Currency: "EUR",
			Models: map[string]pricing.ModelPrice{
				"bad-sched": {
					Input: 1.0,
					Schedule: []pricing.PriceSchedule{
						{
							EffectiveDate: "2027-01-01",
							Input:         math.NaN(),
						},
					},
				},
			},
		}
		if err := badSched.Validate(); err == nil {
			t.Error("expected Validate() to reject NaN schedule rate, but got nil")
		}
	})
}
