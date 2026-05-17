package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerTools(s *server.MCPServer, st store.Store, emb embedder.Embedder) {
	// 1. search_semantic
	s.AddTool(
		mcp.NewTool("search_semantic",
			mcp.WithDescription("Search code by natural language query using vector similarity"),
			mcp.WithString("query", mcp.Required(), mcp.Description("Natural language search query")),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := req.RequireString("query")
			if err != nil {
				return nil, fmt.Errorf("search_semantic: %w", err)
			}
			limit := req.GetInt("limit", 10)

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return nil, fmt.Errorf("search_semantic: embed query: %w", err)
			}
			results, err := st.SearchSemantic(ctx, vec, limit)
			if err != nil {
				return nil, fmt.Errorf("search_semantic: %w", err)
			}
			return jsonResult(results)
		},
	)

	// 2. search_structural
	s.AddTool(
		mcp.NewTool("search_structural",
			mcp.WithDescription("Search code by symbol name, type, and language"),
			mcp.WithString("query", mcp.Required(), mcp.Description("Symbol name to search for")),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithString("type", mcp.Description("Node type filter (e.g. function_declaration, class_declaration)")),
			mcp.WithString("language", mcp.Description("Language filter (e.g. go, python)")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("query")
			if err != nil {
				return nil, fmt.Errorf("search_structural: %w", err)
			}
			nodeType := req.GetString("type", "")
			language := req.GetString("language", "")
			limit := req.GetInt("limit", 20)

			results, err := st.SearchStructural(ctx, name, nodeType, language, limit)
			if err != nil {
				return nil, fmt.Errorf("search_structural: %w", err)
			}
			return jsonResult(results)
		},
	)

	// 3. list_symbols
	s.AddTool(
		mcp.NewTool("list_symbols",
			mcp.WithDescription("List all symbols in a file or directory path prefix"),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithString("filepath", mcp.Required(), mcp.Description("File or directory path prefix")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 200)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			fp, err := req.RequireString("filepath")
			if err != nil {
				return nil, fmt.Errorf("list_symbols: %w", err)
			}
			limit := req.GetInt("limit", 200)

			results, err := st.ListByPath(ctx, fp, limit)
			if err != nil {
				return nil, fmt.Errorf("list_symbols: %w", err)
			}
			return jsonResult(results)
		},
	)

	// 4. get_chunk
	s.AddTool(
		mcp.NewTool("get_chunk",
			mcp.WithDescription("Get a specific named symbol from a file"),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithString("filepath", mcp.Required(), mcp.Description("File path containing the symbol")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Symbol name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			fp, err := req.RequireString("filepath")
			if err != nil {
				return nil, fmt.Errorf("get_chunk: %w", err)
			}
			name, err := req.RequireString("name")
			if err != nil {
				return nil, fmt.Errorf("get_chunk: %w", err)
			}

			result, err := st.GetByName(ctx, fp, name)
			if err != nil {
				return nil, fmt.Errorf("get_chunk: %w", err)
			}
			if result == nil {
				return jsonResult(map[string]string{"error": "symbol not found"})
			}
			return jsonResult(result)
		},
	)

	// 5. index_status
	s.AddTool(
		mcp.NewTool("index_status",
			mcp.WithDescription("Check whether the codesearch daemon is running and when it last indexed"),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			age, err := st.HeartbeatAge(ctx)
			if err != nil {
				return nil, fmt.Errorf("index_status: %w", err)
			}
			running := age >= 0 && age < 30
			return jsonResult(map[string]any{
				"daemon_running":     running,
				"heartbeat_age_secs": age,
			})
		},
	)
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}
