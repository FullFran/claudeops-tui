package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) handleSessions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := clamp(req.GetInt("limit", 20), 1, 100)

	sessions, err := s.store.TopSessionsByCost(ctx, limit, time.Time{})
	if err != nil {
		return mcp.NewToolResultError("query failed: " + err.Error()), nil
	}

	resp := make([]SessionResponse, 0, len(sessions))
	for _, sa := range sessions {
		resp = append(resp, sessionToResponse(sa))
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError("marshal failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func (s *Server) handleSessionDetail(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID, err := req.RequireString("session_id")
	if err != nil {
		return mcp.NewToolResultError("session_id is required"), nil
	}

	sa, err := s.store.SessionAggByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return mcp.NewToolResultError("session not found: " + sessionID), nil
		}
		return mcp.NewToolResultError("query failed: " + err.Error()), nil
	}

	models, err := s.store.ModelsForSession(ctx, sessionID)
	if err != nil {
		return mcp.NewToolResultError("models query failed: " + err.Error()), nil
	}

	hourly, err := s.store.HourlyForSession(ctx, sessionID)
	if err != nil {
		return mcp.NewToolResultError("hourly query failed: " + err.Error()), nil
	}

	modelResps := make([]ModelResponse, 0, len(models))
	for _, m := range models {
		modelResps = append(modelResps, modelToResponse(m))
	}

	hourlyResps := make([]HourlyResponse, 0, len(hourly))
	for _, h := range hourly {
		hourlyResps = append(hourlyResps, HourlyResponse{
			Hour:    h.Hour,
			CostEUR: h.CostEUR,
			Events:  h.Events,
		})
	}

	resp := SessionDetailResponse{
		Session: sessionToResponse(sa),
		Models:  modelResps,
		Hourly:  hourlyResps,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError("marshal failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// sessionToResponse converts a store.SessionAgg to a SessionResponse.
func sessionToResponse(sa store.SessionAgg) SessionResponse {
	dur := sa.LastSeen.Sub(sa.FirstSeen).Seconds()
	return SessionResponse{
		SessionID:   sa.SessionID,
		Project:     sa.ProjectName,
		CostEUR:     sa.CostEUR,
		Events:      sa.Events,
		FirstSeen:   formatTime(sa.FirstSeen),
		LastSeen:    formatTime(sa.LastSeen),
		DurationSec: dur,
	}
}
