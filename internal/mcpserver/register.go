package mcpserver

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// registerTools adds all 7 ClaudeOps tools to the MCP server.
func (s *Server) registerTools() {
	s.mcp.AddTool(
		mcp.NewTool("claudeops_summary",
			mcp.WithDescription("Return aggregate usage stats (cost, tokens, cache) for a time period."),
			mcp.WithString("period",
				mcp.Required(),
				mcp.Description("Time window: today, 7d, or 30d"),
				mcp.Enum("today", "7d", "30d"),
			),
		),
		s.handleSummary,
	)

	s.mcp.AddTool(
		mcp.NewTool("claudeops_sessions",
			mcp.WithDescription("List sessions ordered by cost descending."),
			mcp.WithNumber("limit",
				mcp.Description("Max sessions to return (1-100, default 20)"),
			),
		),
		s.handleSessions,
	)

	s.mcp.AddTool(
		mcp.NewTool("claudeops_session_detail",
			mcp.WithDescription("Return full detail for a single session: per-model breakdown and hourly activity."),
			mcp.WithString("session_id",
				mcp.Required(),
				mcp.Description("The session ID to look up"),
			),
		),
		s.handleSessionDetail,
	)

	s.mcp.AddTool(
		mcp.NewTool("claudeops_projects",
			mcp.WithDescription("List projects ordered by total cost descending."),
			mcp.WithNumber("limit",
				mcp.Description("Max projects to return (default 20)"),
			),
		),
		s.handleProjects,
	)

	s.mcp.AddTool(
		mcp.NewTool("claudeops_models",
			mcp.WithDescription("Return per-model aggregate stats across all time."),
		),
		s.handleModels,
	)

	s.mcp.AddTool(
		mcp.NewTool("claudeops_daily",
			mcp.WithDescription("Return per-day cost and activity for the last N days."),
			mcp.WithNumber("days",
				mcp.Description("Number of days to include (1-90, default 30)"),
			),
		),
		s.handleDaily,
	)

	s.mcp.AddTool(
		mcp.NewTool("claudeops_insights",
			mcp.WithDescription("Return derived insights about usage patterns, cost trends, and cache efficiency."),
		),
		s.handleInsights,
	)
}
