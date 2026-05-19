package bench

import "github.com/anthropics/anthropic-sdk-go"

// ArmName identifies which tool set the agent runs with.
type ArmName string

const (
	// ArmCodesearch equips the agent with codesearch MCP tools.
	ArmCodesearch ArmName = "codesearch"
	// ArmBaseline equips the agent with only a bash tool.
	ArmBaseline ArmName = "baseline"
)

// ToolDefs returns the Anthropic tool definitions for the given arm.
// Both arms include read_file and edit_file; ArmCodesearch adds the five
// codesearch tools and ArmBaseline adds bash.
func ToolDefs(arm ArmName) []anthropic.ToolUnionParam {
	shared := []anthropic.ToolUnionParam{
		makeTool("read_file", "Read the contents of a file.",
			propMap(
				"path", "string", "Absolute path of the file to read.",
			),
			[]string{"path"},
		),
		makeTool("edit_file", "Apply a textual replacement to a file.",
			propMap(
				"path", "string", "Absolute path of the file to edit.",
				"old", "string", "Exact text to replace.",
				"new", "string", "Replacement text.",
			),
			[]string{"path", "old", "new"},
		),
	}

	switch arm {
	case ArmCodesearch:
		return append(shared,
			makeTool("search_semantic", "Vector similarity search over indexed code. Use for fuzzy questions (\"what depends on X\", \"find something analogous to Y\"). For literal lookups (exact function name, error string, import path) prefer bash grep — semantic search burns tokens on questions with a literal answer. Returns headers only; set include_source=true to fold source into each hit.",
				propMap(
					"query", "string", "Natural language search query.",
					"limit", "integer", "Max results (default 5).",
					"include_source", "boolean", "Include each hit's source text inline (default false).",
				),
				[]string{"query"},
			),
			makeTool("search_structural", "Symbol-name lookup by exact match with optional node-type and language filters. Fast and precise — use when you know the symbol name. Returns headers only; set include_source=true to fold source into each hit and skip a separate get_chunk round-trip.",
				propMap(
					"query", "string", "Symbol name to search for.",
					"type", "string", "Node type filter, e.g. function_declaration.",
					"language", "string", "Language filter, e.g. go.",
					"limit", "integer", "Max results (default 10).",
					"include_source", "boolean", "Include each hit's source text inline (default false).",
				),
				[]string{"query"},
			),
			makeTool("list_symbols", "List symbols under a file or directory prefix. Returns headers only (path, name, lines); fetch a specific symbol's body with get_chunk.",
				propMap(
					"filepath", "string", "File or directory path prefix.",
					"limit", "integer", "Max results (default 50).",
				),
				[]string{"filepath"},
			),
			makeTool("get_chunk", "Fetch the full source of a named symbol from a file.",
				propMap(
					"filepath", "string", "File path containing the symbol.",
					"name", "string", "Symbol name.",
				),
				[]string{"filepath", "name"},
			),
			makeTool("search_hybrid", "Best general-purpose search; use when unsure which of semantic vs structural fits. Fuses semantic vector search and structural name search via reciprocal rank fusion (RRF). Returns headers only; set include_source=true to fold source into each hit.",
				propMap(
					"query", "string", "Search query (used for both semantic embedding and structural name lookup).",
					"limit", "integer", "Max results after fusion (default 5).",
					"include_source", "boolean", "Include each hit's source text inline (default false).",
				),
				[]string{"query"},
			),
			makeTool("index_status", "Check whether the codesearch daemon is running and when it last indexed.",
				map[string]any{},
				nil,
			),
			makeTool("trace_path", "Traverse the call graph rooted at `symbol`. direction=inbound lists call sites; direction=outbound lists what symbol calls. Useful for impact analysis and 'who depends on X' questions in one round trip instead of 3-5 grep + read steps. Returns headers only (path, name, lines); set include_source=true to fold source into each hit.",
				propMap(
					"symbol", "string", "Symbol name to trace.",
					"direction", "string", "inbound: find call sites of symbol; outbound: find symbols called by symbol.",
					"limit", "integer", "Max results (default 10).",
					"include_source", "boolean", "Include each hit's source text inline (default false).",
				),
				[]string{"symbol", "direction"},
			),
			makeTool("replace_in_files",
				"Replace every occurrence of `old` with `new` in every file matching `pattern` (supports **/*.ext recursive globs). Server-side multi-file edit — use for codebase-wide renames or string substitutions instead of looping read_file + edit_file. Returns changed file list and replacement count. Set dry_run=true to preview without writing.",
				propMap(
					"pattern", "string", "File path glob (e.g. **/*.go, internal/**/*.yaml). **/*.ext matches any depth.",
					"old", "string", "Literal substring to find.",
					"new", "string", "Replacement string (may be empty for deletion).",
					"project", "string", "Project name (for consistency with other tools).",
					"dry_run", "boolean", "When true, return matching files without writing (default false).",
				),
				[]string{"pattern", "old"},
			),
		)

	default: // ArmBaseline
		return append(shared,
			makeTool("bash", "Execute a shell command inside the sandbox. Available binaries: find, grep, sed, awk, cat, ls, wc, head, tail. Use for literal pattern lookups where you know the exact string to grep for.",
				propMap(
					"command", "string", "Shell command to run.",
					"timeout_ms", "integer", "Optional timeout in milliseconds.",
				),
				[]string{"command"},
			),
		)
	}
}

// makeTool constructs an anthropic.ToolUnionParam with Name, Description, and InputSchema.
func makeTool(name, description string, properties map[string]any, required []string) anthropic.ToolUnionParam {
	tp := anthropic.ToolParam{
		Name:        name,
		Description: anthropic.String(description),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: properties,
			Required:   required,
		},
	}
	return anthropic.ToolUnionParam{OfTool: &tp}
}

// propMap builds a JSON-Schema properties map from alternating name/type/description triplets.
// Each group of three args encodes one property.
func propMap(nametypedesc ...string) map[string]any {
	out := make(map[string]any, len(nametypedesc)/3)
	for i := 0; i+2 < len(nametypedesc); i += 3 {
		out[nametypedesc[i]] = map[string]any{
			"type":        nametypedesc[i+1],
			"description": nametypedesc[i+2],
		}
	}
	return out
}
