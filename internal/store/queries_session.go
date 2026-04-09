package store

import (
	"context"
)

// ModelsForSession returns per-model aggregates for a specific session.
func (s *Store) ModelsForSession(ctx context.Context, sessionID string) ([]ModelAgg, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(model, '(none)'),
		       COUNT(*),
		       COALESCE(SUM(in_tokens), 0),
		       COALESCE(SUM(out_tokens), 0),
		       COALESCE(SUM(cache_read_tokens), 0),
		       COALESCE(SUM(cache_create_tokens), 0),
		       COALESCE(SUM(cost_eur), 0)
		FROM events
		WHERE session_id = ?
		GROUP BY COALESCE(model, '(none)')
		ORDER BY COALESCE(SUM(cost_eur), 0) DESC`,
		sessionID)
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

// HourlyForSession returns per-hour aggregates for a specific session.
// Returns only hours with activity, ordered ascending.
func (s *Store) HourlyForSession(ctx context.Context, sessionID string) ([]HourlyAgg, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT CAST(strftime('%H', ts, 'localtime') AS INTEGER) AS hour,
		       COALESCE(SUM(cost_eur), 0) AS cost,
		       COUNT(*) AS events
		FROM events
		WHERE session_id = ?
		GROUP BY hour
		ORDER BY hour ASC`,
		sessionID)
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
