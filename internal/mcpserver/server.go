// Package mcpserver exposes ClaudeOps data via the Model Context Protocol.
// It is designed to be driven over stdio by an MCP-compatible client.
package mcpserver

import (
	"time"

	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go server and the store.
type Server struct {
	mcp   *server.MCPServer
	store *store.Store
}

// New creates an MCP server wired to s and registers all tools.
func New(s *store.Store) *Server {
	srv := &Server{store: s}
	srv.mcp = server.NewMCPServer(
		"claudeops",
		"1.0.0",
		server.WithToolCapabilities(false),
	)
	srv.registerTools()
	return srv
}

// Serve blocks and handles stdio MCP traffic until the client disconnects.
func (s *Server) Serve() error {
	return server.ServeStdio(s.mcp)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// cacheRatio computes the fraction of tokens served from cache.
// Returns 0 when there are no tokens to avoid divide-by-zero.
func cacheRatio(cacheRead, in, out int64) float64 {
	total := in + out + cacheRead
	if total == 0 {
		return 0
	}
	return float64(cacheRead) / float64(total)
}

// formatTime returns an RFC3339 string for t, or "" for the zero time.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// clamp restricts v to the range [min, max].
func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
