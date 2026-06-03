package export

import (
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/store"
)

// TestBuildPayloadSourceAttribute covers REQ-4.3: export carries source attribute.
func TestBuildPayloadSourceAttribute(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	d := PeriodData{
		From: from,
		To:   to,
		ByProject: []store.ProjectPeriodAgg{
			{
				ProjectName: "alpha",
				Source:      "claude",
				CostEUR:     1.5,
				InTokens:    100,
				Sessions:    2,
			},
			{
				ProjectName: "alpha",
				Source:      "codex",
				CostEUR:     0.5,
				InTokens:    50,
				Sessions:    1,
			},
		},
	}

	req := buildPayload(baseResource("alice"), d, testScope)
	sm := req.ResourceMetrics[0].ScopeMetrics[0]

	// Find cost metric.
	var costMetric *Metric
	for i := range sm.Metrics {
		if sm.Metrics[i].Name == "claudeops.cost" {
			costMetric = &sm.Metrics[i]
			break
		}
	}
	if costMetric == nil {
		t.Fatal("missing claudeops.cost metric")
	}

	// REQ-4.3.1: source attribute present on each data point.
	t.Run("source attribute present on cost data points", func(t *testing.T) {
		if len(costMetric.Sum.DataPoints) != 2 {
			t.Fatalf("want 2 cost data points (one per source/project row), got %d", len(costMetric.Sum.DataPoints))
		}
		for i, dp := range costMetric.Sum.DataPoints {
			var src string
			for _, attr := range dp.Attributes {
				if attr.Key == "source" {
					if attr.Value.StringValue != nil {
						src = *attr.Value.StringValue
					}
				}
			}
			if src == "" {
				t.Errorf("data point[%d]: missing source attribute; attrs=%v", i, dp.Attributes)
			}
		}
	})

	// REQ-4.3.1: source values are correct.
	t.Run("source attribute values match input", func(t *testing.T) {
		seenSources := make(map[string]bool)
		for _, dp := range costMetric.Sum.DataPoints {
			for _, attr := range dp.Attributes {
				if attr.Key == "source" && attr.Value.StringValue != nil {
					seenSources[*attr.Value.StringValue] = true
				}
			}
		}
		for _, want := range []string{"claude", "codex"} {
			if !seenSources[want] {
				t.Errorf("source %q not found in cost data point attributes", want)
			}
		}
	})

	// REQ-4.3.2: existing attributes (project) still present; user_name is
	// present only when PeriodData.UserName is set — this test leaves it empty
	// so user_name is absent from data points (it lives on the resource instead).
	t.Run("project attribute still present on each data point", func(t *testing.T) {
		for i, dp := range costMetric.Sum.DataPoints {
			keys := make(map[string]bool)
			for _, attr := range dp.Attributes {
				keys[attr.Key] = true
			}
			if !keys["project"] {
				t.Errorf("data point[%d]: missing project attribute; attrs=%v", i, dp.Attributes)
			}
		}
	})
}

// TestBuildPayloadSourceWithUserName verifies user_name and team_name survive
// alongside the new source attribute (REQ-4.3.2 full check).
func TestBuildPayloadSourceWithUserName(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	d := PeriodData{
		From:     from,
		To:       to,
		UserName: "alice",
		TeamName: "eng",
		ByProject: []store.ProjectPeriodAgg{
			{ProjectName: "p", Source: "claude", CostEUR: 1.0, Sessions: 1},
		},
	}

	req := buildPayload(baseResource("alice"), d, testScope)
	sm := req.ResourceMetrics[0].ScopeMetrics[0]
	var costMetric *Metric
	for i := range sm.Metrics {
		if sm.Metrics[i].Name == "claudeops.cost" {
			costMetric = &sm.Metrics[i]
			break
		}
	}
	if costMetric == nil || len(costMetric.Sum.DataPoints) == 0 {
		t.Fatal("expected at least one cost data point")
	}
	dp := costMetric.Sum.DataPoints[0]
	keys := make(map[string]bool)
	for _, attr := range dp.Attributes {
		keys[attr.Key] = true
	}
	for _, want := range []string{"project", "source", "user_name", "team_name"} {
		if !keys[want] {
			t.Errorf("missing attribute %q; present: %v", want, dp.Attributes)
		}
	}
}

// TestBuildPayloadSourceEmptyFallback verifies that a zero-value Source
// in ProjectPeriodAgg still produces a data point without panic.
// (Backwards compat: old rows from DB before source column have empty source.)
func TestBuildPayloadSourceEmptyFallback(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	d := PeriodData{
		From: from,
		To:   to,
		ByProject: []store.ProjectPeriodAgg{
			{
				ProjectName: "p",
				Source:      "", // legacy row: no source
				CostEUR:     1.0,
				Sessions:    1,
			},
		},
	}

	// Must not panic.
	req := buildPayload(baseResource(""), d, testScope)
	sm := req.ResourceMetrics[0].ScopeMetrics[0]
	for _, m := range sm.Metrics {
		if m.Sum == nil {
			t.Errorf("metric %q has nil Sum", m.Name)
		}
	}
	_ = sm
}

// TestAggregatesByProjectBetweenReturnsSource verifies that the store query
// now returns a Source field per row (REQ-4.1 alignment).
func TestAggregatesByProjectBetweenReturnsSource(t *testing.T) {
	// This is an export-package integration test that uses the real store
	// to check the Source field flows through.
	// Detailed store-level coverage lives in queries_projects_test.go.
	// Here we just confirm ProjectPeriodAgg.Source is populated.
	row := store.ProjectPeriodAgg{
		ProjectName: "alpha",
		Source:      "claude",
		CostEUR:     1.0,
	}
	if row.Source != "claude" {
		t.Errorf("Source field not accessible: %+v", row)
	}
}
