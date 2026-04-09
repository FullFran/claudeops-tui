package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleSessions(t *testing.T) {
	s := newTestStore(t)
	srv := New(s)
	now := time.Now().UTC()

	insertEvent(t, s, "s1", "sess-a", "/p/alpha", "claude-opus-4-6", now, 3.0)
	insertEvent(t, s, "s2", "sess-b", "/p/beta", "claude-opus-4-6", now, 1.0)

	t.Run("returns sessions ordered by cost", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := srv.handleSessions(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}

		var resp []SessionResponse
		if err := unmarshalResult(res, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp) != 2 {
			t.Fatalf("want 2 sessions, got %d", len(resp))
		}
		if resp[0].CostEUR < resp[1].CostEUR {
			t.Error("sessions should be ordered by cost desc")
		}
	})

	t.Run("limit clamps to 1", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"limit": float64(1)}

		res, err := srv.handleSessions(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		var resp []SessionResponse
		_ = unmarshalResult(res, &resp)
		if len(resp) != 1 {
			t.Errorf("want 1 session with limit=1, got %d", len(resp))
		}
	})

	t.Run("empty store returns empty array", func(t *testing.T) {
		empty := newTestStore(t)
		emptySrv := New(empty)
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := emptySrv.handleSessions(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}
		var resp []SessionResponse
		_ = unmarshalResult(res, &resp)
		if len(resp) != 0 {
			t.Errorf("want 0 sessions, got %d", len(resp))
		}
	})
}

func TestHandleSessionDetail(t *testing.T) {
	s := newTestStore(t)
	srv := New(s)
	now := time.Now().UTC()

	insertEvent(t, s, "d1", "sess-detail", "/p/x", "claude-opus-4-6", now, 2.0)
	insertEvent(t, s, "d2", "sess-detail", "/p/x", "claude-sonnet-4-6", now.Add(time.Hour), 1.0)

	t.Run("found returns detail with models and hourly", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"session_id": "sess-detail"}

		res, err := srv.handleSessionDetail(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %v", res.Content)
		}

		var resp SessionDetailResponse
		if err := unmarshalResult(res, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Session.SessionID != "sess-detail" {
			t.Errorf("want session_id 'sess-detail', got %q", resp.Session.SessionID)
		}
		if resp.Session.Events != 2 {
			t.Errorf("want 2 events, got %d", resp.Session.Events)
		}
		if len(resp.Models) == 0 {
			t.Error("expected at least one model")
		}
		if resp.Session.DurationSec <= 0 {
			t.Errorf("expected positive duration, got %v", resp.Session.DurationSec)
		}
	})

	t.Run("missing session_id returns tool error", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}

		res, err := srv.handleSessionDetail(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !res.IsError {
			t.Error("expected tool error for missing session_id")
		}
	})

	t.Run("not found returns tool error", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"session_id": "no-such-session"}

		res, err := srv.handleSessionDetail(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !res.IsError {
			t.Error("expected tool error for non-existent session")
		}
	})
}
