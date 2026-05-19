package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/internal/store"
)

// ErrUnknownTool is returned by MCPDispatcher.Call when the tool name is not recognised.
var ErrUnknownTool = errors.New("unknown tool")

// heartbeatStaleSeconds mirrors the threshold used in internal/mcp/tools.go.
const heartbeatStaleSeconds = 30

// MCPDispatcher exposes codesearch MCP tools as in-process calls, mirroring
// the JSON-RPC handlers in internal/mcp/tools.go.
type MCPDispatcher struct {
	project string
	store   store.Store
	emb     embedder.Embedder
}

// NewMCPDispatcher creates an MCPDispatcher backed by the given store and embedder.
func NewMCPDispatcher(project string, st store.Store, emb embedder.Embedder) *MCPDispatcher {
	return &MCPDispatcher{project: project, store: st, emb: emb}
}

// Call dispatches a single tool call by name with JSON-encoded arguments and
// returns a JSON string result.
func (d *MCPDispatcher) Call(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	switch name {
	case "search_semantic":
		query := strArg(args, "query")
		limit := intArg(args, "limit", 5)
		includeSource := boolArg(args, "include_source", false)
		vec, err := d.emb.Embed(ctx, query)
		if err != nil {
			return "", fmt.Errorf("search_semantic: embed query: %w", err)
		}
		results, err := d.store.SearchSemantic(ctx, vec, limit)
		if err != nil {
			return "", fmt.Errorf("search_semantic: %w", err)
		}
		if !includeSource {
			results = store.LeanResults(results)
		}
		return marshal(results)

	case "search_structural":
		query := strArg(args, "query")
		nodeType := strArg(args, "type")
		language := strArg(args, "language")
		limit := intArg(args, "limit", 10)
		includeSource := boolArg(args, "include_source", false)
		results, err := d.store.SearchStructural(ctx, query, nodeType, language, limit)
		if err != nil {
			return "", fmt.Errorf("search_structural: %w", err)
		}
		if !includeSource {
			results = store.LeanResults(results)
		}
		return marshal(results)

	case "list_symbols":
		fp := strArg(args, "filepath")
		limit := intArg(args, "limit", 50)
		results, err := d.store.ListByPath(ctx, fp, limit)
		if err != nil {
			return "", fmt.Errorf("list_symbols: %w", err)
		}
		results = store.LeanResults(results)
		return marshal(results)

	case "get_chunk":
		fp := strArg(args, "filepath")
		symName := strArg(args, "name")
		result, err := d.store.GetByName(ctx, fp, symName)
		if err != nil {
			return "", fmt.Errorf("get_chunk: %w", err)
		}
		if result == nil {
			return marshal(map[string]string{"error": "symbol not found"})
		}
		return marshal(result)

	case "search_hybrid":
		query := strArg(args, "query")
		limit := intArg(args, "limit", 5)
		includeSource := boolArg(args, "include_source", false)
		vec, err := d.emb.Embed(ctx, query)
		if err != nil {
			return "", fmt.Errorf("search_hybrid: embed query: %w", err)
		}
		semResults, err := d.store.SearchSemantic(ctx, vec, limit)
		if err != nil {
			return "", fmt.Errorf("search_hybrid: semantic: %w", err)
		}
		strResults, err := d.store.SearchStructural(ctx, query, "", "", limit)
		if err != nil {
			return "", fmt.Errorf("search_hybrid: structural: %w", err)
		}
		results := store.FuseRRF(semResults, strResults, limit, 0)
		if !includeSource {
			results = store.LeanResults(results)
		}
		return marshal(results)

	case "index_status":
		age, err := d.store.HeartbeatAge(ctx)
		if err != nil {
			return "", fmt.Errorf("index_status: %w", err)
		}
		running := age >= 0 && age < heartbeatStaleSeconds
		return marshal(map[string]any{
			"daemon_running":     running,
			"heartbeat_age_secs": age,
		})
	}

	return "", fmt.Errorf("%w: %s", ErrUnknownTool, name)
}

// strArg extracts a string argument from the args map, defaulting to "".
func strArg(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// boolArg extracts a bool argument from the args map, defaulting to def.
func boolArg(args map[string]any, key string, def bool) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// intArg extracts an int argument from the args map, defaulting to def.
func intArg(args map[string]any, key string, def int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return def
}

// marshal serialises v to a JSON string.
func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
