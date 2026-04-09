// Package insights derives human-readable observations from aggregated usage data.
package insights

import "github.com/fullfran/claudeops-tui/internal/store"

// Severity indicates how important an insight is.
type Severity int

const (
	Info Severity = iota
	Tip
	Warn
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "INFO"
	case Tip:
		return "TIP"
	case Warn:
		return "WARN"
	}
	return "?"
}

// Insight is a single human-readable observation with a recommended action.
type Insight struct {
	ID             string
	Severity       Severity
	Title          string
	Detail         string
	Recommendation string
}

// Input bundles all the data sources required to compute insights.
type Input struct {
	Last7d       store.Aggregates
	AllTime      store.Aggregates
	PerModel     []store.ModelAgg
	Daily        []store.DailyAgg
	Sessions     []store.SessionAgg
	HourlyGlobal []store.HourlyAgg
}

// Compute runs all insight generators and returns those that produced a result.
func Compute(in Input) []Insight {
	var out []Insight
	if ins, ok := CacheEfficiency(in.Last7d); ok {
		out = append(out, ins)
	}
	if ins, ok := ModelMix(in.PerModel); ok {
		out = append(out, ins)
	}
	if ins, ok := CostTrend(in.Daily); ok {
		out = append(out, ins)
	}
	if ins, ok := SessionEfficiency(in.Sessions); ok {
		out = append(out, ins)
	}
	if ins, ok := PeakHours(in.HourlyGlobal); ok {
		out = append(out, ins)
	}
	return out
}
