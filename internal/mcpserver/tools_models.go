package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) handleModels(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	models, err := s.store.PerModelAggregates(ctx, time.Time{})
	if err != nil {
		return mcp.NewToolResultError("query failed: " + err.Error()), nil
	}

	resp := make([]ModelResponse, 0, len(models))
	for _, m := range models {
		resp = append(resp, modelToResponse(m))
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError("marshal failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// modelToResponse converts a store.ModelAgg to a ModelResponse.
func modelToResponse(m store.ModelAgg) ModelResponse {
	return ModelResponse{
		Model:         m.Model,
		Events:        m.Events,
		CostEUR:       m.CostEUR,
		InTokens:      m.InTokens,
		OutTokens:     m.OutTokens,
		CacheRead:     m.CacheReadTokens,
		CacheCreate:   m.CacheCreateTokens,
		CacheHitRatio: cacheRatio(m.CacheReadTokens, m.InTokens, m.OutTokens),
	}
}
