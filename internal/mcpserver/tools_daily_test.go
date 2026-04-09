package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleDaily(t *testing.T) {
	s := newTestStore(t)
	srv := New(s)
	now := time.Now()

	insertEvent(t, s, "day1", "sess-1", "/p/a", "model", now, 2.0)

	t.Run("returns N days with contiguous dates", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"days": float64(7)}

		res, err := srv.handleDaily(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}

		var resp []DailyResponse
		if err := unmarshalResult(res, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp) != 7 {
			t.Fatalf("want 7 daily rows, got %d", len(resp))
		}
		// Last row should be today and have cost 2.0
		last := resp[len(resp)-1]
		if last.CostEUR < 1.99 || last.CostEUR > 2.01 {
			t.Errorf("today cost want ~2.0, got %v", last.CostEUR)
		}
		// Date format must be YYYY-MM-DD
		if len(last.Date) != 10 {
			t.Errorf("date format wrong: %q", last.Date)
		}
	})

	t.Run("days clamps to 1 minimum", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"days": float64(-5)}

		res, err := srv.handleDaily(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}
		var resp []DailyResponse
		_ = unmarshalResult(res, &resp)
		if len(resp) != 1 {
			t.Errorf("want 1 row when clamped to minimum, got %d", len(resp))
		}
	})

	t.Run("days clamps to 90 maximum", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"days": float64(200)}

		res, err := srv.handleDaily(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}
		var resp []DailyResponse
		_ = unmarshalResult(res, &resp)
		if len(resp) != 90 {
			t.Errorf("want 90 rows when clamped to max, got %d", len(resp))
		}
	})
}
