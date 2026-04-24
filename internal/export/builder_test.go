package export

import (
	"strconv"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/store"
)

func ptr[T any](v T) *T { return &v }

var testScope = InstrumentationScope{Name: "claudeops", Version: "test"}

func baseResource(userName string) Resource {
	attrs := []KeyValue{strAttr("service.name", "claudeops")}
	if userName != "" {
		attrs = append(attrs, strAttr("user_name", userName))
	}
	return Resource{Attributes: attrs}
}

func TestBuildPayloadEmptyPeriodData(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	d := PeriodData{From: from, To: to}

	req := buildPayload(baseResource(""), d, testScope)

	if len(req.ResourceMetrics) != 1 {
		t.Fatalf("want 1 ResourceMetric, got %d", len(req.ResourceMetrics))
	}
	rm := req.ResourceMetrics[0]
	if len(rm.ScopeMetrics) != 1 {
		t.Fatalf("want 1 ScopeMetric, got %d", len(rm.ScopeMetrics))
	}
	sm := rm.ScopeMetrics[0]
	if len(sm.Metrics) != 3 {
		t.Fatalf("want 3 metrics, got %d", len(sm.Metrics))
	}

	names := make(map[string]bool)
	for _, m := range sm.Metrics {
		names[m.Name] = true
	}
	for _, want := range []string{"claudeops.cost.eur", "claudeops.tokens", "claudeops.sessions"} {
		if !names[want] {
			t.Errorf("missing metric %q", want)
		}
	}

	// With no projects, data points must be empty slices (or nil)
	for _, m := range sm.Metrics {
		if m.Sum == nil {
			t.Errorf("metric %q has nil Sum", m.Name)
			continue
		}
		if len(m.Sum.DataPoints) != 0 {
			t.Errorf("metric %q: want 0 data points for empty period, got %d", m.Name, len(m.Sum.DataPoints))
		}
	}
}

func TestBuildPayloadNonZeroOneProject(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	d := PeriodData{
		From: from,
		To:   to,
		ByProject: []store.ProjectPeriodAgg{
			{
				ProjectName:       "alpha",
				CostEUR:           1.5,
				InTokens:          100,
				OutTokens:         50,
				CacheReadTokens:   200,
				CacheCreateTokens: 300,
				Sessions:          3,
			},
		},
	}

	req := buildPayload(baseResource("alice"), d, testScope)
	sm := req.ResourceMetrics[0].ScopeMetrics[0]

	var costMetric, tokenMetric, sessionMetric *Metric
	for i := range sm.Metrics {
		m := &sm.Metrics[i]
		switch m.Name {
		case "claudeops.cost.eur":
			costMetric = m
		case "claudeops.tokens":
			tokenMetric = m
		case "claudeops.sessions":
			sessionMetric = m
		}
	}

	// Cost metric
	if costMetric == nil {
		t.Fatal("missing claudeops.cost.eur")
	}
	if len(costMetric.Sum.DataPoints) != 1 {
		t.Fatalf("cost data points: want 1 got %d", len(costMetric.Sum.DataPoints))
	}
	dp := costMetric.Sum.DataPoints[0]
	if dp.AsDouble == nil || *dp.AsDouble < 1.49 || *dp.AsDouble > 1.51 {
		t.Errorf("cost AsDouble: want ~1.5 got %v", dp.AsDouble)
	}

	// Token metric
	if tokenMetric == nil {
		t.Fatal("missing claudeops.tokens.total")
	}
	// One project → 4 token type data points
	if len(tokenMetric.Sum.DataPoints) != 4 {
		t.Fatalf("token data points: want 4 got %d", len(tokenMetric.Sum.DataPoints))
	}
	tokenTypeValues := map[string]int64{
		"input":          100,
		"output":         50,
		"cache_read":     200,
		"cache_creation": 300,
	}
	for _, dp := range tokenMetric.Sum.DataPoints {
		var ttype string
		for _, attr := range dp.Attributes {
			if attr.Key == "token_type" {
				ttype = *attr.Value.StringValue
			}
		}
		if ttype == "" {
			t.Error("token data point missing token_type attribute")
			continue
		}
		want, ok := tokenTypeValues[ttype]
		if !ok {
			t.Errorf("unexpected token_type %q", ttype)
			continue
		}
		if dp.AsInt == nil || *dp.AsInt != want {
			t.Errorf("token_type %q: want %d got %v", ttype, want, dp.AsInt)
		}
	}

	// Session metric
	if sessionMetric == nil {
		t.Fatal("missing claudeops.sessions.count")
	}
	if len(sessionMetric.Sum.DataPoints) != 1 {
		t.Fatalf("session data points: want 1 got %d", len(sessionMetric.Sum.DataPoints))
	}
	sdp := sessionMetric.Sum.DataPoints[0]
	if sdp.AsInt == nil || *sdp.AsInt != 3 {
		t.Errorf("sessions AsInt: want 3 got %v", sdp.AsInt)
	}
}

func TestBuildPayloadTimestampsAreDecimalStrings(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	d := PeriodData{
		From: from,
		To:   to,
		ByProject: []store.ProjectPeriodAgg{
			{ProjectName: "p", CostEUR: 1.0, Sessions: 1},
		},
	}

	req := buildPayload(baseResource(""), d, testScope)
	sm := req.ResourceMetrics[0].ScopeMetrics[0]

	for _, m := range sm.Metrics {
		for _, dp := range m.Sum.DataPoints {
			startNs, err := strconv.ParseInt(dp.StartTimeUnixNano, 10, 64)
			if err != nil {
				t.Errorf("metric %q: StartTimeUnixNano %q is not a decimal int64: %v", m.Name, dp.StartTimeUnixNano, err)
			} else if startNs <= 0 {
				t.Errorf("metric %q: StartTimeUnixNano %d should be > 0", m.Name, startNs)
			}
			endNs, err := strconv.ParseInt(dp.TimeUnixNano, 10, 64)
			if err != nil {
				t.Errorf("metric %q: TimeUnixNano %q is not a decimal int64: %v", m.Name, dp.TimeUnixNano, err)
			} else if endNs <= 0 {
				t.Errorf("metric %q: TimeUnixNano %d should be > 0", m.Name, endNs)
			}
		}
	}
}

func TestBuildPayloadDeltaTemporalityAndMonotonic(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	d := PeriodData{From: from, To: to}

	req := buildPayload(baseResource(""), d, testScope)
	sm := req.ResourceMetrics[0].ScopeMetrics[0]

	for _, m := range sm.Metrics {
		if m.Sum == nil {
			t.Errorf("metric %q has nil Sum", m.Name)
			continue
		}
		if m.Sum.AggregationTemporality != AggregationTemporalityDelta {
			t.Errorf("metric %q: AggregationTemporality want %d got %d",
				m.Name, AggregationTemporalityDelta, m.Sum.AggregationTemporality)
		}
		if !m.Sum.IsMonotonic {
			t.Errorf("metric %q: IsMonotonic should be true", m.Name)
		}
	}
}

func TestBuildPayloadUserNameInResourceAttrs(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	d := PeriodData{From: from, To: to}

	t.Run("user_name present when non-empty", func(t *testing.T) {
		req := buildPayload(baseResource("alice"), d, testScope)
		attrs := req.ResourceMetrics[0].Resource.Attributes
		found := false
		for _, a := range attrs {
			if a.Key == "user_name" {
				found = true
				if a.Value.StringValue == nil || *a.Value.StringValue != "alice" {
					t.Errorf("user_name value: want alice got %v", a.Value.StringValue)
				}
			}
		}
		if !found {
			t.Error("user_name attribute absent")
		}
	})

	t.Run("user_name absent when empty", func(t *testing.T) {
		req := buildPayload(baseResource(""), d, testScope)
		attrs := req.ResourceMetrics[0].Resource.Attributes
		for _, a := range attrs {
			if a.Key == "user_name" {
				t.Error("user_name attribute should be absent when empty")
			}
		}
	})
}

func TestBuildPayloadMetricNames(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	d := PeriodData{From: from, To: to}

	req := buildPayload(baseResource(""), d, testScope)
	sm := req.ResourceMetrics[0].ScopeMetrics[0]

	wantNames := []string{
		"claudeops.cost.eur",
		"claudeops.tokens",
		"claudeops.sessions",
	}
	for i, m := range sm.Metrics {
		if i >= len(wantNames) {
			t.Errorf("unexpected extra metric: %q", m.Name)
			continue
		}
		if m.Name != wantNames[i] {
			t.Errorf("metric[%d]: want %q got %q", i, wantNames[i], m.Name)
		}
	}
}

func TestBuildPayloadTokenTypeAttributeValues(t *testing.T) {
	from := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	d := PeriodData{
		From: from,
		To:   to,
		ByProject: []store.ProjectPeriodAgg{
			{ProjectName: "p", CostEUR: 1.0, InTokens: 10, OutTokens: 20, CacheReadTokens: 30, CacheCreateTokens: 40, Sessions: 1},
		},
	}

	req := buildPayload(baseResource(""), d, testScope)
	sm := req.ResourceMetrics[0].ScopeMetrics[0]

	var tokenMetric *Metric
	for i := range sm.Metrics {
		if sm.Metrics[i].Name == "claudeops.tokens" {
			tokenMetric = &sm.Metrics[i]
			break
		}
	}
	if tokenMetric == nil {
		t.Fatal("missing claudeops.tokens.total")
	}

	wantTypes := map[string]bool{
		"input":          false,
		"output":         false,
		"cache_read":     false,
		"cache_creation": false,
	}

	for _, dp := range tokenMetric.Sum.DataPoints {
		for _, attr := range dp.Attributes {
			if attr.Key == "token_type" {
				if attr.Value.StringValue == nil {
					t.Error("token_type value is nil")
					continue
				}
				v := *attr.Value.StringValue
				if _, ok := wantTypes[v]; !ok {
					t.Errorf("unexpected token_type value %q", v)
				} else {
					wantTypes[v] = true
				}
			}
		}
	}

	for ttype, seen := range wantTypes {
		if !seen {
			t.Errorf("token_type %q not found in data points", ttype)
		}
	}
}
