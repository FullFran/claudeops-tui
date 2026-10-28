package pricing

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCostForEvent_ContextTiers(t *testing.T) {
	tbl := &Table{
		Updated:  "2026-10-28",
		Currency: "EUR",
		Models: map[string]ModelPrice{
			"claude-haiku-5-5": {
				Input:         0.092,
				Output:        0.46,
				CacheRead:     0.0092,
				CacheCreate:   0.115,
				CacheCreate1h: 0.184,
				Tiers: []PriceTier{
					{
						Threshold:     100000,
						Input:         0.46,
						Output:        2.30,
						CacheRead:     0.046,
						CacheCreate:   0.575,
						CacheCreate1h: 0.92,
					},
				},
			},
			"gpt-6.1-sol": {
				Input:     1.84,
				Output:    9.20,
				CacheRead: 0.092,
				Tiers: []PriceTier{
					{
						Threshold: 272000,
						Input:     3.68,
						Output:    13.80,
						CacheRead: 0.184,
					},
				},
			},
		},
	}
	c := NewCalculator(tbl)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		model         string
		in            int64
		out           int64
		cacheRead     int64
		cacheCreate   int64
		cacheCreate1h int64
		wantEUR       float64
	}{
		{
			name:        "haiku-5.5 below threshold (base rates)",
			model:       "claude-haiku-5-5",
			in:          50000,
			out:         10000,
			cacheRead:   20000,
			cacheCreate: 10000,
			// total prompt = 50k + 20k + 10k = 80k <= 100k
			// 50k*0.092/1M + 10k*0.46/1M + 20k*0.0092/1M + 10k*0.115/1M
			// = 0.0046 + 0.0046 + 0.000184 + 0.00115 = 0.010534
			wantEUR: 0.010534,
		},
		{
			name:        "haiku-5.5 exact threshold <=100000 (base rates)",
			model:       "claude-haiku-5-5",
			in:          70000,
			out:         1000,
			cacheRead:   20000,
			cacheCreate: 10000,
			// total prompt = 70k + 20k + 10k = 100k <= 100k
			// 70k*0.092/1M + 1k*0.46/1M + 20k*0.0092/1M + 10k*0.115/1M
			// = 0.00644 + 0.00046 + 0.000184 + 0.00115 = 0.008234
			wantEUR: 0.008234,
		},
		{
			name:        "haiku-5.5 above threshold >100000 (tier rates)",
			model:       "claude-haiku-5-5",
			in:          70001,
			out:         1000,
			cacheRead:   20000,
			cacheCreate: 10000,
			// total prompt = 70001 + 20k + 10k = 100001 > 100k
			// 70001*0.46/1M + 1k*2.30/1M + 20k*0.046/1M + 10k*0.575/1M
			// = 0.03220046 + 0.0023 + 0.00092 + 0.00575 = 0.04117046
			wantEUR: 0.04117046,
		},
		{
			name:          "haiku-5.5 1h subset not double counted in total prompt",
			model:         "claude-haiku-5-5",
			in:            50000,
			out:           0,
			cacheRead:     20000,
			cacheCreate:   25000,
			cacheCreate1h: 20000,
			// total prompt = 50k + 20k + 25k = 95k <= 100k (1h is subset of cacheCreate, not added again!)
			// 50k*0.092/1M + 0 + 20k*0.0092/1M + 5k*0.115/1M + 20k*0.184/1M
			// = 0.0046 + 0.000184 + 0.000575 + 0.00368 = 0.009039
			wantEUR: 0.009039,
		},
		{
			name:      "gpt-6.1-sol <=272000 short tier",
			model:     "gpt-6.1-sol",
			in:        200000,
			out:       10000,
			cacheRead: 72000,
			// total prompt = 200k + 72k = 272k <= 272k
			// 200k*1.84/1M + 10k*9.20/1M + 72k*0.092/1M
			// = 0.368 + 0.092 + 0.006624 = 0.466624
			wantEUR: 0.466624,
		},
		{
			name:        "gpt-6.1-sol cache writes do not inflate context threshold",
			model:       "gpt-6.1-sol",
			in:          200000,
			out:         0,
			cacheRead:   72000,
			cacheCreate: 50000, // For non-Haiku, cache writes are NOT counted toward prompt threshold
			// total prompt = 200k + 72k = 272k <= 272k -> base rates
			// 200k*1.84/1M + 72k*0.092/1M + 50k*0
			// = 0.368 + 0.006624 = 0.374624
			wantEUR: 0.374624,
		},
		{
			name:      "gpt-6.1-sol >272000 long tier",
			model:     "gpt-6.1-sol",
			in:        200001,
			out:       10000,
			cacheRead: 72000,
			// total prompt = 272001 > 272k
			// 200001*3.68/1M + 10k*13.80/1M + 72k*0.184/1M
			// = 0.73600368 + 0.138 + 0.013248 = 0.88725168
			wantEUR: 0.88725168,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.CostForEvent(tt.model, tt.in, tt.out, tt.cacheRead, tt.cacheCreate, tt.cacheCreate1h, now)
			if got == nil {
				t.Fatalf("CostForEvent(%q) = nil, want %v", tt.model, tt.wantEUR)
			}
			diff := *got - tt.wantEUR
			if diff < -1e-6 || diff > 1e-6 {
				t.Errorf("CostForEvent(%q) = %v, want %v (diff: %v)", tt.model, *got, tt.wantEUR, diff)
			}
		})
	}
}

func TestCostForEvent_ScheduledDates(t *testing.T) {
	tbl := &Table{
		Updated:  "2026-10-28",
		Currency: "EUR",
		Models: map[string]ModelPrice{
			"gemini-4-argon": {
				Input:     1.84, // USD 2
				Output:    9.20, // USD 10
				CacheRead: 0.092,
				Schedule: []PriceSchedule{
					{
						EffectiveDate: "2026-12-01",
						Input:         3.68,  // USD 4
						Output:        18.40, // USD 20
						CacheRead:     0.184,
					},
				},
			},
			"gemini-3.8-flash": {
				Input:     0.69,  // USD 0.75
				Output:    3.45,  // USD 3.75
				CacheRead: 0.069, // USD 0.075
				Schedule: []PriceSchedule{
					{
						EffectiveDate: "2027-01-01",
						Input:         1.38,  // USD 1.50
						Output:        6.90,  // USD 7.50
						CacheRead:     0.138, // USD 0.15
					},
				},
			},
		},
	}
	c := NewCalculator(tbl)

	tests := []struct {
		name    string
		model   string
		ts      time.Time
		wantEUR float64 // for 1M in, 1M out, 1M cacheRead
	}{
		{
			name:    "argon before 2026-12-01 (base rate)",
			model:   "gemini-4-argon",
			ts:      time.Date(2026, 11, 30, 23, 59, 59, 0, time.UTC),
			wantEUR: 1.84 + 9.20 + 0.092, // 11.132
		},
		{
			name:    "argon at 2026-12-01T00:00:00Z (thereafter rate)",
			model:   "gemini-4-argon",
			ts:      time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
			wantEUR: 3.68 + 18.40 + 0.184, // 22.264
		},
		{
			name:    "argon after 2026-12-01 (thereafter rate)",
			model:   "gemini-4-argon",
			ts:      time.Date(2026, 12, 15, 10, 0, 0, 0, time.UTC),
			wantEUR: 3.68 + 18.40 + 0.184,
		},
		{
			name:    "argon zero timestamp uses deterministic base rates",
			model:   "gemini-4-argon",
			ts:      time.Time{},
			wantEUR: 1.84 + 9.20 + 0.092,
		},
		{
			name:  "argon non-UTC timezone evaluated in UTC",
			model: "gemini-4-argon",
			// 2026-11-30 19:00:00 EST (-5h) is 2026-12-01 00:00:00 UTC
			ts:      time.Date(2026, 11, 30, 19, 0, 0, 0, time.FixedZone("EST", -5*3600)),
			wantEUR: 3.68 + 18.40 + 0.184,
		},
		{
			name:    "flash before 2027-01-01 (base rate)",
			model:   "gemini-3.8-flash",
			ts:      time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			wantEUR: 0.69 + 3.45 + 0.069, // 4.209
		},
		{
			name:    "flash at 2027-01-01 (thereafter rate)",
			model:   "gemini-3.8-flash",
			ts:      time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			wantEUR: 1.38 + 6.90 + 0.138, // 8.418
		},
		{
			name:    "flash zero timestamp uses deterministic base rate",
			model:   "gemini-3.8-flash",
			ts:      time.Time{},
			wantEUR: 0.69 + 3.45 + 0.069,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.CostForEvent(tt.model, 1000000, 1000000, 1000000, 0, 0, tt.ts)
			if got == nil {
				t.Fatalf("CostForEvent(%q, %v) = nil, want %v", tt.model, tt.ts, tt.wantEUR)
			}
			diff := *got - tt.wantEUR
			if diff < -1e-6 || diff > 1e-6 {
				t.Errorf("CostForEvent(%q, %v) = %v, want %v", tt.model, tt.ts, *got, tt.wantEUR)
			}
		})
	}
}

func TestEncodeTable_RoundTrip(t *testing.T) {
	orig := &Table{
		Updated:  "2026-10-28",
		Currency: "EUR",
		Models: map[string]ModelPrice{
			"claude-haiku-5-5": {
				Input:         0.092,
				Output:        0.46,
				CacheRead:     0.0092,
				CacheCreate:   0.115,
				CacheCreate1h: 0.184,
				Tiers: []PriceTier{
					{
						Threshold:     100000,
						Input:         0.46,
						Output:        2.30,
						CacheRead:     0.046,
						CacheCreate:   0.575,
						CacheCreate1h: 0.92,
					},
				},
			},
			"gemini-4-argon": {
				Input:     1.84,
				Output:    9.20,
				CacheRead: 0.092,
				Schedule: []PriceSchedule{
					{
						EffectiveDate: "2026-12-01",
						Input:         3.68,
						Output:        18.40,
						CacheRead:     0.184,
					},
				},
			},
		},
	}

	encoded := encodeTable(orig)
	round, err := parse(encoded)
	if err != nil {
		t.Fatalf("parse(encodeTable): %v", err)
	}

	haiku := round.Models["claude-haiku-5-5"]
	if len(haiku.Tiers) != 1 {
		t.Fatalf("haiku tiers len = %d, want 1", len(haiku.Tiers))
	}
	tier := haiku.Tiers[0]
	if tier.Threshold != 100000 || tier.Input != 0.46 || tier.Output != 2.30 || tier.CacheRead != 0.046 || tier.CacheCreate != 0.575 || tier.CacheCreate1h != 0.92 {
		t.Errorf("haiku tier roundtrip mismatch: %+v", tier)
	}

	argon := round.Models["gemini-4-argon"]
	if len(argon.Schedule) != 1 {
		t.Fatalf("argon schedule len = %d, want 1", len(argon.Schedule))
	}
	sched := argon.Schedule[0]
	if sched.EffectiveDate != "2026-12-01" || sched.Input != 3.68 || sched.Output != 18.40 || sched.CacheRead != 0.184 {
		t.Errorf("argon schedule roundtrip mismatch: %+v", sched)
	}
}

func TestValidation_InvalidConfigPreservesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.toml")

	validContent := `# valid pricing
updated = "2026-09-26"
currency = "EUR"

[models."claude-opus-4-6"]
input = 4.60
output = 23.00
cache_read = 0.46
cache_create = 5.75
`
	if err := os.WriteFile(path, []byte(validContent), 0o644); err != nil {
		t.Fatal(err)
	}

	invalidCases := []struct {
		name    string
		content string
	}{
		{
			name: "invalid date format",
			content: validContent + `
[models."bad-date"]
input = 1.0
output = 2.0
cache_read = 0.1
cache_create = 0.0

[[models."bad-date".schedule]]
effective_date = "invalid-date-here"
input = 2.0
output = 4.0
cache_read = 0.2
cache_create = 0.0
`,
		},
		{
			name: "negative threshold",
			content: validContent + `
[models."bad-thresh"]
input = 1.0
output = 2.0
cache_read = 0.1
cache_create = 0.0

[[models."bad-thresh".tiers]]
threshold = -500
input = 2.0
output = 4.0
cache_read = 0.2
cache_create = 0.0
`,
		},
		{
			name: "negative price",
			content: validContent + `
[models."bad-price"]
input = -1.0
output = 2.0
cache_read = 0.1
cache_create = 0.0
`,
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			// Write the invalid content to path
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatalf("Load(%s) should have failed validation", tc.name)
			}
		})
	}
}

func TestLoadOrSeed_EnrichUntouchedOldSeed_PreserveCustomized(t *testing.T) {
	current := &Table{
		Updated:  "2026-09-26",
		Currency: "EUR",
		Models: map[string]ModelPrice{
			"gpt-6-astra": {
				Input:       9.20,
				Output:      46.00,
				CacheRead:   0.92,
				CacheCreate: 11.50,
			},
			"gpt-6-sol": {
				Input:       3.00, // customized by user!
				Output:      9.20,
				CacheRead:   0.184,
				CacheCreate: 2.30,
			},
		},
	}

	seed := &Table{
		Updated:  "2026-10-28",
		Currency: "EUR",
		Models: map[string]ModelPrice{
			"gpt-6-astra": {
				Input:       9.20,
				Output:      46.00,
				CacheRead:   0.92,
				CacheCreate: 11.50,
				Tiers: []PriceTier{
					{Threshold: 272000, Input: 18.40, Output: 69.00, CacheRead: 1.84, CacheCreate: 11.50},
				},
			},
			"gpt-6-sol": {
				Input:       1.84,
				Output:      9.20,
				CacheRead:   0.184,
				CacheCreate: 2.30,
				Tiers: []PriceTier{
					{Threshold: 272000, Input: 3.68, Output: 13.80, CacheRead: 0.368, CacheCreate: 2.30},
				},
			},
		},
	}

	changed := enrichUntouchedSeedModels(current, seed)
	if !changed {
		t.Fatalf("enrichUntouchedSeedModels should report changed")
	}

	// Customized sol must preserve 3.00 input and NOT be overwritten with tiers
	sol := current.Models["gpt-6-sol"]
	if sol.Input != 3.00 {
		t.Errorf("customized gpt-6-sol input was overwritten: got %v, want 3.00", sol.Input)
	}
	if len(sol.Tiers) != 0 {
		t.Errorf("customized gpt-6-sol should NOT have been enriched with tiers, got %v", sol.Tiers)
	}

	// Untouched astra gets enriched with new tiers
	astra := current.Models["gpt-6-astra"]
	if astra.Input != 9.20 {
		t.Errorf("astra input: got %v, want 9.20", astra.Input)
	}
	if len(astra.Tiers) != 1 {
		t.Errorf("untouched astra should have been enriched with 1 tier, got %d", len(astra.Tiers))
	}
}
