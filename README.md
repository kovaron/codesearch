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

#### Foreground (simplest, for first run)

```bash
codesearch daemon
```

The daemon runs in the foreground. Stops gracefully on `Ctrl-C` (SIGINT) or SIGTERM.

On startup the daemon walks the full project tree, calls `IndexFile` for every file with a known language extension, and subscribes recursively to every non-noise subdirectory (`.git`, `vendor`, `node_modules`, IDE/build artifacts are skipped). After that it runs continuously, picking up writes/creates/renames with a 200 ms debounce and writing a heartbeat to Qdrant every 15 seconds.

#### Background (detached)

```bash
mkdir -p ~/.codesearch/logs
nohup codesearch daemon /path/to/your/repo \
    > ~/.codesearch/logs/codesearch-daemon.log 2>&1 &
disown
```

Check it is alive:

```bash
pgrep -fl 'codesearch daemon'
tail -f ~/.codesearch/logs/codesearch-daemon.log
```

#### macOS — launchd (survives reboots, auto-restarts on crash)

Create `~/Library/LaunchAgents/com.<you>.codesearch.plist` (replace `<you>`, the binary path, the project path, and the username in the log paths to match your machine):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.you.codesearch</string>

    <key>ProgramArguments</key>
    <array>
        <string>/Users/you/.local/bin/codesearch</string>
        <string>daemon</string>
        <string>/Users/you/code/your-repo</string>
    </array>

    <key>WorkingDirectory</key>
    <string>/Users/you/code/your-repo</string>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
        <key>Crashed</key>
        <true/>
    </dict>

    <key>ThrottleInterval</key>
    <integer>10</integer>

    <key>StandardOutPath</key>
    <string>/Users/you/.codesearch/logs/codesearch-daemon.log</string>

    <key>StandardErrorPath</key>
    <string>/Users/you/.codesearch/logs/codesearch-daemon.log</string>

    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin</string>
    </dict>

    <key>ProcessType</key>
    <string>Background</string>
</dict>
</plist>
```

A ready-to-edit copy for this repo lives at [`docs/launchd/com.kovaron.codesearch.plist`](docs/launchd/com.kovaron.codesearch.plist).

`KeepAlive.Crashed=true` restarts the daemon if it crashes; `SuccessfulExit=false` does **not** restart on a clean exit (so `launchctl bootout` actually stops it). `ThrottleInterval=10` caps the restart rate at one every 10 s.

Load it:

```bash
mkdir -p ~/.codesearch/logs
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.you.codesearch.plist
```

Verify:

```bash
launchctl print gui/$(id -u)/com.you.codesearch | grep -E 'state|pid|last exit'
# → state = running, pid = <n>, last exit code = (never exited)

pgrep -fl 'codesearch daemon'
tail -f ~/.codesearch/logs/codesearch-daemon.log
```

#### Linux — systemd user unit (sketch)

`~/.config/systemd/user/codesearch.service`:

```ini
[Unit]
Description=codesearch indexing daemon
After=network.target

[Service]
ExecStart=/home/you/.local/bin/codesearch daemon /home/you/code/your-repo
Restart=on-failure
RestartSec=10
StandardOutput=append:/home/you/.codesearch/logs/codesearch-daemon.log
StandardError=append:/home/you/.codesearch/logs/codesearch-daemon.log

[Install]
WantedBy=default.target
```

Then:

```bash
mkdir -p ~/.codesearch/logs
systemctl --user daemon-reload
systemctl --user enable --now codesearch
journalctl --user -u codesearch -f
```

### Operational commands

| Action | Foreground / `nohup` | launchd |
|---|---|---|
| Start | `codesearch daemon /path/to/repo` | `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.you.codesearch.plist` |
| Stop  | `Ctrl-C` (fg) or `pkill -f 'codesearch daemon'` | `launchctl bootout gui/$(id -u)/com.you.codesearch` |
| Restart | stop + start | `launchctl kickstart -k gui/$(id -u)/com.you.codesearch` |
| Status | `pgrep -fl 'codesearch daemon'` | `launchctl print gui/$(id -u)/com.you.codesearch \| grep -E 'state\|pid'` |
| Logs | `tail -f ~/.codesearch/logs/codesearch-daemon.log` | same |
| Reload after rebuild | stop, replace binary, start | `launchctl kickstart -k gui/$(id -u)/com.you.codesearch` |

Verify the index is actually populating:

```bash
curl -s -X POST http://localhost:6333/collections/<project>/points/count \
    -H 'Content-Type: application/json' -d '{}'
# → {"result":{"count":<n>},"status":"ok"}
```

A non-zero count + a recent heartbeat (use the `index_status` MCP tool from your agent) = healthy daemon.

### Updating the daemon after a code change

```bash
cd /path/to/codesearch
go build -o ~/.local/bin/codesearch ./cmd/codesearch/
launchctl kickstart -k gui/$(id -u)/com.you.codesearch   # if running under launchd
# OR: kill the foreground/nohup process and start it again
```

The collection on Qdrant survives binary upgrades; only restart the daemon, not Qdrant.

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

Index portability uses Qdrant's native snapshot API. A `.csi` archive is a gzip-tar containing `manifest.json` (project name, version, export timestamp) and `qdrant-snapshot.bin` (the raw Qdrant snapshot).

### Quick export / import

```bash
# Write the snapshot to an explicit path
codesearch export ~/Desktop/my-app-2026-05.csi

# Restore from that path on another machine
codesearch import ~/Desktop/my-app-2026-05.csi
codesearch daemon   # reconciles drift since the snapshot
```

The import restores the collection wholesale; `daemon` afterwards picks up any files that changed since the snapshot was taken.

### Committing the index to the repo

`codesearch` treats `.codesearch/index.csi` as the canonical repo-local snapshot. Both `export` and `import` default to this path when invoked with no argument, so the index can ride along with the source code:

```bash
# Refresh the committed snapshot before pushing
codesearch export              # writes .codesearch/index.csi (mkdir -p as needed)
git add .codesearch/index.csi
git commit -m "chore: refresh codesearch index snapshot"
git push

# On clone (anywhere), bootstrap the index from the commit
git clone <repo> && cd <repo>
codesearch import              # reads .codesearch/index.csi
codesearch daemon              # background indexer catches up on any diff
```

The repo's `.gitignore` already keeps stray `*.csi` archives out of the working tree but **un-ignores** `.codesearch/index.csi` specifically. Anyone cloning your repo gets the indexed snapshot for free; their local Qdrant restores it in one call, and the daemon then watches for changes.

**Size note.** With `nomic-embed-text` (768-dim, 3 KB per vector) plus payload, a 132-chunk project compresses to ~50–200 KB. Repos with tens of thousands of symbols may reach several MB — consider git-LFS or excluding the snapshot from main and shipping it as a release artifact instead.

**Refresh policy.** The snapshot is a point-in-time copy. The daemon keeps the *live* Qdrant collection in sync via fsnotify, but the committed `.csi` only matches reality when you re-run `codesearch export`. Pick one:

- **Per-PR refresh** — run `codesearch export && git add .codesearch/index.csi` as part of your release / PR-prep script.
- **Pre-commit hook** — automatic but adds a few hundred ms to every commit.
- **CI artifact** — let CI run `codesearch export` and upload the result; skip committing the file altogether.

---

## Verifying the install

After registering the MCP server in Claude Code or Claude Desktop and restarting the app, ask the agent any of these prompts. Each one exercises a different MCP tool — if all five answer correctly, the pipeline is healthy.

| Prompt | Exercises | Expected behavior |
|---|---|---|
| "Use the `codesearch` MCP to check if the indexer daemon is running." | `index_status` | Returns `{"daemon_running":true,"heartbeat_age_secs":<n>}` with `n < 30`. If `daemon_running` is `false` or the age is large, the daemon stopped — restart it. |
| "Using `codesearch`, find the function that handles initial filesystem walking." | `search_semantic` | Returns chunks ranked by vector similarity. Top result should be `WalkFiles` or `WalkAndIndex` in `internal/indexer/walker.go`. |
| "Using `codesearch`, show me the `parseQdrantURL` function." | `search_structural` (query=`parseQdrantURL`, type=`function_declaration`) | Returns a single chunk pointing at `cmd/codesearch/cmd_daemon.go` with line numbers + the function body. |
| "Using `codesearch`, list every exported symbol in `internal/parser/`." | `list_symbols` (filepath prefix) | Returns the parser-package chunks (`Chunk`, `Parser`, `Registry`, `NewRegistry`, `For`, each `*Parser` `Parse` method, language-specific patterns). |
| "Using `codesearch`, fetch the full source of `Indexer.IndexFile`." | `get_chunk` | Returns the exact function body from `internal/indexer/indexer.go`. |

If the agent does not pick up the MCP tools, restart Claude Code/Desktop after editing the settings file. Tool discovery happens once at startup.

If a search returns empty or stale results, run:

```bash
# Count points in the collection
curl -s -X POST http://localhost:6333/collections/<project>/points/count \
    -H 'Content-Type: application/json' -d '{}'
```

A count of `1` (the heartbeat alone) means the indexer never wrote any chunks — check the daemon log. A count of `0` means the collection was never created — the daemon hasn't started.

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
