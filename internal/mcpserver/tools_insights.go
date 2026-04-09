package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/fullfran/claudeops-tui/internal/insights"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) handleInsights(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	now := time.Now().UTC()
	since7d := now.Add(-7 * 24 * time.Hour)
	since30d := now.Add(-30 * 24 * time.Hour)

	last7d, err := s.store.AggregatesSince(ctx, since7d)
	if err != nil {
		return mcp.NewToolResultError("aggregates query failed: " + err.Error()), nil
	}

	perModel, err := s.store.PerModelAggregates(ctx, time.Time{})
	if err != nil {
		return mcp.NewToolResultError("models query failed: " + err.Error()), nil
	}

	daily, err := s.store.DailyAggregatesLocal(ctx, 30)
	if err != nil {
		return mcp.NewToolResultError("daily query failed: " + err.Error()), nil
	}

	sessions, err := s.store.TopSessionsByCost(ctx, 500, time.Time{})
	if err != nil {
		return mcp.NewToolResultError("sessions query failed: " + err.Error()), nil
	}

	hourly, err := s.store.GlobalHourlyAggregates(ctx, since30d)
	if err != nil {
		return mcp.NewToolResultError("hourly query failed: " + err.Error()), nil
	}

	computed := insights.Compute(insights.Input{
		Last7d:       last7d,
		PerModel:     perModel,
		Daily:        daily,
		Sessions:     sessions,
		HourlyGlobal: hourly,
	})

	resp := make([]InsightResponse, 0, len(computed))
	for _, ins := range computed {
		resp = append(resp, InsightResponse{
			ID:             ins.ID,
			Severity:       strings.ToLower(ins.Severity.String()),
			Title:          ins.Title,
			Detail:         ins.Detail,
			Recommendation: ins.Recommendation,
		})
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError("marshal failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

