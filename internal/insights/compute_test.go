package insights_test

import (
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/insights"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// ---------------------------------------------------------------------------
// CacheEfficiency
// ---------------------------------------------------------------------------

func TestCacheEfficiency(t *testing.T) {
	tests := []struct {
		name        string
		agg         store.Aggregates
		wantOk      bool
		wantSev     insights.Severity
		wantTitleRE string
	}{
		{
			name:   "zero denominator skips",
			agg:    store.Aggregates{},
			wantOk: false,
		},
		{
			name: "10 percent ratio gives Warn",
			// cache_read=10, in=50, out=40 => denom=100 => ratio=10%
			agg:         store.Aggregates{CacheReadTokens: 10, InTokens: 50, OutTokens: 40},
			wantOk:      true,
			wantSev:     insights.Warn,
			wantTitleRE: "Low cache efficiency",
		},
		{
			name: "30 percent ratio gives Tip",
			// cache_read=30, in=40, out=30 => denom=100 => ratio=30%
			agg:         store.Aggregates{CacheReadTokens: 30, InTokens: 40, OutTokens: 30},
			wantOk:      true,
			wantSev:     insights.Tip,
			wantTitleRE: "Moderate cache efficiency",
		},
		{
			name: "50 percent ratio gives Info",
			// cache_read=50, in=30, out=20 => denom=100 => ratio=50%
			agg:         store.Aggregates{CacheReadTokens: 50, InTokens: 30, OutTokens: 20},
			wantOk:      true,
			wantSev:     insights.Info,
			wantTitleRE: "Good cache efficiency",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := insights.CacheEfficiency(tc.agg)
			if ok != tc.wantOk {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if got.Severity != tc.wantSev {
				t.Errorf("severity=%v, want %v", got.Severity, tc.wantSev)
			}
			if tc.wantTitleRE != "" && !strings.Contains(got.Title, tc.wantTitleRE) {
				t.Errorf("title=%q does not contain %q", got.Title, tc.wantTitleRE)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ModelMix
// ---------------------------------------------------------------------------

func TestModelMix(t *testing.T) {
	tests := []struct {
		name        string
		models      []store.ModelAgg
		wantOk      bool
		wantSev     insights.Severity
		wantTitleRE string
	}{
		{
			name:   "one model skips",
			models: []store.ModelAgg{{Model: "claude-3", CostEUR: 1.0}},
			wantOk: false,
		},
		{
			name:   "zero total cost skips",
			models: []store.ModelAgg{{Model: "a", CostEUR: 0}, {Model: "b", CostEUR: 0}},
			wantOk: false,
		},
		{
			name: "80 percent on one model gives Tip",
			models: []store.ModelAgg{
				{Model: "claude-opus", CostEUR: 8.0},
				{Model: "claude-haiku", CostEUR: 2.0},
			},
			wantOk:      true,
			wantSev:     insights.Tip,
			wantTitleRE: "claude-opus",
		},
		{
			name: "50/50 split gives Info",
			models: []store.ModelAgg{
				{Model: "claude-sonnet", CostEUR: 5.0},
				{Model: "claude-haiku", CostEUR: 5.0},
			},
			wantOk:      true,
			wantSev:     insights.Info,
			wantTitleRE: "Balanced model mix",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := insights.ModelMix(tc.models)
			if ok != tc.wantOk {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if got.Severity != tc.wantSev {
				t.Errorf("severity=%v, want %v", got.Severity, tc.wantSev)
			}
			if tc.wantTitleRE != "" && !strings.Contains(got.Title, tc.wantTitleRE) {
				t.Errorf("title=%q does not contain %q", got.Title, tc.wantTitleRE)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CostTrend
// ---------------------------------------------------------------------------

func TestCostTrend(t *testing.T) {
	makeDays := func(costs []float64) []store.DailyAgg {
		out := make([]store.DailyAgg, len(costs))
		for i, c := range costs {
			out[i] = store.DailyAgg{CostEUR: c}
		}
		return out
	}

	tests := []struct {
		name        string
		daily       []store.DailyAgg
		wantOk      bool
		wantSev     insights.Severity
		wantTitleRE string
	}{
		{
			name:   "fewer than 14 days skips",
			daily:  makeDays(make([]float64, 13)),
			wantOk: false,
		},
		{
			name: "60 percent increase gives Warn",
			// days[0..6]: this week (most recent), days[7..13]: last week
			daily: makeDays([]float64{
				1.6, 1.6, 1.6, 1.6, 1.6, 1.6, 1.6,
				1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0,
			}),
			wantOk:      true,
			wantSev:     insights.Warn,
			wantTitleRE: "Cost up",
		},
		{
			name: "20 percent increase gives Info",
			daily: makeDays([]float64{
				1.2, 1.2, 1.2, 1.2, 1.2, 1.2, 1.2,
				1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0,
			}),
			wantOk:      true,
			wantSev:     insights.Info,
			wantTitleRE: "Cost up",
		},
		{
			name: "decrease gives Info",
			daily: makeDays([]float64{
				0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9,
				1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0,
			}),
			wantOk:      true,
			wantSev:     insights.Info,
			wantTitleRE: "Cost down",
		},
		{
			name:   "last week zero skips",
			daily:  makeDays([]float64{1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 0, 0, 0, 0, 0, 0, 0}),
			wantOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := insights.CostTrend(tc.daily)
			if ok != tc.wantOk {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if got.Severity != tc.wantSev {
				t.Errorf("severity=%v, want %v", got.Severity, tc.wantSev)
			}
			if tc.wantTitleRE != "" && !strings.Contains(got.Title, tc.wantTitleRE) {
				t.Errorf("title=%q does not contain %q", got.Title, tc.wantTitleRE)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SessionEfficiency
// ---------------------------------------------------------------------------

type sessionSpec struct {
	durationSec int64
	cost        float64
	tokens      int64
}

func makeSessionsWithDuration(specs []sessionSpec) []store.SessionAgg {
	out := make([]store.SessionAgg, len(specs))
	baseTime := time.Unix(1_700_000_000, 0)
	for i, sp := range specs {
		startT := baseTime.Add(time.Duration(i) * 10000 * time.Second)
		endT := startT.Add(time.Duration(sp.durationSec) * time.Second)
		out[i] = store.SessionAgg{
			CostEUR:   sp.cost,
			InTokens:  sp.tokens,
			FirstSeen: startT,
			LastSeen:  endT,
		}
	}
	return out
}

func TestSessionEfficiency(t *testing.T) {
	tests := []struct {
		name        string
		sessions    []store.SessionAgg
		wantOk      bool
		wantSev     insights.Severity
		wantTitleRE string
	}{
		{
			name:     "fewer than 5 sessions skips",
			sessions: make([]store.SessionAgg, 4),
			wantOk:   false,
		},
		{
			name: "short sessions 3x more expensive gives Tip",
			sessions: makeSessionsWithDuration([]sessionSpec{
				// 3 short sessions: <10min, 1M tokens, €3 each => €3/Mtok
				{durationSec: 300, cost: 3.0, tokens: 1_000_000},
				{durationSec: 300, cost: 3.0, tokens: 1_000_000},
				{durationSec: 300, cost: 3.0, tokens: 1_000_000},
				// 2 long sessions: >1h, 1M tokens, €1 each => €1/Mtok
				{durationSec: 7200, cost: 1.0, tokens: 1_000_000},
				{durationSec: 7200, cost: 1.0, tokens: 1_000_000},
			}),
			wantOk:      true,
			wantSev:     insights.Tip,
			wantTitleRE: "Short sessions cost",
		},
		{
			name: "equal cost per token gives no insight",
			sessions: makeSessionsWithDuration([]sessionSpec{
				{durationSec: 300, cost: 1.0, tokens: 1_000_000},
				{durationSec: 300, cost: 1.0, tokens: 1_000_000},
				{durationSec: 300, cost: 1.0, tokens: 1_000_000},
				{durationSec: 7200, cost: 1.0, tokens: 1_000_000},
				{durationSec: 7200, cost: 1.0, tokens: 1_000_000},
			}),
			wantOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := insights.SessionEfficiency(tc.sessions)
			if ok != tc.wantOk {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if got.Severity != tc.wantSev {
				t.Errorf("severity=%v, want %v", got.Severity, tc.wantSev)
			}
			if tc.wantTitleRE != "" && !strings.Contains(got.Title, tc.wantTitleRE) {
				t.Errorf("title=%q does not contain %q", got.Title, tc.wantTitleRE)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PeakHours
// ---------------------------------------------------------------------------

func TestPeakHours(t *testing.T) {
	tests := []struct {
		name        string
		hourly      []store.HourlyAgg
		wantOk      bool
		wantSev     insights.Severity
		wantTitleRE string
	}{
		{
			name:   "empty skips",
			hourly: []store.HourlyAgg{},
			wantOk: false,
		},
		{
			name: "nil skips",
			hourly: nil,
			wantOk: false,
		},
		{
			name: "multiple hours gives Info with peak hours label",
			hourly: []store.HourlyAgg{
				{Hour: 9, CostEUR: 5.0, Events: 10},
				{Hour: 14, CostEUR: 3.0, Events: 6},
				{Hour: 10, CostEUR: 2.0, Events: 4},
				{Hour: 22, CostEUR: 0.5, Events: 2},
			},
			wantOk:      true,
			wantSev:     insights.Info,
			wantTitleRE: "Peak hours",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := insights.PeakHours(tc.hourly)
			if ok != tc.wantOk {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if got.Severity != tc.wantSev {
				t.Errorf("severity=%v, want %v", got.Severity, tc.wantSev)
			}
			if tc.wantTitleRE != "" && !strings.Contains(got.Title, tc.wantTitleRE) {
				t.Errorf("title=%q does not contain %q", got.Title, tc.wantTitleRE)
			}
		})
	}
}
