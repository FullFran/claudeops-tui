package store

import (
	"context"
	"time"
)

// SessionAggByID returns the cost aggregate for a specific session_id.
// Returns sql.ErrNoRows if the session does not exist.
func (s *Store) SessionAggByID(ctx context.Context, sessionID string) (SessionAgg, error) {
	row := s.db.QueryRowContext(ctx, `
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
		WHERE e.session_id = ?
		GROUP BY e.session_id, p.name`,
		sessionID)

	var sa SessionAgg
	var firstStr, lastStr string
	if err := row.Scan(&sa.SessionID, &sa.ProjectName, &sa.CostEUR,
		&sa.Events, &sa.InTokens, &sa.OutTokens, &sa.CacheReadTokens, &sa.CacheCreateTokens,
		&firstStr, &lastStr); err != nil {
		return SessionAgg{}, err
	}
	if t, err := time.Parse(time.RFC3339Nano, firstStr); err == nil {
		sa.FirstSeen = t
	}
	if t, err := time.Parse(time.RFC3339Nano, lastStr); err == nil {
		sa.LastSeen = t
	}
	return sa, nil
}

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
