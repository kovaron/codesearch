# Prereqs to start before Task 8

Tasks 8–15 need three things running locally. Start each, then tell Claude "continue with Task 8."

## 1. Docker

Need Docker daemon for `testcontainers-go` (used by Tasks 9, 10, 15 integration tests).

```bash
# Verify running
docker ps
```

If `Cannot connect to the Docker daemon`: start Docker Desktop (`open -a "Docker"`) and wait ~30 s for the whale icon to settle.

## 2. Qdrant (vector DB)

Native gRPC on `:6334`, HTTP on `:6333`.

```bash
docker run -d --name qdrant -p 6333:6333 -p 6334:6334 qdrant/qdrant
```

Verify:

```bash
curl -s http://localhost:6333/healthz   # → "healthz check passed"
```

If port `6334` is busy, the daemon and MCP both default to that port — stop the colliding process or change `qdrant_url` in `.codesearch.yaml`.

## 3. Ollama (embeddings)

Default port `:11434`.

```bash
# Install (macOS)
brew install ollama         # or: download from https://ollama.com

# Start the server (foreground, leave it running)
ollama serve &

# Pull the embedding model used by .codesearch.yaml
ollama pull nomic-embed-text
```

Verify:

```bash
curl -s http://localhost:11434/api/tags | jq '.models[].name'
# → should include "nomic-embed-text:latest"
```

The embedder calls `POST /api/embed` with `{"model":"nomic-embed-text","input":"<text>"}` — model name must match exactly.

## Quick health probe

Once all three are up:

```bash
docker ps --format '{{.Names}}: {{.Status}}' | grep qdrant
curl -s http://localhost:6333/healthz && echo
curl -s http://localhost:11434/api/tags | jq '.models | length'
```

Three OK lines = ready for Task 8.

## When done

Tear down:

```bash
docker stop qdrant && docker rm qdrant
pkill ollama   # or stop the ollama serve process
```

`.codesearch.yaml` already points at `http://localhost:6334` (Qdrant gRPC) and `http://localhost:11434` (Ollama) so no config changes are needed.
