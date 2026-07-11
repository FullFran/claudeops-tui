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
	seed, err := parse(SeedTOML)
	if err != nil {
		t.Fatal(err)
	}
	if tbl.Updated != seed.Updated {
		t.Fatalf("updated = %q, want seed date %q after merge", tbl.Updated, seed.Updated)
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

func TestLoadOrSeedCorrectsStaleShippedPricesButNotCustomizedOnes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pricing.toml")

	// An existing install from an older claudeops: opus-4-7 still at the wrong
	// $15/$75 tier we shipped, plus opus-4-6 the user customized themselves.
	const old = `
updated = "2026-04-17"
currency = "EUR"

[models."claude-opus-4-7"]
input        = 13.80
output       = 69.00
cache_read   =  1.38
cache_create = 17.25

[models."claude-opus-4-6"]
input        = 50.0
output       = 50.0
cache_read   =  5.0
cache_create =  5.0
`
	if err := os.WriteFile(p, []byte(strings.TrimSpace(old)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := LoadOrSeed(p)
	if err != nil {
		t.Fatal(err)
	}

	// Stale shipped value is corrected to the current seed price.
	if got := tbl.Models["claude-opus-4-7"]; got.Input != 4.60 || got.Output != 23.00 {
		t.Fatalf("stale opus-4-7 not corrected: %+v", got)
	}
	// Customized value is preserved (it does not match the known-wrong value).
	if got := tbl.Models["claude-opus-4-6"]; got.Input != 50.0 || got.Output != 50.0 {
		t.Fatalf("customized opus-4-6 overwritten: %+v", got)
	}

	// Correction is persisted to disk.
	reloaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Models["claude-opus-4-7"].Input != 4.60 {
		t.Fatalf("correction not persisted: %+v", reloaded.Models["claude-opus-4-7"])
	}
	if reloaded.Models["claude-opus-4-6"].Input != 50.0 {
		t.Fatalf("customized value lost after persist: %+v", reloaded.Models["claude-opus-4-6"])
	}
}

func TestSeedIncludesCurrentAnthropicLineup(t *testing.T) {
	tbl, err := parse(SeedTOML)
	if err != nil {
		t.Fatal(err)
	}

	// EUR = USD list price x 0.92.
	cases := []struct {
		model string
		want  ModelPrice
	}{
		{"claude-fable-5", ModelPrice{Input: 9.20, Output: 46.00, CacheRead: 0.92, CacheCreate: 11.50}},
		{"claude-opus-4-8", ModelPrice{Input: 4.60, Output: 23.00, CacheRead: 0.46, CacheCreate: 5.75}},
		{"claude-sonnet-4-6", ModelPrice{Input: 2.76, Output: 13.80, CacheRead: 0.276, CacheCreate: 3.45}},
		{"claude-haiku-4-5", ModelPrice{Input: 0.92, Output: 4.60, CacheRead: 0.092, CacheCreate: 1.15}},
		// Short aliases Claude Code reports when the user selects a model by
		// family name; priced as the current generation of that family.
		{"opus", ModelPrice{Input: 4.60, Output: 23.00, CacheRead: 0.46, CacheCreate: 5.75}},
		{"sonnet", ModelPrice{Input: 2.76, Output: 13.80, CacheRead: 0.276, CacheCreate: 3.45}},
		{"haiku", ModelPrice{Input: 0.92, Output: 4.60, CacheRead: 0.092, CacheCreate: 1.15}},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			got, ok := tbl.Models[tc.model]
			if !ok {
				t.Fatalf("seed missing model %q", tc.model)
			}
			if got != tc.want {
				t.Errorf("seed price for %q = %+v, want %+v", tc.model, got, tc.want)
			}
		})
	}
}

func TestCostForFallsBackToBaseModelForBracketSuffix(t *testing.T) {
	const table = `
updated = "2026-06-10"
currency = "EUR"

[models."claude-fable-5"]
input        =  9.20
output       = 46.00
cache_read   =  0.92
cache_create = 11.50

[models."claude-opus-4-8[1m]"]
input        =  1.00
output       =  2.00
cache_read   =  3.00
cache_create =  4.00
`
	tbl, err := parse([]byte(table))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		model   string
		want    *float64 // nil = unknown
		wantVal float64
	}{
		{name: "exact id still matches", model: "claude-fable-5", wantVal: 9.20},
		{name: "bracket suffix falls back to base id", model: "claude-fable-5[1m]", wantVal: 9.20},
		{name: "explicit bracket entry wins over fallback", model: "claude-opus-4-8[1m]", wantVal: 1.00},
		{name: "unknown base id stays unknown", model: "claude-zaphod-9[1m]", want: nil},
		{name: "leading bracket is not stripped", model: "[1m]claude-fable-5", want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCalculator(tbl)
			warned := ""
			c.OnWarn = func(m string) { warned = m }

			got := c.CostFor(tc.model, 1_000_000, 0, 0, 0)
			unknown := tc.want == nil && tc.wantVal == 0
			if unknown {
				if got != nil {
					t.Fatalf("CostFor(%q) = %v, want nil", tc.model, *got)
				}
				if warned != tc.model {
					t.Errorf("OnWarn got %q, want original model id %q", warned, tc.model)
				}
				return
			}
			if got == nil {
				t.Fatalf("CostFor(%q) = nil, want %v", tc.model, tc.wantVal)
			}
			if math.Abs(*got-tc.wantVal) > 1e-9 {
				t.Errorf("CostFor(%q) = %v, want %v", tc.model, *got, tc.wantVal)
			}
			if warned != "" {
				t.Errorf("unexpected warn for known model: %q", warned)
			}
		})
	}
}

func TestCostForStripsProviderPrefix(t *testing.T) {
	const table = `
updated = "2026-07-11"
currency = "EUR"

[models."gpt-5"]
input        = 1.15
output       = 9.20
cache_read   = 0.115
cache_create = 0.00

[models."gemini-2.5-pro"]
input        = 1.15
output       = 9.20
cache_read   = 0.115
cache_create = 0.00
`
	tbl, err := parse([]byte(table))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		model   string
		wantVal float64 // 0 = unknown
	}{
		{name: "bare openai key", model: "gpt-5", wantVal: 1.15},
		{name: "opencode-qualified openai key", model: "openai/gpt-5", wantVal: 1.15},
		{name: "opencode-qualified gemini key", model: "google/gemini-2.5-pro", wantVal: 1.15},
		{name: "qualified with bracket suffix", model: "openai/gpt-5[1m]", wantVal: 1.15},
		{name: "unknown even after strip", model: "openai/gpt-9", wantVal: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCalculator(tbl)
			c.OnWarn = func(string) {}
			got := c.CostFor(tc.model, 1_000_000, 0, 0, 0)
			if tc.wantVal == 0 {
				if got != nil {
					t.Fatalf("CostFor(%q) = %v, want nil", tc.model, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("CostFor(%q) = nil, want %v", tc.model, tc.wantVal)
			}
			if math.Abs(*got-tc.wantVal) > 1e-9 {
				t.Errorf("CostFor(%q) = %v, want %v", tc.model, *got, tc.wantVal)
			}
		})
	}
}

// TestSeedPricesOtherProviders locks in that the shipped seed actually covers
// the current Claude Sonnet 5 model (previously €0) and the common OpenAI/
// Gemini models, including opencode's provider-qualified keys.
func TestSeedPricesOtherProviders(t *testing.T) {
	tbl, err := parse(SeedTOML)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)
	c.OnWarn = func(string) {}
	for _, model := range []string{
		"claude-sonnet-5",
		"gpt-5", "gpt-5-codex", "gpt-5.1-codex", "gpt-4o", "o3",
		"gemini-2.5-pro", "gemini-2.5-flash",
		"openai/gpt-5", "google/gemini-2.5-flash",
	} {
		if got := c.CostFor(model, 1_000_000, 1_000_000, 0, 0); got == nil || *got == 0 {
			t.Errorf("seed does not price %q (got %v)", model, got)
		}
	}
}

// TestLiteLLMFallbackPricesRealModels verifies the embedded LiteLLM snapshot
// prices the multi-provider models real opencode sessions emit — the ones the
// hand-written seed does not carry — including provider-qualified ids and
// dot-versioned Claude ids.
func TestLiteLLMFallbackPricesRealModels(t *testing.T) {
	// An empty user table forces every lookup through the LiteLLM fallback.
	c := NewCalculator(&Table{Models: map[string]ModelPrice{}})
	c.OnWarn = func(string) {}

	priced := []string{
		"gpt-5.3-codex",
		"gpt-5.4",
		"gpt-5.5",
		"gpt-5.2-codex",
		"openai/gpt-5.3-codex",           // opencode-qualified
		"github-copilot/gpt-5.4",         // copilot-qualified → gpt-5.4
		"google/gemini-2.5-flash",        // → gemini-2.5-flash
		"github-copilot/claude-opus-4.6", // → claude-opus-4-6 via dot normalization
	}
	for _, m := range priced {
		got := c.CostFor(m, 1_000_000, 1_000_000, 0, 0)
		if got == nil || *got == 0 {
			t.Errorf("LiteLLM fallback does not price %q (got %v)", m, got)
		}
	}

	// Genuinely unknown models must still return nil (not a silent zero).
	if got := c.CostFor("totally-made-up-model-zzz", 1_000_000, 0, 0, 0); got != nil {
		t.Errorf("unknown model priced unexpectedly: %v", *got)
	}
}

// TestUserTableWinsOverLiteLLM verifies the editable table takes precedence
// over the embedded snapshot for the same model.
func TestUserTableWinsOverLiteLLM(t *testing.T) {
	c := NewCalculator(&Table{Models: map[string]ModelPrice{
		"gpt-5.4": {Input: 999, Output: 999},
	}})
	c.OnWarn = func(string) {}
	got := c.CostFor("gpt-5.4", 1_000_000, 0, 0, 0)
	if got == nil || *got != 999 {
		t.Errorf("user table did not win over LiteLLM: got %v, want 999", got)
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
