package store

import (
	"context"
	"sort"
	"time"

	"github.com/fullfran/claudeops-tui/internal/pricing"
)

// noneModel is the label for events that carry no model (user prompts).
const noneModel = "(none)"

// modelAggKey is the grouping key for per-model aggregates. Ids reach the
// events table decorated — "claude-opus-4-8[1m]", "openai/gpt-5", "kimi:cloud"
// — and a plain GROUP BY therefore splits one model across several rows.
//
// It reuses pricing's normalizer instead of duplicating the strippers, but
// deliberately re-attaches the free-tier marker that the normalizer erases:
// pricing collapses "…-free" onto the paid id so a free variant can be looked
// up and billed at zero, whereas an aggregate that merged the two would report
// the paid model's spend against tokens that cost nothing. Grouping happens at
// read time, so the events table keeps the id exactly as the source wrote it
// and no data migration is involved.
func modelAggKey(model string) string {
	if model == "" || model == noneModel {
		return model
	}
	base := pricing.NormalizeModelID(model)
	if pricing.IsFreeModelID(model) {
		return base + "-free"
	}
	return base
}

// mergeModelAggs folds rows whose ids differ only by decoration into one, then
// re-sorts by cost (then events, then name) since merging changes the totals
// the SQL ordering was based on.
func mergeModelAggs(rows []ModelAgg) []ModelAgg {
	byKey := make(map[string]*ModelAgg, len(rows))
	order := make([]string, 0, len(rows))
	for _, r := range rows {
		key := modelAggKey(r.Model)
		agg, ok := byKey[key]
		if !ok {
			r.Model = key
			cp := r
			byKey[key] = &cp
			order = append(order, key)
			continue
		}
		agg.Events += r.Events
		agg.InTokens += r.InTokens
		agg.OutTokens += r.OutTokens
		agg.CacheReadTokens += r.CacheReadTokens
		agg.CacheCreateTokens += r.CacheCreateTokens
		agg.CostEUR += r.CostEUR
	}
	out := make([]ModelAgg, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CostEUR != out[j].CostEUR {
			return out[i].CostEUR > out[j].CostEUR
		}
		if out[i].Events != out[j].Events {
			return out[i].Events > out[j].Events
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// ModelAgg is a per-model aggregate.
type ModelAgg struct {
	Model             string
	Events            int64
	InTokens          int64
	OutTokens         int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	CostEUR           float64
}

// PerModelAggregates groups events by model since `since`. Empty model is
// reported as "(none)" — these are non-assistant events with no cost.
func (s *Store) PerModelAggregates(ctx context.Context, since time.Time) ([]ModelAgg, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT COALESCE(model, '(none)'),
		        COUNT(*),
		        COALESCE(SUM(in_tokens), 0),
		        COALESCE(SUM(out_tokens), 0),
		        COALESCE(SUM(cache_read_tokens), 0),
		        COALESCE(SUM(cache_create_tokens), 0),
		        COALESCE(SUM(cost_eur), 0)
		 FROM events
		 WHERE ts >= ?
		 GROUP BY COALESCE(model, '(none)')
		 ORDER BY 7 DESC, 2 DESC`,
		since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelAgg
	for rows.Next() {
		var m ModelAgg
		if err := rows.Scan(&m.Model, &m.Events, &m.InTokens, &m.OutTokens,
			&m.CacheReadTokens, &m.CacheCreateTokens, &m.CostEUR); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return mergeModelAggs(out), nil
}
