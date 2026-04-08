package store

import (
	"context"
	"fmt"
	"time"
)

// Aggregates summarizes activity over a time window.
type Aggregates struct {
	Events            int64
	InTokens          int64
	OutTokens         int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	CostEUR           float64
}

// SessionAgg is a per-session cost aggregate.
type SessionAgg struct {
	SessionID   string
	ProjectName string
	CostEUR     float64
}

// ProjectAgg is a per-project cost aggregate.
type ProjectAgg struct {
	ProjectName string
	CostEUR     float64
}

// TaskAgg is a per-task aggregate including duration.
type TaskAgg struct {
	ID                string
	Name              string
	StartedAt         time.Time
	EndedAt           *time.Time
	Events            int64
	InTokens          int64
	OutTokens         int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	CostEUR           float64
}

// AggregatesForToday returns totals for events with ts >= start of UTC day.
func (s *Store) AggregatesForToday(ctx context.Context) (Aggregates, error) {
	return s.aggregatesSince(ctx, startOfTodayUTC())
}

// AggregatesSince returns totals for events with ts >= since.
func (s *Store) AggregatesSince(ctx context.Context, since time.Time) (Aggregates, error) {
	return s.aggregatesSince(ctx, since)
}

func (s *Store) aggregatesSince(ctx context.Context, since time.Time) (Aggregates, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT
		    COUNT(*),
		    COALESCE(SUM(in_tokens), 0),
		    COALESCE(SUM(out_tokens), 0),
		    COALESCE(SUM(cache_read_tokens), 0),
		    COALESCE(SUM(cache_create_tokens), 0),
		    COALESCE(SUM(cost_eur), 0)
		 FROM events
		 WHERE ts >= ?`,
		since.UTC().Format(time.RFC3339Nano))
	var a Aggregates
	err := row.Scan(&a.Events, &a.InTokens, &a.OutTokens, &a.CacheReadTokens, &a.CacheCreateTokens, &a.CostEUR)
	return a, err
}

// TopSessionsByCost returns the N highest-cost sessions for events since `since`.
func (s *Store) TopSessionsByCost(ctx context.Context, n int, since time.Time) ([]SessionAgg, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.session_id, p.name, COALESCE(SUM(e.cost_eur), 0) AS c
		 FROM events e
		 JOIN sessions s ON s.id = e.session_id
		 JOIN projects p ON p.id = s.project_id
		 WHERE e.ts >= ?
		 GROUP BY e.session_id, p.name
		 ORDER BY c DESC
		 LIMIT ?`,
		since.UTC().Format(time.RFC3339Nano), n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionAgg
	for rows.Next() {
		var sa SessionAgg
		if err := rows.Scan(&sa.SessionID, &sa.ProjectName, &sa.CostEUR); err != nil {
			return nil, err
		}
		out = append(out, sa)
	}
	return out, rows.Err()
}

// TopProjectsByCost returns the N highest-cost projects for events since `since`.
func (s *Store) TopProjectsByCost(ctx context.Context, n int, since time.Time) ([]ProjectAgg, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.name, COALESCE(SUM(e.cost_eur), 0) AS c
		 FROM events e
		 JOIN sessions s ON s.id = e.session_id
		 JOIN projects p ON p.id = s.project_id
		 WHERE e.ts >= ?
		 GROUP BY p.name
		 ORDER BY c DESC
		 LIMIT ?`,
		since.UTC().Format(time.RFC3339Nano), n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectAgg
	for rows.Next() {
		var pa ProjectAgg
		if err := rows.Scan(&pa.ProjectName, &pa.CostEUR); err != nil {
			return nil, err
		}
		out = append(out, pa)
	}
	return out, rows.Err()
}

// UpsertTask creates or updates a task row.
func (s *Store) UpsertTask(ctx context.Context, id, name string, startedAt time.Time, maxAge time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (id, name, started_at, max_age_seconds)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name`,
		id, name, startedAt.UTC().Format(time.RFC3339Nano), int64(maxAge.Seconds()))
	return err
}

// EndTask marks a task ended.
func (s *Store) EndTask(ctx context.Context, id string, endedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET ended_at = ? WHERE id = ?`,
		endedAt.UTC().Format(time.RFC3339Nano), id)
	return err
}

// TaskAggregates returns per-task totals.
func (s *Store) TaskAggregates(ctx context.Context) ([]TaskAgg, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.started_at, t.ended_at,
		        COUNT(e.uuid),
		        COALESCE(SUM(e.in_tokens), 0),
		        COALESCE(SUM(e.out_tokens), 0),
		        COALESCE(SUM(e.cache_read_tokens), 0),
		        COALESCE(SUM(e.cache_create_tokens), 0),
		        COALESCE(SUM(e.cost_eur), 0)
		 FROM tasks t
		 LEFT JOIN events e ON e.task_id = t.id
		 GROUP BY t.id
		 ORDER BY t.started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskAgg
	for rows.Next() {
		var ta TaskAgg
		var startStr string
		var endStr *string
		if err := rows.Scan(&ta.ID, &ta.Name, &startStr, &endStr,
			&ta.Events, &ta.InTokens, &ta.OutTokens,
			&ta.CacheReadTokens, &ta.CacheCreateTokens, &ta.CostEUR); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, startStr); err == nil {
			ta.StartedAt = t
		}
		if endStr != nil {
			if t, err := time.Parse(time.RFC3339Nano, *endStr); err == nil {
				ta.EndedAt = &t
			}
		}
		out = append(out, ta)
	}
	return out, rows.Err()
}

// DailyAgg is the per-day rollup used by the sparkline, streak counter, and
// (in a follow-up PR) the calendar tab.
type DailyAgg struct {
	Date     time.Time // local midnight of the day
	CostEUR  float64
	Events   int64
	Sessions int64
}

// DailyAggregatesLocal returns one row per day for the last `days` days,
// grouped in the user's LOCAL timezone (so a session that ran at 23:30 local
// on Friday lands on Friday, not Saturday). Days with no activity are
// included with zero values to make sparklines and streak math straightforward.
//
// The result is ordered oldest → newest, has exactly `days` entries, and the
// last entry is always today (in local time).
func (s *Store) DailyAggregatesLocal(ctx context.Context, days int) ([]DailyAgg, error) {
	if days <= 0 {
		return nil, nil
	}
	// SQLite computes date(ts, 'localtime') from the stored RFC3339 string.
	// We aggregate, then merge with a generated date series client-side so
	// empty days appear as zeros.
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(ts, 'localtime')              AS day,
		       COALESCE(SUM(cost_eur), 0)         AS cost,
		       COUNT(*)                           AS events,
		       COUNT(DISTINCT session_id)         AS sessions
		FROM events
		WHERE date(ts, 'localtime') >= date('now', 'localtime', ?)
		GROUP BY day
		ORDER BY day ASC`,
		fmt.Sprintf("-%d days", days-1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDay := make(map[string]DailyAgg, days)
	for rows.Next() {
		var dayStr string
		var d DailyAgg
		if err := rows.Scan(&dayStr, &d.CostEUR, &d.Events, &d.Sessions); err != nil {
			return nil, err
		}
		byDay[dayStr] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Build the contiguous local-day series oldest → newest.
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	out := make([]DailyAgg, 0, days)
	for i := days - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i)
		key := d.Format("2006-01-02")
		da := byDay[key]
		da.Date = d
		out = append(out, da)
	}
	return out, nil
}

func startOfTodayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}
