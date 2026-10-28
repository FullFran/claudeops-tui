package pricing

import (
	"math"
	"testing"
	"time"
)

func TestSeedTOML_VerifiedModels202610(t *testing.T) {
	tbl, err := parse(SeedTOML)
	if err != nil {
		t.Fatalf("parse(SeedTOML): %v", err)
	}

	calc := NewCalculator(tbl)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	t.Run("claude-sonnet-5-5 rates with cache_create_1h", func(t *testing.T) {
		m, ok := tbl.Models["claude-sonnet-5-5"]
		if !ok {
			t.Fatalf("seed missing claude-sonnet-5-5")
		}
		if m.Input != 1.84 || m.Output != 9.20 || m.CacheRead != 0.092 || m.CacheCreate != 2.30 || m.CacheCreate1h != 3.68 {
			t.Errorf("sonnet 5.5 rates mismatch: %+v", m)
		}
	})

	t.Run("claude-haiku-5-5 base rates and prompt context tier", func(t *testing.T) {
		m, ok := tbl.Models["claude-haiku-5-5"]
		if !ok {
			t.Fatalf("seed missing claude-haiku-5-5")
		}
		if m.Input != 0.092 || m.Output != 0.46 || m.CacheRead != 0.0092 || m.CacheCreate != 0.115 || m.CacheCreate1h != 0.184 {
			t.Errorf("haiku 5.5 base rates mismatch: %+v", m)
		}
		if len(m.Tiers) != 1 {
			t.Fatalf("haiku 5.5 tiers len = %d, want 1", len(m.Tiers))
		}
		tier := m.Tiers[0]
		if tier.Threshold != 100000 || tier.Input != 0.46 || tier.Output != 2.30 || tier.CacheRead != 0.046 || tier.CacheCreate != 0.575 || tier.CacheCreate1h != 0.92 {
			t.Errorf("haiku 5.5 tier mismatch: %+v", tier)
		}

		// Verify CostForEvent below and above threshold
		costBelow := calc.CostForEvent("claude-haiku-5-5", 80000, 1000, 10000, 10000, 0, now)
		wantBelow := (80000*0.092 + 1000*0.46 + 10000*0.0092 + 10000*0.115) / 1e6
		if costBelow == nil || math.Abs(*costBelow-wantBelow) > 1e-9 {
			t.Errorf("haiku below threshold: got %v, want %v", costBelow, wantBelow)
		}

		costAbove := calc.CostForEvent("claude-haiku-5-5", 80001, 1000, 10000, 10000, 0, now)
		wantAbove := (80001*0.46 + 1000*2.30 + 10000*0.046 + 10000*0.575) / 1e6
		if costAbove == nil || math.Abs(*costAbove-wantAbove) > 1e-9 {
			t.Errorf("haiku above threshold: got %v, want %v", costAbove, wantAbove)
		}
	})

	t.Run("gpt-6.1-sol base rates and short/long tier", func(t *testing.T) {
		m, ok := tbl.Models["gpt-6.1-sol"]
		if !ok {
			t.Fatalf("seed missing gpt-6.1-sol")
		}
		if m.Input != 1.84 || m.Output != 9.20 || m.CacheRead != 0.092 {
			t.Errorf("gpt-6.1-sol base mismatch: %+v", m)
		}
		if len(m.Tiers) != 1 || m.Tiers[0].Threshold != 272000 {
			t.Fatalf("gpt-6.1-sol tiers mismatch: %+v", m.Tiers)
		}
		if m.Tiers[0].Input != 3.68 || m.Tiers[0].Output != 13.80 || m.Tiers[0].CacheRead != 0.184 {
			t.Errorf("gpt-6.1-sol tier rates mismatch: %+v", m.Tiers[0])
		}
	})

	t.Run("gpt-6.1-terra base rates and short/long tier", func(t *testing.T) {
		m, ok := tbl.Models["gpt-6.1-terra"]
		if !ok {
			t.Fatalf("seed missing gpt-6.1-terra")
		}
		if m.Input != 0.92 || m.Output != 4.60 || m.CacheRead != 0.046 {
			t.Errorf("gpt-6.1-terra base mismatch: %+v", m)
		}
		if len(m.Tiers) != 1 || m.Tiers[0].Threshold != 272000 {
			t.Fatalf("gpt-6.1-terra tiers mismatch: %+v", m.Tiers)
		}
		if m.Tiers[0].Input != 1.84 || m.Tiers[0].Output != 6.90 || m.Tiers[0].CacheRead != 0.092 {
			t.Errorf("gpt-6.1-terra tier rates mismatch: %+v", m.Tiers[0])
		}
	})

	t.Run("gemini-4-argon schedule effective 2026-12-01", func(t *testing.T) {
		m, ok := tbl.Models["gemini-4-argon"]
		if !ok {
			t.Fatalf("seed missing gemini-4-argon")
		}
		if m.Input != 1.84 || m.Output != 9.20 || m.CacheRead != 0.092 {
			t.Errorf("gemini-4-argon base mismatch: %+v", m)
		}
		if len(m.Schedule) != 1 || m.Schedule[0].EffectiveDate != "2026-12-01" {
			t.Fatalf("gemini-4-argon schedule mismatch: %+v", m.Schedule)
		}
		if m.Schedule[0].Input != 3.68 || m.Schedule[0].Output != 18.40 || m.Schedule[0].CacheRead != 0.184 {
			t.Errorf("gemini-4-argon schedule rates mismatch: %+v", m.Schedule[0])
		}
	})

	t.Run("gemini-3.8-flash schedule effective 2027-01-01", func(t *testing.T) {
		m, ok := tbl.Models["gemini-3.8-flash"]
		if !ok {
			t.Fatalf("seed missing gemini-3.8-flash")
		}
		if len(m.Schedule) != 1 || m.Schedule[0].EffectiveDate != "2027-01-01" {
			t.Fatalf("gemini-3.8-flash schedule mismatch: %+v", m.Schedule)
		}
		if m.Schedule[0].Input != 1.38 || m.Schedule[0].Output != 6.90 || m.Schedule[0].CacheRead != 0.138 {
			t.Errorf("gemini-3.8-flash schedule rates mismatch: %+v", m.Schedule[0])
		}
	})

	t.Run("existing gpt-6 models have long context tiers", func(t *testing.T) {
		astra := tbl.Models["gpt-6-astra"]
		if len(astra.Tiers) != 1 || astra.Tiers[0].Threshold != 272000 {
			t.Fatalf("astra tiers mismatch: %+v", astra.Tiers)
		}
		if astra.Tiers[0].Input != 18.40 || astra.Tiers[0].Output != 69.00 || astra.Tiers[0].CacheRead != 1.84 {
			t.Errorf("astra tier rates mismatch: %+v", astra.Tiers[0])
		}

		sol := tbl.Models["gpt-6-sol"]
		if len(sol.Tiers) != 1 || sol.Tiers[0].Threshold != 272000 {
			t.Fatalf("sol tiers mismatch: %+v", sol.Tiers)
		}
		if sol.Tiers[0].Input != 3.68 || sol.Tiers[0].Output != 13.80 || sol.Tiers[0].CacheRead != 0.368 {
			t.Errorf("sol tier rates mismatch: %+v", sol.Tiers[0])
		}

		luna := tbl.Models["gpt-6-luna"]
		if len(luna.Tiers) != 1 || luna.Tiers[0].Threshold != 272000 {
			t.Fatalf("luna tiers mismatch: %+v", luna.Tiers)
		}
		if luna.Tiers[0].Input != 0.184 || luna.Tiers[0].Output != 0.69 || luna.Tiers[0].CacheRead != 0.0184 {
			t.Errorf("luna tier rates mismatch: %+v", luna.Tiers[0])
		}
	})
}
