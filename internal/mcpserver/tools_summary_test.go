package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleSummary(t *testing.T) {
	s := newTestStore(t)
	srv := New(s)
	now := time.Now().UTC()

	insertEvent(t, s, "u1", "s1", "/p/a", "claude-opus-4-6", now, 1.5)
	insertEvent(t, s, "u2", "s1", "/p/a", "claude-opus-4-6", now.Add(-time.Hour), 2.5)

	cases := []struct {
		name    string
		period  string
		wantErr bool
	}{
		{"today", "today", false},
		{"7d", "7d", false},
		{"30d", "30d", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"period": tc.period}

			res, err := srv.handleSummary(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if res.IsError {
				t.Fatalf("tool returned error: %v", res.Content)
			}

			var resp SummaryResponse
			if err := unmarshalResult(res, &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp.Period != tc.period {
				t.Errorf("period: want %q got %q", tc.period, resp.Period)
			}
		})
	}

	t.Run("missing period returns tool error", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := srv.handleSummary(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !res.IsError {
			t.Error("expected tool error for missing period")
		}
	})

	t.Run("invalid period returns tool error", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"period": "1y"}

		res, err := srv.handleSummary(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !res.IsError {
			t.Error("expected tool error for invalid period")
		}
	})
}

// unmarshalResult extracts the first text content from a tool result and unmarshals JSON.
func unmarshalResult(res *mcp.CallToolResult, v any) error {
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return json.Unmarshal([]byte(tc.Text), v)
		}
	}
	return nil
}
