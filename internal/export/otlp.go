package export

// AggregationTemporalityDelta is the OTLP delta temporality value (proto3 enum 2).
const AggregationTemporalityDelta = 2

type exportRequest struct {
	ResourceMetrics []ResourceMetric `json:"resourceMetrics"`
}

type ResourceMetric struct {
	Resource     Resource      `json:"resource"`
	ScopeMetrics []ScopeMetric `json:"scopeMetrics"`
}

// Resource describes the entity producing telemetry.
type Resource struct {
	Attributes []KeyValue `json:"attributes"`
}

// ScopeMetric groups metrics by instrumentation scope.
type ScopeMetric struct {
	Scope   InstrumentationScope `json:"scope"`
	Metrics []Metric             `json:"metrics"`
}

// InstrumentationScope identifies the library emitting metrics.
type InstrumentationScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Metric is a single named metric with its data points.
type Metric struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Unit        string `json:"unit,omitempty"`
	Sum         *Sum   `json:"sum,omitempty"`
}

// Sum holds data points for a cumulative or delta sum metric.
type Sum struct {
	DataPoints             []NumberDataPoint `json:"dataPoints"`
	AggregationTemporality int               `json:"aggregationTemporality"`
	IsMonotonic            bool              `json:"isMonotonic"`
}

// NumberDataPoint is a single observation for a numeric metric.
// StartTimeUnixNano and TimeUnixNano are decimal strings per proto3 uint64 JSON mapping.
type NumberDataPoint struct {
	Attributes        []KeyValue `json:"attributes,omitempty"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	TimeUnixNano      string     `json:"timeUnixNano"`
	AsDouble          *float64   `json:"asDouble,omitempty"`
	AsInt             *int64     `json:"asInt,omitempty"`
}

// KeyValue is an OTLP attribute key-value pair.
type KeyValue struct {
	Key   string   `json:"key"`
	Value AnyValue `json:"value"`
}

// AnyValue wraps one of several primitive types.
type AnyValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *int64   `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

func strAttr(k, v string) KeyValue {
	return KeyValue{Key: k, Value: AnyValue{StringValue: &v}}
}
