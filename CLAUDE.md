# codesearch — project instructions for Claude

A Go CLI that indexes codebases with tree-sitter + Ollama + Qdrant and exposes them to AI agents over MCP stdio. See `README.md` for user-facing docs and `docs/superpowers/specs/` + `docs/superpowers/plans/` for design + implementation history.

## Module

- Module path: `github.com/kovaron/codesearch`
- Go version: 1.22+ (CI uses 1.26)
- CGO required (tree-sitter grammars are C bindings)

## Layout

```
cmd/codesearch/         cobra root + 5 subcommands (init, daemon, mcp, export, import)
internal/config/        YAML loader + validation
internal/parser/        Parser interface + Go/TS/Java/fallback (tree-sitter)
internal/embedder/      Embedder interface + Ollama HTTP client
internal/store/         Store interface + Qdrant gRPC client
internal/indexer/       Pipeline: parse → embed → upsert; also e2e_test.go
internal/watcher/       fsnotify watcher + 200ms debouncer
internal/mcp/           MCP stdio server + 5 tool handlers
pkg/archive/            .csi gzip-tar format (export/import)
testdata/fixtures/      sample.go, sample.ts, Sample.java (parser tests)
docs/superpowers/       design spec + implementation plan + prereq notes
```

## Commands

```bash
# Build
go build -o codesearch ./cmd/codesearch/

# Unit tests (no Docker)
go test ./...

# Integration tests (testcontainers spins up Qdrant v1.13.0)
go test -tags integration ./...

# Lint
go vet ./...
gofmt -l .
```

Tests under `_test.go` with `//go:build integration` need Docker running. Plain unit tests run anywhere.

## Conventions

- **Format + vet pass before every commit.** `go fmt ./...` and `go vet ./...` must be clean.
- **Strict TDD for non-trivial changes** — failing test first, then implementation.
- **No `_ = x` hacks.** Delete dead vars; check errors.
- **Wrap errors with `%w`** when callers might need `errors.Is/As`.
- **Avoid shadowing stdlib names** (`path`, `filepath`, `url`, etc.) — rename locals to `fp`, `u`, `path`, etc.
- **Named constants for repeat magic numbers** — vector dim, heartbeat interval, timeouts. See `internal/embedder/ollama.go:NomicEmbedTextDim` for the pattern.
- **Exported symbols carry doc comments** starting with the symbol name.
- **Atomic commits.** One logical change per commit. Conventional Commits prefix (`feat:`, `fix:`, `refactor:`, `chore:`, `docs:`, `test:`).

## Adapt the spec when it doesn't compile

The implementation plan was written against approximate dependency APIs. Confirmed adaptations that future tasks should not rediscover:

- `github.com/smacker/go-tree-sitter/<lang>` exposes `<lang>.GetLanguage()` returning `*sitter.Language`. The plan's `sitter.NewLanguage(<lang>.Language())` does not compile.
- `github.com/qdrant/go-client@v1.18` uses `qdrant.NewQuery(vec...)` for `QueryPoints.Query`, not `NewVectorInput`.
- `testcontainers-go` `MappedPort` returns `network.Port`; use `int(p.Num())`, not `p.Int()`.
- `mark3labs/mcp-go@v0.54` argument access: `req.RequireString("x")`, `req.GetString("x", "")`, `req.GetInt("x", 10)` — not `req.Params.Arguments["x"].(T)`.
- Qdrant container image `v1.9.0` lacks the `Query` gRPC endpoint; use `v1.13.0`.

When a new compile error reveals another mismatch, fix it and add to this list.

## Prereqs for integration tests + local run

- Docker daemon running
- Qdrant: `docker run -d --name qdrant -p 6333:6333 -p 6334:6334 qdrant/qdrant:v1.13.0`
- Ollama: `ollama serve` + `ollama pull nomic-embed-text`

Health probes in `docs/superpowers/prereqs-task8.md`.

## Vector dimension is locked at collection creation

Changing `ollama_model` to a model with a different embedding dimension requires dropping the Qdrant collection (`DELETE /collections/<project>`) and re-indexing. The default `nomic-embed-text` is 768-dim; the constant lives in `internal/embedder/ollama.go`.

## License

Apache-2.0. Copyright 2026 Aron Kovacs. Section 4(b) of the license requires marking modified files; section 4(c)-(d) requires preserving the `NOTICE` file in derivatives. PR contributions are accepted under the same license.
