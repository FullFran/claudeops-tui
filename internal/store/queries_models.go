package store

import (
	"context"
	"time"
)

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
	return out, rows.Err()
}
