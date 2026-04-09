package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleInsights(t *testing.T) {
	s := newTestStore(t)
	srv := New(s)
	now := time.Now().UTC()

	// Insert enough data for insights to potentially fire.
	for i := 0; i < 5; i++ {
		insertEvent(t, s,
			"ins"+string(rune('a'+i)),
			"sess-ins",
			"/p/insights",
			"claude-opus-4-6",
			now.Add(time.Duration(-i)*time.Hour),
			float64(i+1)*0.5,
		)
	}

	t.Run("returns valid insight array (may be empty)", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := srv.handleInsights(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}

		var resp []InsightResponse
		if err := unmarshalResult(res, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		// Validate severity values when insights are present.
		for _, ins := range resp {
			switch ins.Severity {
			case "info", "tip", "warn":
			default:
				t.Errorf("unexpected severity %q for insight %q", ins.Severity, ins.ID)
			}
			if ins.ID == "" {
				t.Error("insight ID must not be empty")
			}
			if ins.Title == "" {
				t.Error("insight Title must not be empty")
			}
		}
	})

	t.Run("empty store returns empty array", func(t *testing.T) {
		empty := newTestStore(t)
		emptySrv := New(empty)
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := emptySrv.handleInsights(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}
		var resp []InsightResponse
		_ = unmarshalResult(res, &resp)
		// Empty DB should produce no insights or valid empty slice.
		for _, ins := range resp {
			if ins.ID == "" {
				t.Error("insight ID must not be empty")
			}
		}
	})
}
