package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) handleProjects(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := clamp(req.GetInt("limit", 20), 1, 100)

	projects, err := s.store.TopProjectsByCost(ctx, limit, time.Time{})
	if err != nil {
		return mcp.NewToolResultError("query failed: " + err.Error()), nil
	}

	resp := make([]ProjectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, ProjectResponse{
			Project: p.ProjectName,
			CostEUR: p.CostEUR,
		})
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError("marshal failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
