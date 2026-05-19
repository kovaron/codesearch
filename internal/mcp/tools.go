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

const heartbeatStaleSeconds = 30

func registerTools(s *server.MCPServer, st store.Store, emb embedder.Embedder) {
	// 1. search_semantic
	s.AddTool(
		mcp.NewTool("search_semantic",
			mcp.WithDescription("Vector similarity search over indexed code. Use for fuzzy questions (\"what depends on X\", \"find something analogous to Y\"). For literal lookups — exact function name, error string, import path — prefer bash grep; semantic search burns tokens on questions with a literal answer. Returns headers only (path, name, lines, score); set include_source=true to fold source into each hit and skip a separate get_chunk."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Natural language search query")),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 5)")),
			mcp.WithBoolean("include_source", mcp.Description("Include each hit's source text inline (default false)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := req.RequireString("query")
			if err != nil {
				return nil, fmt.Errorf("search_semantic: %w", err)
			}
			limit := req.GetInt("limit", 5)
			includeSource := req.GetBool("include_source", false)

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return nil, fmt.Errorf("search_semantic: embed query: %w", err)
			}
			results, err := st.SearchSemantic(ctx, vec, limit)
			if err != nil {
				return nil, fmt.Errorf("search_semantic: %w", err)
			}
			if !includeSource {
				results = store.LeanResults(results)
			}
			return jsonResult(results)
		},
	)

	// 2. search_structural
	s.AddTool(
		mcp.NewTool("search_structural",
			mcp.WithDescription("Symbol-name lookup by exact match plus optional node-type and language filters. Fast and precise — use when you know the symbol name. Returns headers only (path, name, lines); set include_source=true to fold the source body into each hit and skip a separate get_chunk round-trip."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Symbol name to search for")),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithString("type", mcp.Description("Node type filter (e.g. function_declaration, class_declaration)")),
			mcp.WithString("language", mcp.Description("Language filter (e.g. go, python)")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
			mcp.WithBoolean("include_source", mcp.Description("Include each hit's source text inline (default false)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("query")
			if err != nil {
				return nil, fmt.Errorf("search_structural: %w", err)
			}
			nodeType := req.GetString("type", "")
			language := req.GetString("language", "")
			limit := req.GetInt("limit", 10)
			includeSource := req.GetBool("include_source", false)

			results, err := st.SearchStructural(ctx, name, nodeType, language, limit)
			if err != nil {
				return nil, fmt.Errorf("search_structural: %w", err)
			}
			if !includeSource {
				results = store.LeanResults(results)
			}
			return jsonResult(results)
		},
	)

	// 3. list_symbols
	s.AddTool(
		mcp.NewTool("list_symbols",
			mcp.WithDescription("List symbols under a file or directory prefix. Returns headers only (path, name, lines); fetch a specific symbol's body with get_chunk."),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithString("filepath", mcp.Required(), mcp.Description("File or directory path prefix")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			fp, err := req.RequireString("filepath")
			if err != nil {
				return nil, fmt.Errorf("list_symbols: %w", err)
			}
			limit := req.GetInt("limit", 50)

			results, err := st.ListByPath(ctx, fp, limit)
			if err != nil {
				return nil, fmt.Errorf("list_symbols: %w", err)
			}
			results = store.LeanResults(results)
			return jsonResult(results)
		},
	)

	// 4. get_chunk
	s.AddTool(
		mcp.NewTool("get_chunk",
			mcp.WithDescription("Fetch the full source of a named symbol from a file."),
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

	// 6. search_hybrid
	s.AddTool(
		mcp.NewTool("search_hybrid",
			mcp.WithDescription("Best general-purpose search; use when unsure which of semantic vs structural fits. Embeds the query and runs both semantic vector search and structural name search, then fuses results via reciprocal rank fusion (RRF). Returns headers only (path, name, lines, score); set include_source=true to fold source into each hit."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query (used for both semantic embedding and structural name lookup)")),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithNumber("limit", mcp.Description("Max results after fusion (default 5)")),
			mcp.WithBoolean("include_source", mcp.Description("Include each hit's source text inline (default false)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := req.RequireString("query")
			if err != nil {
				return nil, fmt.Errorf("search_hybrid: %w", err)
			}
			limit := req.GetInt("limit", 5)
			includeSource := req.GetBool("include_source", false)

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return nil, fmt.Errorf("search_hybrid: embed query: %w", err)
			}
			semResults, err := st.SearchSemantic(ctx, vec, limit)
			if err != nil {
				return nil, fmt.Errorf("search_hybrid: semantic: %w", err)
			}
			strResults, err := st.SearchStructural(ctx, query, "", "", limit)
			if err != nil {
				return nil, fmt.Errorf("search_hybrid: structural: %w", err)
			}
			results := store.FuseRRF(semResults, strResults, limit, 0)
			if !includeSource {
				results = store.LeanResults(results)
			}
			return jsonResult(results)
		},
	)

	// 7. trace_path
	s.AddTool(
		mcp.NewTool("trace_path",
			mcp.WithDescription("Traverse the call graph rooted at `symbol`. direction=inbound lists call sites; direction=outbound lists what symbol calls. Useful for impact analysis and 'who depends on X' questions in one round trip instead of 3-5 grep + read steps. Returns headers only (path, name, lines); set include_source=true to fold source into each hit."),
			mcp.WithString("symbol", mcp.Required(), mcp.Description("Symbol name to trace")),
			mcp.WithString("direction", mcp.Required(), mcp.Description("inbound: find call sites of symbol; outbound: find symbols called by symbol")),
			mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
			mcp.WithBoolean("include_source", mcp.Description("Include each hit's source text inline (default false)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			symbol, err := req.RequireString("symbol")
			if err != nil {
				return nil, fmt.Errorf("trace_path: %w", err)
			}
			direction, err := req.RequireString("direction")
			if err != nil {
				return nil, fmt.Errorf("trace_path: %w", err)
			}
			if direction != "inbound" && direction != "outbound" {
				return nil, fmt.Errorf("trace_path: direction must be \"inbound\" or \"outbound\", got %q", direction)
			}
			limit := req.GetInt("limit", 10)
			includeSource := req.GetBool("include_source", false)

			var results []store.SearchResult
			switch direction {
			case "inbound":
				results, err = store.FindCallers(ctx, st, symbol, limit)
			case "outbound":
				results, err = store.FindCallees(ctx, st, symbol, limit)
			}
			if err != nil {
				return nil, fmt.Errorf("trace_path: %w", err)
			}
			if !includeSource {
				results = store.LeanResults(results)
			}
			return jsonResult(results)
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
			running := age >= 0 && age < heartbeatStaleSeconds
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
