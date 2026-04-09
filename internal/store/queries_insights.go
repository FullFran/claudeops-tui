package store

import (
	"context"
	"database/sql"
	"time"
)

// GlobalHourlyAggregates returns per-hour aggregates across all days.
// When since is the zero time, all events are included.
// Results are ordered by hour ASC (0-23, only hours with activity).
func (s *Store) GlobalHourlyAggregates(ctx context.Context, since time.Time) ([]HourlyAgg, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if since.IsZero() {
		rows, err = s.db.QueryContext(ctx, `
			SELECT CAST(strftime('%H', ts, 'localtime') AS INTEGER) AS hour,
			       COALESCE(SUM(cost_eur), 0) AS cost,
			       COUNT(*) AS events
			FROM events
			GROUP BY hour
			ORDER BY hour ASC`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT CAST(strftime('%H', ts, 'localtime') AS INTEGER) AS hour,
			       COALESCE(SUM(cost_eur), 0) AS cost,
			       COUNT(*) AS events
			FROM events
			WHERE ts >= ?
			GROUP BY hour
			ORDER BY hour ASC`,
			since.UTC().Format(time.RFC3339Nano))
	}
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
