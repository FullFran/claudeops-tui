package insights

import (
	"fmt"
	"sort"

	"github.com/fullfran/claudeops-tui/internal/store"
)

// CacheEfficiency checks how well Claude Code reuses cached context.
// Returns false when there are no tokens to analyse.
func CacheEfficiency(agg store.Aggregates) (Insight, bool) {
	denom := float64(agg.CacheReadTokens + agg.InTokens + agg.OutTokens)
	if denom == 0 {
		return Insight{}, false
	}
	ratio := float64(agg.CacheReadTokens) / denom * 100

	ins := Insight{ID: "cache-efficiency"}
	switch {
	case ratio < 20:
		ins.Severity = Warn
		ins.Title = fmt.Sprintf("Low cache efficiency (%.0f%%)", ratio)
		ins.Detail = fmt.Sprintf("Cache read tokens represent only %.0f%% of total token usage.", ratio)
		ins.Recommendation = "Your sessions rebuild context frequently. Keep sessions longer or use prompt caching."
	case ratio < 40:
		ins.Severity = Tip
		ins.Title = fmt.Sprintf("Moderate cache efficiency (%.0f%%)", ratio)
		ins.Detail = fmt.Sprintf("Cache read tokens represent %.0f%% of total token usage.", ratio)
		ins.Recommendation = "Consider longer sessions or smaller context to improve cache reuse."
	default:
		ins.Severity = Info
		ins.Title = fmt.Sprintf("Good cache efficiency (%.0f%%)", ratio)
		ins.Detail = fmt.Sprintf("Cache read tokens represent %.0f%% of total token usage.", ratio)
		ins.Recommendation = "Your cache usage is healthy."
	}
	return ins, true
}

// ModelMix checks whether spend is concentrated on a single model.
// Returns false when fewer than 2 models are present or total cost is zero.
func ModelMix(models []store.ModelAgg) (Insight, bool) {
	if len(models) < 2 {
		return Insight{}, false
	}
	var total float64
	for _, m := range models {
		total += m.CostEUR
	}
	if total == 0 {
		return Insight{}, false
	}
	// models is assumed to be sorted DESC by cost (matches store query ordering).
	top := models[0]
	topPct := top.CostEUR / total * 100

	ins := Insight{ID: "model-mix"}
	if topPct > 70 {
		ins.Severity = Tip
		ins.Title = fmt.Sprintf("%.0f%% of spend on %s", topPct, top.Model)
		ins.Detail = fmt.Sprintf("Model %s accounts for %.0f%% of total cost (€%.4f of €%.4f).",
			top.Model, topPct, top.CostEUR, total)
		ins.Recommendation = "Consider routing routine tasks to a cheaper model."
	} else {
		ins.Severity = Info
		ins.Title = "Balanced model mix"
		ins.Detail = fmt.Sprintf("Top model: %s at %.0f%% of spend.", top.Model, topPct)
		ins.Recommendation = ""
	}
	return ins, true
}

// CostTrend compares average daily spend this week vs last week.
// Returns false when fewer than 14 data points are available or last week avg is zero.
func CostTrend(daily []store.DailyAgg) (Insight, bool) {
	if len(daily) < 14 {
		return Insight{}, false
	}
	// daily[0] is the most recent day (newest first expected by callers).
	var thisWeekSum, lastWeekSum float64
	for _, d := range daily[0:7] {
		thisWeekSum += d.CostEUR
	}
	for _, d := range daily[7:14] {
		lastWeekSum += d.CostEUR
	}
	thisWeekAvg := thisWeekSum / 7
	lastWeekAvg := lastWeekSum / 7
	if lastWeekAvg == 0 {
		return Insight{}, false
	}
	change := (thisWeekAvg - lastWeekAvg) / lastWeekAvg * 100

	ins := Insight{ID: "cost-trend"}
	detail := fmt.Sprintf("Daily avg: €%.4f this week vs €%.4f last week.", thisWeekAvg, lastWeekAvg)
	switch {
	case change > 50:
		ins.Severity = Warn
		ins.Title = fmt.Sprintf("Cost up %.0f%% vs last week", change)
		ins.Detail = detail
		ins.Recommendation = "Review recent sessions for unexpected cost spikes."
	case change > 0:
		ins.Severity = Info
		ins.Title = fmt.Sprintf("Cost up %.0f%% vs last week", change)
		ins.Detail = detail
		ins.Recommendation = ""
	default:
		ins.Severity = Info
		ins.Title = fmt.Sprintf("Cost down %.0f%% vs last week", -change)
		ins.Detail = detail
		ins.Recommendation = ""
	}
	return ins, true
}

// SessionEfficiency compares the cost-per-token ratio across short vs long sessions.
// Returns false when fewer than 5 usable sessions are present or no significant
// difference is detected.
func SessionEfficiency(sessions []store.SessionAgg) (Insight, bool) {
	if len(sessions) < 5 {
		return Insight{}, false
	}

	type bin struct {
		totalCost   float64
		totalTokens int64
		count       int
	}
	var shortBin, longBin bin

	for _, s := range sessions {
		dur := s.LastSeen.Sub(s.FirstSeen)
		tokens := s.InTokens + s.OutTokens + s.CacheReadTokens
		if tokens == 0 || dur == 0 {
			continue
		}
		switch {
		case dur.Minutes() < 10:
			shortBin.totalCost += s.CostEUR
			shortBin.totalTokens += tokens
			shortBin.count++
		case dur.Hours() > 1:
			longBin.totalCost += s.CostEUR
			longBin.totalTokens += tokens
			longBin.count++
		}
	}

	if shortBin.count < 2 || longBin.totalTokens == 0 || shortBin.totalTokens == 0 {
		return Insight{}, false
	}

	shortCostPerMtok := shortBin.totalCost / float64(shortBin.totalTokens) * 1_000_000
	longCostPerMtok := longBin.totalCost / float64(longBin.totalTokens) * 1_000_000

	if longCostPerMtok == 0 || shortCostPerMtok <= 2*longCostPerMtok {
		return Insight{}, false
	}

	ratio := shortCostPerMtok / longCostPerMtok
	return Insight{
		ID:             "session-efficiency",
		Severity:       Tip,
		Title:          fmt.Sprintf("Short sessions cost %.1fx more per token", ratio),
		Detail:         fmt.Sprintf("Short sessions: €%.4f/Mtok | Long sessions: €%.4f/Mtok.", shortCostPerMtok, longCostPerMtok),
		Recommendation: "Longer sessions reuse cached context. Try batching related tasks.",
	}, true
}

// PeakHours identifies the hours with the highest spend.
// Returns false when no hourly data is available.
func PeakHours(hourly []store.HourlyAgg) (Insight, bool) {
	if len(hourly) == 0 {
		return Insight{}, false
	}

	// Sort a copy by cost DESC.
	sorted := make([]store.HourlyAgg, len(hourly))
	copy(sorted, hourly)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CostEUR > sorted[j].CostEUR
	})

	top := sorted
	if len(top) > 3 {
		top = top[:3]
	}

	var totalCost float64
	for _, h := range hourly {
		totalCost += h.CostEUR
	}
	var topCost float64
	for _, h := range top {
		topCost += h.CostEUR
	}

	labels := make([]string, len(top))
	for i, h := range top {
		labels[i] = fmt.Sprintf("%02d:00", h.Hour)
	}

	title := "Peak hours: "
	for i, l := range labels {
		if i > 0 {
			title += ", "
		}
		title += l
	}

	var pct float64
	if totalCost > 0 {
		pct = topCost / totalCost * 100
	}
	detail := fmt.Sprintf("%.0f%% of total cost happens in these hours.", pct)

	return Insight{
		ID:             "peak-hours",
		Severity:       Info,
		Title:          title,
		Detail:         detail,
		Recommendation: "",
	}, true
}
