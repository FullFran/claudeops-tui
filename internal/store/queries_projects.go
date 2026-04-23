package store

import (
	"context"
	"time"
)

// ProjectPeriodAgg summarizes activity for one project over a time window.
type ProjectPeriodAgg struct {
	ProjectName       string
	CostEUR           float64
	InTokens          int64
	OutTokens         int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	Sessions          int64
}

// AggregatesByProjectBetween returns per-project totals for events with
// from <= ts < to. Results are ordered by cost descending.
func (s *Store) AggregatesByProjectBetween(ctx context.Context, from, to time.Time) ([]ProjectPeriodAgg, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name,
		       COALESCE(SUM(e.cost_eur), 0),
		       COALESCE(SUM(e.in_tokens), 0),
		       COALESCE(SUM(e.out_tokens), 0),
		       COALESCE(SUM(e.cache_read_tokens), 0),
		       COALESCE(SUM(e.cache_create_tokens), 0),
		       COUNT(DISTINCT e.session_id)
		  FROM events e
		  JOIN sessions s2 ON s2.id = e.session_id
		  JOIN projects p  ON p.id  = s2.project_id
		 WHERE e.ts >= ? AND e.ts < ?
		 GROUP BY p.name
		 ORDER BY 2 DESC`,
		from.UTC().Format(time.RFC3339Nano),
		to.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ProjectPeriodAgg
	for rows.Next() {
		var a ProjectPeriodAgg
		if err := rows.Scan(&a.ProjectName, &a.CostEUR, &a.InTokens, &a.OutTokens,
			&a.CacheReadTokens, &a.CacheCreateTokens, &a.Sessions); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
