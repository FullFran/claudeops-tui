package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) handleSummary(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	period, err := req.RequireString("period")
	if err != nil {
		return mcp.NewToolResultError("period is required (today, 7d, 30d)"), nil
	}

	now := time.Now().UTC()
	var since time.Time
	switch period {
	case "today":
		// AggregatesForToday already handles this, but we want to keep uniform API.
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		since = today
	case "7d":
		since = now.Add(-7 * 24 * time.Hour)
	case "30d":
		since = now.Add(-30 * 24 * time.Hour)
	default:
		return mcp.NewToolResultError("period must be one of: today, 7d, 30d"), nil
	}

	agg, err := s.store.AggregatesSince(ctx, since)
	if err != nil {
		return mcp.NewToolResultError("query failed: " + err.Error()), nil
	}

	resp := SummaryResponse{
		Period:        period,
		Events:        agg.Events,
		CostEUR:       agg.CostEUR,
		InTokens:      agg.InTokens,
		OutTokens:     agg.OutTokens,
		CacheRead:     agg.CacheReadTokens,
		CacheCreate:   agg.CacheCreateTokens,
		CacheHitRatio: cacheRatio(agg.CacheReadTokens, agg.InTokens, agg.OutTokens),
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError("marshal failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
