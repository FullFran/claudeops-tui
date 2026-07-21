package store

import (
	"context"
	"time"
)

// SourceAgg is the per-source cost/token aggregate.
type SourceAgg struct {
	Source            string
	Events            int64
	InTokens          int64
	OutTokens         int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	CostEUR           float64
}

// AggregatesBySource returns one row per source for events with ts >= since.
// When since is the zero time all rows are included.
// Results are ordered by total cost descending.
func (s *Store) AggregatesBySource(ctx context.Context, since time.Time) ([]SourceAgg, error) {
	sinceStr := since.UTC().Format(time.RFC3339Nano)
	whereClause := "WHERE ts >= ?"
	if since.IsZero() {
		whereClause = "WHERE 1=1 OR ts >= ?"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT
		    source,
		    COUNT(*),
		    COALESCE(SUM(in_tokens), 0),
		    COALESCE(SUM(out_tokens), 0),
		    COALESCE(SUM(cache_read_tokens), 0),
		    COALESCE(SUM(cache_create_tokens), 0),
		    COALESCE(SUM(cost_eur), 0)
		 FROM events
		 `+whereClause+`
		 GROUP BY source
		 ORDER BY COALESCE(SUM(cost_eur), 0) DESC`,
		sinceStr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SourceAgg
	for rows.Next() {
		var ag SourceAgg
		if err := rows.Scan(&ag.Source, &ag.Events, &ag.InTokens, &ag.OutTokens,
			&ag.CacheReadTokens, &ag.CacheCreateTokens, &ag.CostEUR); err != nil {
			return nil, err
		}
		out = append(out, ag)
	}
	return out, rows.Err()
}
