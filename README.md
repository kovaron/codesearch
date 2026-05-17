# codesearch

A self-hosted code indexing and search service for AI agents. Indexes your codebase with [tree-sitter](https://tree-sitter.github.io/) (structural parsing), embeds chunks via [Ollama](https://ollama.com/) (local LLM), stores vectors in [Qdrant](https://qdrant.tech/), and exposes everything to AI coding agents over a [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) stdio server. A file watcher keeps the index live as you edit.

Everything runs locally. No code leaves your machine.

---

## Features

- **Structural search** — exact symbol lookup (`HandleLogin`, `UserService`) filtered by language, node type (`function_declaration`, `class_declaration`, etc.).
- **Semantic search** — natural-language queries ("the function that signs JWTs") via vector similarity over Ollama embeddings.
- **Live index** — `fsnotify`-based watcher with 200 ms debounce. Edit a file, the index is up to date within a second.
- **Per-symbol granularity** — functions, methods, classes, interfaces are indexed as individual chunks. Unsupported file types fall back to whole-file indexing (≤ 8 KB).
- **Portable snapshots** — `export` / `import` produce a `.csi` archive (gzip-tar with manifest + Qdrant snapshot). Move an index between machines, share with teammates.
- **MCP-native** — five tools (`search_semantic`, `search_structural`, `list_symbols`, `get_chunk`, `index_status`) usable from Claude Code, Claude Desktop, or any MCP client.
- **Supported languages out of the box** — Go, TypeScript / JavaScript, Java. Everything else falls back to whole-file chunks.

---

## How it works

```
┌─────────────┐    fsnotify    ┌──────────────┐    parse     ┌─────────────┐
│  Your repo  │───────────────▶│   Watcher    │─────────────▶│ tree-sitter │
└─────────────┘                └──────────────┘              └──────┬──────┘
                                                                    │ chunks
                                                                    ▼
                                                             ┌─────────────┐
                                                             │   Ollama    │  HTTP /api/embed
                                                             │ (embedding) │
                                                             └──────┬──────┘
                                                                    │ vector
                                                                    ▼
                                                             ┌─────────────┐
                                                             │   Qdrant    │  gRPC :6334
                                                             │ (vector DB) │
                                                             └──────┬──────┘
                                                                    │ payload + vec
                                                                    ▼
┌────────────────────────┐   stdio MCP    ┌─────────────────────────────┐
│  Claude Code / Desktop │◀──────────────▶│  codesearch mcp (read-only) │
└────────────────────────┘                └─────────────────────────────┘
```

### Process model

- **`codesearch daemon`** — long-running. Reads `.codesearch.yaml`, walks the project, watches for changes, parses → embeds → upserts. Writes a heartbeat every 15 s.
- **`codesearch mcp`** — read-only stdio MCP server. Connects to the same Qdrant collection. Stateless. Spawned by your MCP client.

The two processes do **not** talk to each other — they share state through Qdrant. You can run them on different machines if Qdrant is reachable from both.

### Indexing pipeline

1. **Walk** — daemon walks every included path (respecting `exclude` globs).
2. **Parse** — `internal/parser` dispatches to a language parser based on file extension. Each parser runs tree-sitter queries to extract symbol-level chunks (`function_declaration`, `method_declaration`, `class_declaration`, `interface_declaration`, `type_declaration`, arrow functions). Unknown extensions go to the fallback parser (whole file, ≤ 8 KB).
3. **Delete stale** — before re-indexing a file, all existing points for that filepath are removed. Renames are handled as delete + insert.
4. **Embed** — each chunk's text is sent to Ollama (`POST /api/embed`) with the configured model (default: `nomic-embed-text`, 768-dim).
5. **Upsert** — each chunk becomes one Qdrant point. ID is `sha256("filepath|node_type|start_byte")[:8]` as `uint64`; payload carries filepath, name, node_type, language, start_line, end_line, full text.

### Query tools (exposed over MCP)

| Tool | Purpose | Required args | Optional args |
|---|---|---|---|
| `search_semantic` | natural-language → vector similarity | `query`, `project` | `limit` (default 10) |
| `search_structural` | exact / token symbol lookup | `query`, `project` | `type`, `language`, `limit` (default 20) |
| `list_symbols` | list chunks under a file or directory | `project`, `filepath` | `limit` (default 200) |
| `get_chunk` | fetch full source for one symbol | `project`, `filepath`, `name` | — |
| `index_status` | daemon liveness + heartbeat age | `project` | — |

`index_status` returns `daemon_running=true` if the heartbeat is less than 30 s old. This lets the agent detect a stopped indexer before relying on stale results.

---

## Prerequisites

- **Go 1.22+** to build (Go 1.26 used in CI).
- **Docker** running locally (for Qdrant; or use a remote Qdrant if you prefer).
- **Ollama** with an embedding model pulled.
- **macOS / Linux** — CGO is required (tree-sitter grammars are C). Windows works via WSL.

### Start Qdrant

```bash
docker run -d --name qdrant -p 6333:6333 -p 6334:6334 qdrant/qdrant:v1.13.0
curl -s http://localhost:6333/healthz   # → "healthz check passed"
```

`6333` is the REST/HTTP port (used by `export`/`import`); `6334` is gRPC (used by the daemon and MCP server).

### Start Ollama and pull the embedding model

```bash
# macOS — homebrew install
brew install ollama

# Start the server (leave it running)
ollama serve &

# Pull the default model
ollama pull nomic-embed-text
```

To use a different embedding model, change `ollama_model` in `.codesearch.yaml`. **Important:** if the model's vector dimension differs from 768, you also need to update the `dim` argument in `cmd/codesearch/cmd_daemon.go` and `cmd_mcp.go` (currently `embedder.NomicEmbedTextDim`). Qdrant collections are dimension-locked at creation.

---

## Install

### Build from source

```bash
git clone https://github.com/kovaron/codesearch.git
cd codesearch
go build -o codesearch ./cmd/codesearch/
sudo install codesearch /usr/local/bin/
```

Verify:

```bash
codesearch --help
```

You should see `init`, `daemon`, `mcp`, `export`, `import`.

---

## Usage

### 1. Initialize a project

From the root of the repo you want to index:

```bash
cd ~/code/my-app
codesearch init
```

This generates `.codesearch.yaml`:

```yaml
project: my-app
languages: [go, typescript, java]
include: ["**/*"]
exclude: ["vendor/**", "node_modules/**", ".git/**"]
qdrant_url: http://localhost:6334
ollama_url: http://localhost:11434
ollama_model: nomic-embed-text
workers: 4
```

Edit `include` / `exclude` to scope what gets indexed.

### 2. Start the daemon

```bash
codesearch daemon
```

The daemon runs in the foreground. Stops gracefully on `Ctrl-C` (SIGINT) or SIGTERM. Run under your favorite supervisor (`launchd`, `systemd`, `tmux`) for persistence.

Initial indexing happens as files are walked + their first change events fire. Heartbeat updates every 15 s.

### 3. Register the MCP server

#### Claude Code

Add to `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "codesearch": {
      "command": "/usr/local/bin/codesearch",
      "args": ["mcp"],
      "cwd": "/Users/you/code/my-app"
    }
  }
}
```

Restart Claude Code. The five tools appear under "MCP tools".

#### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "codesearch": {
      "command": "/usr/local/bin/codesearch",
      "args": ["mcp"],
      "cwd": "/Users/you/code/my-app"
    }
  }
}
```

Restart the app.

### 4. Use it from your agent

In Claude Code or Claude Desktop, ask:

> "Find the function that handles user login."

The agent calls `search_semantic` with your query, gets back ranked chunks, and reads the relevant code. No need to grep, no need to manually point at files.

For exact symbol lookups:

> "Show me the `HandleLogin` function."

The agent calls `search_structural` with `query="HandleLogin"`.

---

## Export / Import

Index portability uses Qdrant's native snapshot API.

### Export a project to a `.csi` archive

```bash
cd ~/code/my-app
codesearch export my-app-2026-05.csi
```

This produces a gzip-tar containing `manifest.json` (project name, version, export timestamp) and `qdrant-snapshot.bin` (the raw Qdrant snapshot).

### Import on another machine

```bash
cd ~/code/my-app
codesearch import my-app-2026-05.csi
codesearch daemon   # to catch up on edits since the snapshot
```

The import restores the collection wholesale; running `daemon` afterwards reconciles any drift.

---

## Configuration reference

`.codesearch.yaml` keys:

| Key | Default | Purpose |
|---|---|---|
| `project` | (required) | Qdrant collection name. Pick something stable and unique per repo. |
| `languages` | `[go, typescript, java]` | Informational; the parser registry actually keys off file extension. |
| `include` | `["**/*"]` | Glob list of paths to index. |
| `exclude` | `["vendor/**", "node_modules/**", ".git/**"]` | Glob list to skip. |
| `qdrant_url` | `http://localhost:6334` | Qdrant gRPC endpoint. The `export`/`import` commands derive the REST endpoint automatically (port 6333). |
| `ollama_url` | `http://localhost:11434` | Ollama base URL. |
| `ollama_model` | `nomic-embed-text` | Embedding model. Must match the dimension hardcoded for the project. |
| `workers` | `4` | (Reserved for future parallel indexing.) |

---

## Development

### Layout

```
cmd/codesearch/                   # cobra entry point + 5 subcommands
internal/config/                  # YAML loader + validation
internal/parser/                  # Parser interface + 4 language parsers + fallback
internal/embedder/                # Embedder interface + Ollama HTTP client
internal/store/                   # Store interface + Qdrant gRPC client
internal/indexer/                 # Pipeline: parse → embed → upsert
internal/watcher/                 # fsnotify + 200 ms debouncer
internal/mcp/                     # MCP stdio server + 5 tools
pkg/archive/                      # .csi gzip-tar format
testdata/fixtures/                # Sample.{go,ts,java} for parser tests
docs/superpowers/                 # Design spec + implementation plan
```

### Run tests

```bash
# Unit tests (no Docker required)
go test ./...

# Full integration suite (spins up Qdrant containers via testcontainers)
go test -tags integration ./...
```

Integration tests use `testcontainers-go` to start a fresh Qdrant `v1.13.0` container per test. Docker must be running.

### Lint

```bash
go vet ./...
gofmt -l .
```

### Add a new language parser

1. Add the tree-sitter grammar dep: `go get github.com/smacker/go-tree-sitter/<lang>`
2. Create `internal/parser/<lang>.go` following the pattern in `go.go` / `typescript.go` / `java.go` — define `<Lang>Parser` struct, queries for the symbol types you want, call `extractChunks`.
3. Register the extension(s) in `parser.NewRegistry()` (`internal/parser/parser.go`).
4. Add a fixture under `testdata/fixtures/` and tests in `internal/parser/parser_test.go`.

---

## Troubleshooting

**`Cannot connect to the Docker daemon`**
Start Docker Desktop and wait for it to settle. `docker ps` should list containers without error.

**`ollama: unexpected status 404`**
Either Ollama isn't running, or the model isn't pulled. Check `curl http://localhost:11434/api/tags`.

**`qdrant: ... CollectionNotFound`** after upgrading
Vector dimensions are locked at collection creation. If you change `ollama_model` to one with a different dim, drop the collection (`curl -X DELETE http://localhost:6333/collections/<project>`) and re-run `codesearch daemon`.

**Agent says "symbol not found" for code I can see**
Check `index_status` — if `daemon_running=false` or `heartbeat_age_secs > 30`, the daemon stopped. Restart it. If `daemon_running=true` but results are stale, check the daemon logs for `index error:` lines.

**Search returns whole-file chunks instead of symbol chunks**
The file type is going to the fallback parser. Add language support per "Add a new language parser" above, or accept whole-file chunks for that type.

---

## License

Licensed under the [Apache License, Version 2.0](./LICENSE). Copyright 2026 Aron Kovacs.

You may use, modify, and redistribute the source — including in commercial products. The license requires that you:

- Retain the copyright notice and `NOTICE` file in any redistribution.
- Mark any modified files with prominent notices indicating that you changed them.
- Pass along the same license terms to downstream users.

See [LICENSE](./LICENSE) and [NOTICE](./NOTICE) for the full text and third-party attributions.

---

## Acknowledgments

Built on top of [tree-sitter](https://tree-sitter.github.io/), [Ollama](https://ollama.com/), [Qdrant](https://qdrant.tech/), [mcp-go](https://github.com/mark3labs/mcp-go), [cobra](https://cobra.dev/), and [fsnotify](https://github.com/fsnotify/fsnotify). See `NOTICE` for the full list.
