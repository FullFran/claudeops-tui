package store

import (
	"context"
	"testing"
	"time"
)

func TestGlobalHourlyAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert events at different hours over two days.
	now := time.Now()
	baseDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	atHour := func(day time.Time, hour int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), hour, 30, 0, 0, day.Location())
	}

	events := []struct {
		uuid string
		ts   time.Time
		cost float64
	}{
		{"h1", atHour(baseDay, 9), 1.0},
		{"h2", atHour(baseDay, 9), 2.0}, // second event same hour
		{"h3", atHour(baseDay, 14), 3.0},
		{"h4", atHour(baseDay.AddDate(0, 0, -1), 9), 0.5}, // yesterday same hour
	}

	for _, ev := range events {
		e := makeEvent(ev.uuid, "sess-ins-"+ev.uuid, "/p/test", "claude-test", ev.ts)
		c := ev.cost
		if err := s.Insert(ctx, e, &c, nil); err != nil {
			t.Fatalf("Insert %s: %v", ev.uuid, err)
		}
	}

	t.Run("no since filter returns all hours", func(t *testing.T) {
		rows, err := s.GlobalHourlyAggregates(ctx, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatal("expected at least one hourly row")
		}
		// Find hour 9: should have events from both days summed.
		var hour9, hour14 *HourlyAgg
		for i := range rows {
			switch rows[i].Hour {
			case 9:
				hour9 = &rows[i]
			case 14:
				hour14 = &rows[i]
			}
		}
		if hour9 == nil {
			t.Fatal("expected row for hour 9")
		}
		if hour9.Events != 3 {
			t.Errorf("hour 9 events: want 3 got %d", hour9.Events)
		}
		if hour9.CostEUR < 3.49 || hour9.CostEUR > 3.51 {
			t.Errorf("hour 9 cost: want ~3.5 got %.4f", hour9.CostEUR)
		}
		if hour14 == nil {
			t.Fatal("expected row for hour 14")
		}
		if hour14.Events != 1 {
			t.Errorf("hour 14 events: want 1 got %d", hour14.Events)
		}
	})

	t.Run("since filter excludes older events", func(t *testing.T) {
		// Only include today
		sinceToday := time.Date(baseDay.Year(), baseDay.Month(), baseDay.Day(), 0, 0, 0, 0, baseDay.Location())
		rows, err := s.GlobalHourlyAggregates(ctx, sinceToday)
		if err != nil {
			t.Fatal(err)
		}
		// Hour 9 should now only have 2 events (today only, not yesterday).
		var hour9 *HourlyAgg
		for i := range rows {
			if rows[i].Hour == 9 {
				hour9 = &rows[i]
			}
		}
		if hour9 == nil {
			t.Fatal("expected row for hour 9")
		}
		if hour9.Events != 2 {
			t.Errorf("hour 9 events with since filter: want 2 got %d", hour9.Events)
		}
	})

	t.Run("results ordered by hour ASC", func(t *testing.T) {
		rows, err := s.GlobalHourlyAggregates(ctx, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(rows); i++ {
			if rows[i].Hour <= rows[i-1].Hour {
				t.Errorf("not sorted ASC at index %d: %d <= %d", i, rows[i].Hour, rows[i-1].Hour)
			}
		}
	})
}
