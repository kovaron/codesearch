# CodeSearch — Design Spec

**Date:** 2026-05-16
**Status:** Approved

## Summary

`codesearch` is a Go binary that indexes codebases using tree-sitter (structural AST parsing) and Ollama embeddings stored in Qdrant (semantic search). It exposes indexed codebases to AI agents via an MCP stdio server. A background daemon keeps the index fresh using OS file-system events. Indexes are portable: a snapshot can be exported and imported by another developer to avoid re-embedding from scratch.

---

## Architecture

Two long-running processes, both from the same binary, sharing a Qdrant instance as their only coordination point:

```
┌─────────────────────────────────────────────────────────┐
│                   codesearch binary                     │
│                                                         │
│  ┌──────────────────┐      ┌─────────────────────────┐  │
│  │  daemon subcommand│      │    mcp subcommand       │  │
│  │                  │      │                         │  │
│  │  fsnotify watcher│      │  stdio MCP server       │  │
│  │  tree-sitter AST │      │  search_semantic tool   │  │
│  │  Ollama embedder │      │  search_structural tool │  │
│  │  Qdrant writer   │      │  Qdrant reader          │  │
│  └────────┬─────────┘      └───────────┬─────────────┘  │
└───────────┼────────────────────────────┼────────────────┘
            │                            │
            ▼                            ▼
    ┌───────────────┐          ┌──────────────────┐
    │    Qdrant     │◄─────────│   Ollama         │
    │  (vector DB)  │          │ (embeddings)     │
    └───────────────┘          └──────────────────┘
```

**External dependencies:**
- **Qdrant** — vector database, runs via Docker or system install
- **Ollama** — local embedding model server (default model: `nomic-embed-text`)
- **tree-sitter grammars** — compiled into the binary via CGo (`go-tree-sitter`)

---

## Configuration

One config file per project, `.codesearch.yaml`, placed at the project root:

```yaml
project: my-api
languages: [go, typescript, java]
include: ["src/**", "pkg/**"]
exclude: ["vendor/**", "node_modules/**", "**/*_test.go"]
qdrant_url: http://localhost:6334
ollama_url: http://localhost:11434
ollama_model: nomic-embed-text
```

Each project gets its own Qdrant collection (keyed by `project:`), so multiple projects can be indexed simultaneously without interference.

---

## CLI Subcommands

```
codesearch init <dir>        # generate .codesearch.yaml and run full initial index
codesearch daemon [dir]      # watch files and update index incrementally (background)
codesearch mcp               # start stdio MCP server (query-only)
codesearch export <file.csi> # snapshot current index to portable archive
codesearch import <file.csi> # restore index from archive
```

---

## Data Model

Each tree-sitter AST node (function, method, class declaration) becomes one Qdrant point:

```
Qdrant point {
  id:      sha256(filepath + node_type + start_byte)   // stable, content-addressed
  vector:  float32[768]                                // Ollama embedding of chunk text
  payload: {
    project:    "my-api"
    filepath:   "pkg/auth/handler.go"
    language:   "go"
    node_type:  "function_declaration"
    name:       "HandleLogin"
    start_line: 42
    end_line:   87
    text:       "func HandleLogin(w http.ResponseWriter, ..."
    checksum:   "abc123"                               // sha256 of chunk text
  }
}
```

**Supported languages and their indexed node types:**

| Language | Node types indexed |
|---|---|
| Go | `function_declaration`, `method_declaration`, `type_declaration` |
| TypeScript / JavaScript | `function_declaration`, `method_definition`, `class_declaration`, `lexical_declaration` wrapping an arrow function (i.e. `const foo = () => {}` at module or class scope) |
| Java | `method_declaration`, `class_declaration`, `interface_declaration` |

**Unsupported file types** (JSON, YAML, Markdown): indexed as a single chunk if under 8KB; skipped otherwise.

---

## Indexing Pipeline

Triggered per file change event:

```
File change event
      │
      ▼
1. Read file bytes
      │
      ▼
2. tree-sitter parse → extract AST nodes
      │
      ├─ For each node:
      │       │
      │       ▼
      │  3. Compute chunk ID (sha256 of filepath + node_type + start_byte)
      │       │
      │       ▼
      │  4. Check Qdrant for existing point with same checksum → skip if unchanged
      │       │
      │       ▼
      │  5. Send text to Ollama → get embedding vector
      │       │
      │       ▼
      │  6. Upsert point into Qdrant
      │
      ▼
7. Delete Qdrant points for nodes that no longer exist in file
```

---

## File Watching & Incremental Updates

**Daemon startup sequence:**

1. Read `.codesearch.yaml`
2. Full scan: walk all included paths, compute file checksums
3. Compare against Qdrant payload checksums → find new / changed / deleted files
4. Index only the delta
5. Register `fsnotify` watchers on all include directories
6. Enter event loop

**Event loop:**

- Events are debounced per file with a 200ms window (collapses editor temp-file-rename patterns)
- Delete events remove all Qdrant points for the filepath
- Create / Write / Rename events run the indexing pipeline
- Directory renames trigger bulk delete of old path points + parallel re-index of new subtree
- Worker pool for parallel indexing: default 4 workers, configurable via `CODESEARCH_WORKERS`

---

## MCP Server Tools

The `mcp` subcommand exposes five tools over stdio MCP:

### `search_semantic`

Natural language / concept search using vector similarity.

```json
Input:  { "query": "authentication middleware", "project": "my-api", "limit": 10 }
Output: [{ "filepath": "...", "name": "AuthMiddleware", "node_type": "function_declaration",
           "start_line": 12, "end_line": 45, "text": "...", "score": 0.91 }]
```

### `search_structural`

Exact or prefix name match, filtered by language and node type. Uses Qdrant payload filters — no vector similarity involved.

```json
Input:  { "query": "HandleLogin", "project": "my-api", "type": "function", "language": "go" }
Output: [{ "filepath": "...", "name": "HandleLogin", "start_line": 42, "end_line": 87, "text": "..." }]
```

### `list_symbols`

List all indexed symbols in a file or directory subtree.

```json
Input:  { "project": "my-api", "filepath": "pkg/auth/" }
Output: [{ "name": "AuthMiddleware", "node_type": "function_declaration", "filepath": "...", "start_line": 12 }]
```

### `get_chunk`

Retrieve the full source text of a specific symbol.

```json
Input:  { "project": "my-api", "filepath": "pkg/auth/middleware.go", "name": "AuthMiddleware" }
Output: { "text": "func AuthMiddleware(...) { ... }", "start_line": 12, "end_line": 45 }
```

### `index_status`

Health check and daemon liveness indicator.

```json
Input:  { "project": "my-api" }
Output: { "total_chunks": 1842, "last_indexed": "2026-05-16T14:23:00Z", "daemon_running": true }
```

`daemon_running` is determined by checking whether the Qdrant collection metadata includes a heartbeat timestamp written by the daemon within the last 30 seconds.

---

## Export / Import

**Archive format: `.csi` (gzip-compressed tar)**

```
<file>.csi
├── manifest.json        # project name, languages, codesearch version, export timestamp
├── qdrant-snapshot.tar  # Qdrant native collection snapshot via REST snapshot API
└── meta.json            # file checksums at time of export
```

**Export flow:**

```
codesearch export ./snapshot.csi
  1. Call Qdrant REST API: POST /collections/{name}/snapshots
  2. Download snapshot tar
  3. Write manifest.json and meta.json
  4. Bundle into gzip tar → snapshot.csi
```

**Import flow:**

```
codesearch import ./snapshot.csi
  1. Extract archive, validate manifest version compatibility
  2. Upload qdrant-snapshot.tar via Qdrant REST: PUT /collections/{name}/snapshots/upload
  3. Write meta.json to ~/.codesearch/projects/<name>/meta.json
  4. Done — daemon startup will delta-index only files changed since export timestamp
```

**Collaboration flow:**

```
Dev A:  codesearch init . && codesearch export ./snapshot.csi
        # share snapshot.csi via git LFS, S3, or any channel

Dev B:  codesearch import ./snapshot.csi
        codesearch daemon .   # incremental catch-up only
```

Dev B gets a fully populated index in seconds with no re-embedding cost.

---

## Error Handling

| Failure | Behavior |
|---|---|
| Single file parse failure | Log and skip; add to in-memory skip list; retry on next change event |
| Ollama unavailable | Write structural metadata to Qdrant without vector; queue embedding jobs in bounded in-memory channel (max 1000); drain when Ollama recovers |
| Qdrant unavailable | Daemon retries with exponential backoff: 1s → 2s → 4s → max 30s |
| MCP query while Qdrant down | Return structured error: `{"error": "index unavailable, daemon may be starting"}` |
| Config not found | Fail fast with message: `no .codesearch.yaml found — run codesearch init` |
| Binary / oversized file | Skip silently; log at debug level |

---

## Testing Strategy

| Layer | Approach |
|---|---|
| Tree-sitter parsing | Unit tests with fixture `.go`, `.ts`, `.java` source files; assert extracted symbol names and line ranges |
| Indexing pipeline | Integration tests against real Qdrant via `testcontainers-go`; verify upsert and checksum-based skip logic |
| File watcher | Integration tests using `os.WriteFile` to trigger `fsnotify` events; assert Qdrant point creation/deletion |
| MCP server | End-to-end: spawn `codesearch mcp` as subprocess, speak MCP protocol over stdin/stdout; assert tool responses |
| Export/Import | Round-trip: `init` → `export` → wipe Qdrant → `import` → assert identical chunk count and payloads |

---

## Package Structure

```
codesearch/
├── cmd/
│   └── codesearch/
│       └── main.go          # CLI entrypoint, subcommand dispatch
├── internal/
│   ├── config/              # .codesearch.yaml parsing and validation
│   ├── parser/              # tree-sitter wrappers per language
│   ├── embedder/            # Ollama HTTP client, batch embedding
│   ├── store/               # Qdrant client wrapper (upsert, query, delete, snapshot)
│   ├── indexer/             # indexing pipeline: parse → embed → store
│   ├── watcher/             # fsnotify daemon, debouncer, worker pool
│   └── mcp/                 # MCP stdio server, tool handlers
├── pkg/
│   └── archive/             # export/import .csi archive format
├── testdata/
│   └── fixtures/            # sample source files per language for parser tests
├── docs/
│   └── superpowers/specs/
│       └── 2026-05-16-codesearch-design.md
├── .codesearch.yaml         # self-indexing config for this repo
├── go.mod
└── go.sum
```
