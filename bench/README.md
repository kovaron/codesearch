# bench/ — codesearch efficiency benchmarks

Compare codesearch's MCP tools against a POSIX `find`/`grep`/`sed` baseline by
driving real Claude agent loops per task.

## Prereqs

- `ANTHROPIC_API_KEY` exported.
- codesearch daemon running on this repo (`codesearch daemon .` in another terminal).
- Qdrant + Ollama up (see project README).

## Run

```
codesearch bench --dry-run                    # validate YAML + goldens, no API spend
codesearch bench                              # full N=3 run, both arms, all tasks
codesearch bench --task-id find-callers-of-indexer-new --n 1
```

Output lands in `bench/results/<UTC-timestamp>/{results.json,report.md}`.

## Task catalogue

| # | ID | Kind | What it tests |
|---|---|---|---|
| 01 | `find-callers-of-indexer-new` | search | locating all call sites of `indexer.New` |
| 02 | `find-search-structural-handler` | search | finding the MCP tool registration for `search_structural` |
| 03 | `summarize-indexer-pipeline` | read | summarising parse→embed→upsert from source |
| 04 | `rename-parseQdrantURL` | edit | multi-file rename with definition + all call sites |
| 05 | `impact-batchembed` | analysis | interface-change impact on concrete implementors |
| 06 | `find-dead-embedder-export` | analysis | dead-export detection in a small package |
| 07 | `replace-nomic-embed-text` | edit | cross-file string replacement in *.go |
| 08 | `find-qdrant-client-error` | search | finding a specific error-wrapping call |
| 09 | `find-walk-functions` | search | regex-style symbol name search |
| 10 | `similar-to-walkandindex` | search | semantic similarity — find structurally alike functions |

## Authoring new tasks

See `01-find-callers.yaml` for the template. Each task needs:

- A terse, output-shape-constrained prompt.
- `golden.expected` with values verified against current repo HEAD before committing.
- Both arms enabled unless one is structurally impossible.

Golden type guidance:
- `answer_match` + `substring` — the agent's final text must contain each expected string.
- `answer_match` + `set_equal` — the agent's whitespace-split output must match exactly.
- `file_exists` — check a relative path exists in the sandbox after the agent finishes.

## Output format

`bench/results/<UTC-timestamp>/results.json` — machine-readable per-(task, arm) aggregates.  
`bench/results/<UTC-timestamp>/report.md` — human-readable table with headline efficiency ratio.
