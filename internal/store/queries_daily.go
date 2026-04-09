package store

import (
	"context"
	"time"
)

// HourlyAgg is a per-hour rollup for a single day.
type HourlyAgg struct {
	Hour    int // 0-23
	CostEUR float64
	Events  int64
}

// SessionsForDay returns sessions active on a specific local-time day,
// ordered by cost descending.
func (s *Store) SessionsForDay(ctx context.Context, day time.Time) ([]SessionAgg, error) {
	dayStr := day.Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.session_id, p.name,
		       COALESCE(SUM(e.cost_eur), 0) AS c,
		       COUNT(e.uuid) AS events,
		       COALESCE(SUM(e.in_tokens), 0),
		       COALESCE(SUM(e.out_tokens), 0),
		       COALESCE(SUM(e.cache_read_tokens), 0),
		       COALESCE(SUM(e.cache_create_tokens), 0),
		       MIN(e.ts),
		       MAX(e.ts)
		FROM events e
		JOIN sessions s ON s.id = e.session_id
		JOIN projects p ON p.id = s.project_id
		WHERE date(e.ts, 'localtime') = ?
		GROUP BY e.session_id, p.name
		ORDER BY c DESC`,
		dayStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionAgg
	for rows.Next() {
		var sa SessionAgg
		var firstStr, lastStr string
		if err := rows.Scan(&sa.SessionID, &sa.ProjectName, &sa.CostEUR,
			&sa.Events, &sa.InTokens, &sa.OutTokens, &sa.CacheReadTokens, &sa.CacheCreateTokens,
			&firstStr, &lastStr); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, firstStr); err == nil {
			sa.FirstSeen = t
		}
		if t, err := time.Parse(time.RFC3339Nano, lastStr); err == nil {
			sa.LastSeen = t
		}
		out = append(out, sa)
	}
	return out, rows.Err()
}

// ModelsForDay returns per-model aggregates for a specific local-time day.
func (s *Store) ModelsForDay(ctx context.Context, day time.Time) ([]ModelAgg, error) {
	dayStr := day.Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(model, '(none)'),
		       COUNT(*),
		       COALESCE(SUM(in_tokens), 0),
		       COALESCE(SUM(out_tokens), 0),
		       COALESCE(SUM(cache_read_tokens), 0),
		       COALESCE(SUM(cache_create_tokens), 0),
		       COALESCE(SUM(cost_eur), 0)
		FROM events
		WHERE date(ts, 'localtime') = ?
		GROUP BY COALESCE(model, '(none)')
		ORDER BY COALESCE(SUM(cost_eur), 0) DESC`,
		dayStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelAgg
	for rows.Next() {
		var ma ModelAgg
		if err := rows.Scan(&ma.Model, &ma.Events, &ma.InTokens, &ma.OutTokens,
			&ma.CacheReadTokens, &ma.CacheCreateTokens, &ma.CostEUR); err != nil {
			return nil, err
		}
		out = append(out, ma)
	}
	return out, rows.Err()
}

// HourlyForDay returns per-hour aggregates for a specific local-time day.
// Returns up to 24 entries (only hours with activity).
func (s *Store) HourlyForDay(ctx context.Context, day time.Time) ([]HourlyAgg, error) {
	dayStr := day.Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `
		SELECT CAST(strftime('%H', ts, 'localtime') AS INTEGER) AS hour,
		       COALESCE(SUM(cost_eur), 0) AS cost,
		       COUNT(*) AS events
		FROM events
		WHERE date(ts, 'localtime') = ?
		GROUP BY hour
		ORDER BY hour ASC`,
		dayStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HourlyAgg
	for rows.Next() {
		var ha HourlyAgg
		if err := rows.Scan(&ha.Hour, &ha.CostEUR, &ha.Events); err != nil {
			return nil, err
		}
		out = append(out, ha)
	}
	return out, rows.Err()
}
