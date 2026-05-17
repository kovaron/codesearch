package mcp

import (
	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/internal/store"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates a configured MCP server with all 5 tools registered.
func NewServer(st store.Store, emb embedder.Embedder) *server.MCPServer {
	s := server.NewMCPServer("codesearch", "1.0.0",
		server.WithToolCapabilities(true),
	)
	registerTools(s, st, emb)
	return s
}

// Serve starts the MCP stdio server (blocking).
func Serve(st store.Store, emb embedder.Embedder) error {
	s := NewServer(st, emb)
	return server.ServeStdio(s)
}
