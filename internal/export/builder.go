package export

import (
	"strconv"
	"time"

	"github.com/fullfran/claudeops-tui/internal/store"
)

// PeriodData is the bag of aggregated data for one push window.
type PeriodData struct {
	From      time.Time
	To        time.Time
	ByProject []store.ProjectPeriodAgg
	// UserName and TeamName are added as data-point attributes so they appear
	// as native Prometheus labels (resource-level attributes are not reliable).
	UserName string
	TeamName string
}

// buildPayload constructs an OTLP exportRequest from aggregated period data.
func buildPayload(resource Resource, d PeriodData, scope InstrumentationScope) exportRequest {
	startNs := strconv.FormatInt(d.From.UnixNano(), 10)
	endNs := strconv.FormatInt(d.To.UnixNano(), 10)

	var costPoints, sessionPoints []NumberDataPoint
	// map from token_type → []NumberDataPoint
	tokenPoints := map[string][]NumberDataPoint{
		"input":          {},
		"output":         {},
		"cache_read":     {},
		"cache_creation": {},
	}

	// Base attributes shared by every data point.
	var baseAttrs []KeyValue
	if d.UserName != "" {
		baseAttrs = append(baseAttrs, strAttr("user_name", d.UserName))
	}
	if d.TeamName != "" {
		baseAttrs = append(baseAttrs, strAttr("team_name", d.TeamName))
	}

	for _, p := range d.ByProject {
		attrs := append(append([]KeyValue{}, baseAttrs...), strAttr("project", p.ProjectName))

		cost := p.CostEUR
		costPoints = append(costPoints, NumberDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: startNs,
			TimeUnixNano:      endNs,
			AsDouble:          &cost,
		})

		sess := p.Sessions
		sessionPoints = append(sessionPoints, NumberDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: startNs,
			TimeUnixNano:      endNs,
			AsInt:             &sess,
		})

		tokenTypes := []struct {
			key string
			val int64
		}{
			{"input", p.InTokens},
			{"output", p.OutTokens},
			{"cache_read", p.CacheReadTokens},
			{"cache_creation", p.CacheCreateTokens},
		}
		for _, tt := range tokenTypes {
			v := tt.val
			tokenPoints[tt.key] = append(tokenPoints[tt.key], NumberDataPoint{
				Attributes:        append(attrs, strAttr("token_type", tt.key)),
				StartTimeUnixNano: startNs,
				TimeUnixNano:      endNs,
				AsInt:             &v,
			})
		}
	}

	// Flatten token points into one slice in canonical order.
	var allTokenPoints []NumberDataPoint
	for _, ttype := range []string{"input", "output", "cache_read", "cache_creation"} {
		allTokenPoints = append(allTokenPoints, tokenPoints[ttype]...)
	}

	metrics := []Metric{
		{
			Name: "claudeops.cost",
			Unit: "{EUR}",
			Sum: &Sum{
				DataPoints:             costPoints,
				AggregationTemporality: AggregationTemporalityCumulative,
				IsMonotonic:            true,
			},
		},
		{
			Name: "claudeops.tokens",
			Unit: "{token}",
			Sum: &Sum{
				DataPoints:             allTokenPoints,
				AggregationTemporality: AggregationTemporalityCumulative,
				IsMonotonic:            true,
			},
		},
		{
			Name: "claudeops.sessions",
			Unit: "{session}",
			Sum: &Sum{
				DataPoints:             sessionPoints,
				AggregationTemporality: AggregationTemporalityCumulative,
				IsMonotonic:            true,
			},
		},
	}

	return exportRequest{
		ResourceMetrics: []ResourceMetric{{
			Resource: resource,
			ScopeMetrics: []ScopeMetric{{
				Scope:   scope,
				Metrics: metrics,
			}},
		}},
	}
}
