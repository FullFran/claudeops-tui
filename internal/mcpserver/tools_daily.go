package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) handleDaily(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	days := clamp(req.GetInt("days", 30), 1, 90)

	daily, err := s.store.DailyAggregatesLocal(ctx, days)
	if err != nil {
		return mcp.NewToolResultError("query failed: " + err.Error()), nil
	}

	resp := make([]DailyResponse, 0, len(daily))
	for _, d := range daily {
		resp = append(resp, DailyResponse{
			Date:     d.Date.Format("2006-01-02"),
			CostEUR:  d.CostEUR,
			Events:   d.Events,
			Sessions: d.Sessions,
		})
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError("marshal failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
