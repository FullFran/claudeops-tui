package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleProjects(t *testing.T) {
	s := newTestStore(t)
	srv := New(s)
	now := time.Now().UTC()

	insertEvent(t, s, "p1", "sess-1", "/work/alpha", "model", now, 5.0)
	insertEvent(t, s, "p2", "sess-2", "/work/beta", "model", now, 2.0)
	insertEvent(t, s, "p3", "sess-3", "/work/gamma", "model", now, 8.0)

	t.Run("returns projects ordered by cost desc", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := srv.handleProjects(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}

		var resp []ProjectResponse
		if err := unmarshalResult(res, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp) != 3 {
			t.Fatalf("want 3 projects, got %d", len(resp))
		}
		if resp[0].CostEUR < resp[1].CostEUR {
			t.Error("projects should be ordered by cost desc")
		}
		if resp[0].Project != "gamma" {
			t.Errorf("highest cost project should be gamma, got %q", resp[0].Project)
		}
	})

	t.Run("empty store returns empty array", func(t *testing.T) {
		empty := newTestStore(t)
		emptySrv := New(empty)
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := emptySrv.handleProjects(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}
		var resp []ProjectResponse
		_ = unmarshalResult(res, &resp)
		if len(resp) != 0 {
			t.Errorf("want 0 projects, got %d", len(resp))
		}
	})
}
