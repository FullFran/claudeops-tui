package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleModels(t *testing.T) {
	s := newTestStore(t)
	srv := New(s)
	now := time.Now().UTC()

	insertEvent(t, s, "m1", "sess-1", "/p/a", "claude-opus-4-6", now, 3.0)
	insertEvent(t, s, "m2", "sess-1", "/p/a", "claude-sonnet-4-6", now, 1.0)
	insertEvent(t, s, "m3", "sess-2", "/p/b", "claude-opus-4-6", now, 2.0)

	t.Run("returns per-model aggregates", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := srv.handleModels(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}

		var resp []ModelResponse
		if err := unmarshalResult(res, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp) != 2 {
			t.Fatalf("want 2 models, got %d", len(resp))
		}
		// opus total: 5.0, sonnet: 1.0 — opus should be first
		if resp[0].Model != "claude-opus-4-6" {
			t.Errorf("highest cost model should be opus, got %q", resp[0].Model)
		}
		if resp[0].CacheHitRatio < 0 || resp[0].CacheHitRatio > 1 {
			t.Errorf("cache_hit_ratio out of [0,1]: %v", resp[0].CacheHitRatio)
		}
	})

	t.Run("empty store returns empty array", func(t *testing.T) {
		empty := newTestStore(t)
		emptySrv := New(empty)
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := emptySrv.handleModels(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}
		var resp []ModelResponse
		_ = unmarshalResult(res, &resp)
		if len(resp) != 0 {
			t.Errorf("want 0 models, got %d", len(resp))
		}
	})
}
