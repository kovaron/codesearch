# CodeSearch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `codesearch`, a Go CLI that indexes codebases with tree-sitter + Ollama + Qdrant and exposes them to AI agents via a stdio MCP server with live incremental file-watch updates.

**Architecture:** Single binary, multiple subcommands. `daemon` watches files, parses with tree-sitter, embeds via Ollama, writes to Qdrant. `mcp` is a query-only stdio MCP server reading directly from Qdrant. Both processes talk to Qdrant independently — no IPC. `export`/`import` use Qdrant's native snapshot API to produce portable `.csi` archives.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` (CLI), `github.com/smacker/go-tree-sitter` + grammar packages (parsing), Ollama `/api/embed` HTTP endpoint (embeddings), `github.com/qdrant/go-client` (vector DB via gRPC), `github.com/mark3labs/mcp-go` (MCP stdio server), `github.com/fsnotify/fsnotify` (OS file events), `github.com/testcontainers/testcontainers-go` (integration tests), `gopkg.in/yaml.v3` (config).

---

## File Map

```
codesearch/
├── cmd/codesearch/main.go               # cobra root + subcommand wiring
├── internal/
│   ├── config/
│   │   ├── config.go                    # Config struct, Load(), Validate()
│   │   └── config_test.go
│   ├── parser/
│   │   ├── parser.go                    # Parser interface, Chunk struct, NewRegistry()
│   │   ├── go.go                        # Go language parser
│   │   ├── typescript.go                # TypeScript/JavaScript parser
│   │   ├── java.go                      # Java parser
│   │   ├── fallback.go                  # Whole-file fallback for unsupported types
│   │   └── parser_test.go
│   ├── embedder/
│   │   ├── embedder.go                  # Embedder interface
│   │   ├── ollama.go                    # Ollama /api/embed HTTP client
│   │   └── embedder_test.go
│   ├── store/
│   │   ├── store.go                     # Store interface
│   │   ├── qdrant.go                    # Qdrant gRPC client wrapper
│   │   └── store_test.go               # Integration test (testcontainers)
│   ├── indexer/
│   │   ├── indexer.go                   # IndexFile(), DeleteFile()
│   │   └── indexer_test.go             # Integration test (testcontainers)
│   ├── watcher/
│   │   ├── watcher.go                   # fsnotify daemon, debouncer, worker pool
│   │   └── watcher_test.go
│   └── mcp/
│       ├── server.go                    # MCP server setup, ServeStdio()
│       ├── tools.go                     # All 5 tool handlers
│       └── mcp_test.go
├── pkg/archive/
│   ├── archive.go                       # Export/Import .csi gzip-tar format
│   └── archive_test.go
├── testdata/fixtures/
│   ├── sample.go
│   ├── sample.ts
│   └── Sample.java
├── docs/superpowers/
│   ├── specs/2026-05-16-codesearch-design.md
│   └── plans/2026-05-16-codesearch.md
├── CLAUDE.md
├── go.mod
└── go.sum
```

---

## Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/codesearch/main.go`

- [ ] **Step 1: Initialize go module**

```bash
cd /Users/kovaron/projects/file-tree
go mod init github.com/kovaron/codesearch
```

- [ ] **Step 2: Add all dependencies**

```bash
go get github.com/spf13/cobra@latest
go get github.com/smacker/go-tree-sitter@latest
go get github.com/qdrant/go-client@latest
go get github.com/mark3labs/mcp-go@latest
go get github.com/fsnotify/fsnotify@latest
go get gopkg.in/yaml.v3@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/google/uuid@latest
```

- [ ] **Step 3: Create `cmd/codesearch/main.go`**

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

func main() {
    root := &cobra.Command{
        Use:   "codesearch",
        Short: "Code indexing and search tool for AI agents",
    }
    if err := root.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

- [ ] **Step 4: Build to verify**

```bash
go build ./cmd/codesearch/
```
Expected: binary `codesearch` (or `codesearch.exe`) created with no error output.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add go.mod go.sum cmd/ testdata/
git commit -m "feat: project scaffold with cobra CLI and test fixtures"
```

---

## Task 2: Config Package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `.codesearch.yaml` (example, at repo root)

- [ ] **Step 1: Write the failing test**

```go
// internal/config/config_test.go
package config_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/kovaron/codesearch/internal/config"
)

func TestLoad(t *testing.T) {
    dir := t.TempDir()
    content := `
project: my-api
languages: [go, typescript]
include: ["pkg/**"]
exclude: ["vendor/**"]
qdrant_url: http://localhost:6334
ollama_url: http://localhost:11434
ollama_model: nomic-embed-text
`
    if err := os.WriteFile(filepath.Join(dir, ".codesearch.yaml"), []byte(content), 0644); err != nil {
        t.Fatal(err)
    }

    cfg, err := config.Load(dir)
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }

    if cfg.Project != "my-api" {
        t.Errorf("Project = %q, want %q", cfg.Project, "my-api")
    }
    if len(cfg.Languages) != 2 {
        t.Errorf("Languages len = %d, want 2", len(cfg.Languages))
    }
    if cfg.QdrantURL != "http://localhost:6334" {
        t.Errorf("QdrantURL = %q", cfg.QdrantURL)
    }
}

func TestLoad_MissingFile(t *testing.T) {
    _, err := config.Load(t.TempDir())
    if err == nil {
        t.Error("Load() expected error for missing file, got nil")
    }
}

func TestValidate_MissingProject(t *testing.T) {
    cfg := &config.Config{
        Languages: []string{"go"},
        QdrantURL: "http://localhost:6334",
        OllamaURL: "http://localhost:11434",
    }
    if err := cfg.Validate(); err == nil {
        t.Error("Validate() expected error for missing project, got nil")
    }
}
```

- [ ] **Step 2: Run test to confirm failure**

```bash
go test ./internal/config/ -v
```
Expected: `FAIL — package config_test: cannot find package`

- [ ] **Step 3: Implement `internal/config/config.go`**

```go
package config

import (
    "fmt"
    "os"
    "path/filepath"

    "gopkg.in/yaml.v3"
)

type Config struct {
    Project    string   `yaml:"project"`
    Languages  []string `yaml:"languages"`
    Include    []string `yaml:"include"`
    Exclude    []string `yaml:"exclude"`
    QdrantURL  string   `yaml:"qdrant_url"`
    OllamaURL  string   `yaml:"ollama_url"`
    OllamaModel string  `yaml:"ollama_model"`
    Workers    int      `yaml:"workers"`
}

func Load(dir string) (*Config, error) {
    path := filepath.Join(dir, ".codesearch.yaml")
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("no .codesearch.yaml found in %s — run codesearch init", dir)
    }
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, fmt.Errorf("parse .codesearch.yaml: %w", err)
    }
    cfg.setDefaults()
    return &cfg, cfg.Validate()
}

func (c *Config) setDefaults() {
    if c.OllamaModel == "" {
        c.OllamaModel = "nomic-embed-text"
    }
    if c.QdrantURL == "" {
        c.QdrantURL = "http://localhost:6334"
    }
    if c.OllamaURL == "" {
        c.OllamaURL = "http://localhost:11434"
    }
    if c.Workers == 0 {
        c.Workers = 4
    }
    if len(c.Languages) == 0 {
        c.Languages = []string{"go", "typescript", "java"}
    }
}

func (c *Config) Validate() error {
    if c.Project == "" {
        return fmt.Errorf("config: 'project' is required")
    }
    return nil
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/config/ -v
```
Expected: `PASS` for all three tests.

- [ ] **Step 5: Create example `.codesearch.yaml` at repo root**

```yaml
# .codesearch.yaml
project: codesearch
languages: [go]
include: ["**/*.go"]
exclude: ["vendor/**", "testdata/**"]
qdrant_url: http://localhost:6334
ollama_url: http://localhost:11434
ollama_model: nomic-embed-text
workers: 4
```

- [ ] **Step 6: Format and commit**

```bash
go fmt ./...
git add internal/config/ .codesearch.yaml
git commit -m "feat: config package with YAML load and validation"
```

---

## Task 3: Parser Interface and Chunk Types

**Files:**
- Create: `internal/parser/parser.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/parser/parser_test.go
package parser_test

import (
    "testing"

    "github.com/kovaron/codesearch/internal/parser"
)

func TestRegistryReturnsParserForGo(t *testing.T) {
    reg := parser.NewRegistry()
    p, ok := reg.For("sample.go")
    if !ok {
        t.Fatal("expected parser for .go file, got none")
    }
    if p == nil {
        t.Fatal("parser is nil")
    }
}

func TestRegistryReturnsParserForTS(t *testing.T) {
    reg := parser.NewRegistry()
    _, ok := reg.For("sample.ts")
    if !ok {
        t.Fatal("expected parser for .ts file, got none")
    }
}

func TestRegistryReturnsFallbackForUnknown(t *testing.T) {
    reg := parser.NewRegistry()
    p, ok := reg.For("config.yaml")
    if !ok {
        t.Fatal("expected fallback parser for .yaml file, got none")
    }
    if p == nil {
        t.Fatal("fallback parser is nil")
    }
}
```

- [ ] **Step 2: Run test to confirm failure**

```bash
go test ./internal/parser/ -run TestRegistry -v
```
Expected: `FAIL — cannot find package`

- [ ] **Step 3: Implement `internal/parser/parser.go`**

```go
package parser

import "path/filepath"

// Chunk represents a single indexed code unit (function, method, class, or file).
type Chunk struct {
    Name      string // symbol name; empty for file-level chunks
    NodeType  string // "function_declaration", "method_declaration", etc.
    Language  string
    StartLine int
    EndLine   int
    StartByte int
    Text      string
}

// Parser extracts Chunks from source bytes.
type Parser interface {
    Parse(source []byte, language string) ([]Chunk, error)
}

// Registry maps file extensions to parsers.
type Registry struct {
    parsers  map[string]Parser
    fallback Parser
}

func NewRegistry() *Registry {
    r := &Registry{
        parsers:  make(map[string]Parser),
        fallback: &FallbackParser{},
    }
    goP := &GoParser{}
    r.parsers[".go"] = goP

    tsP := &TypeScriptParser{}
    r.parsers[".ts"] = tsP
    r.parsers[".tsx"] = tsP
    r.parsers[".js"] = tsP
    r.parsers[".jsx"] = tsP

    javaP := &JavaParser{}
    r.parsers[".java"] = javaP

    return r
}

// For returns the Parser for the given filename and whether one was found.
// Always returns a non-nil parser (falls back to FallbackParser).
func (r *Registry) For(filename string) (Parser, bool) {
    ext := filepath.Ext(filename)
    p, ok := r.parsers[ext]
    if !ok {
        return r.fallback, true
    }
    return p, ok
}
```

- [ ] **Step 4: Add stub types so the package compiles (full implementations come in Tasks 4–7)**

```go
// internal/parser/go.go
package parser

type GoParser struct{}

func (g *GoParser) Parse(source []byte, language string) ([]Chunk, error) {
    return nil, nil
}
```

```go
// internal/parser/typescript.go
package parser

type TypeScriptParser struct{}

func (t *TypeScriptParser) Parse(source []byte, language string) ([]Chunk, error) {
    return nil, nil
}
```

```go
// internal/parser/java.go
package parser

type JavaParser struct{}

func (j *JavaParser) Parse(source []byte, language string) ([]Chunk, error) {
    return nil, nil
}
```

```go
// internal/parser/fallback.go
package parser

type FallbackParser struct{}

func (f *FallbackParser) Parse(source []byte, language string) ([]Chunk, error) {
    return nil, nil
}
```

- [ ] **Step 5: Run tests to confirm pass**

```bash
go test ./internal/parser/ -run TestRegistry -v
```
Expected: all `TestRegistry*` tests PASS.

- [ ] **Step 6: Format and commit**

```bash
go fmt ./...
git add internal/parser/
git commit -m "feat: parser interface, Chunk type, and registry with stub implementations"
```

---

## Task 4: Go Language Parser

**Files:**
- Modify: `internal/parser/go.go`
- Modify: `internal/parser/parser_test.go` (add Go-specific tests)

- [ ] **Step 1: Add failing tests for the Go parser**

Append to `internal/parser/parser_test.go`:

```go
func TestGoParser_Functions(t *testing.T) {
    src, err := os.ReadFile("../../testdata/fixtures/sample.go")
    if err != nil {
        t.Fatal(err)
    }

    p := &parser.GoParser{}
    chunks, err := p.Parse(src, "go")
    if err != nil {
        t.Fatalf("Parse() error = %v", err)
    }

    names := make(map[string]bool)
    for _, c := range chunks {
        names[c.Name] = true
    }

    for _, want := range []string{"Add", "Greet", "Area", "Perimeter"} {
        if !names[want] {
            t.Errorf("missing symbol %q in parsed chunks; got %v", want, chunkNames(chunks))
        }
    }
}

func TestGoParser_LineNumbers(t *testing.T) {
    src := []byte(`package main

func Foo() int {
    return 1
}
`)
    p := &parser.GoParser{}
    chunks, err := p.Parse(src, "go")
    if err != nil {
        t.Fatalf("Parse() error = %v", err)
    }
    if len(chunks) != 1 {
        t.Fatalf("expected 1 chunk, got %d", len(chunks))
    }
    if chunks[0].Name != "Foo" {
        t.Errorf("Name = %q, want %q", chunks[0].Name, "Foo")
    }
    if chunks[0].StartLine != 3 {
        t.Errorf("StartLine = %d, want 3", chunks[0].StartLine)
    }
}

func chunkNames(chunks []parser.Chunk) []string {
    var names []string
    for _, c := range chunks {
        names = append(names, c.Name)
    }
    return names
}
```

Add `"os"` to imports in the test file.

- [ ] **Step 2: Run tests to confirm failure**

```bash
go test ./internal/parser/ -run TestGoParser -v
```
Expected: `FAIL — chunks is nil / empty`

- [ ] **Step 3: Implement `internal/parser/go.go`**

```go
package parser

import (
    "context"
    "fmt"

    sitter "github.com/smacker/go-tree-sitter"
    "github.com/smacker/go-tree-sitter/golang"
)

type GoParser struct{}

var goLang = sitter.NewLanguage(golang.Language())

func (g *GoParser) Parse(source []byte, language string) ([]Chunk, error) {
    p := sitter.NewParser()
    p.SetLanguage(goLang)
    tree, err := p.ParseCtx(context.Background(), nil, source)
    if err != nil {
        return nil, fmt.Errorf("go parser: %w", err)
    }
    return extractChunks(source, tree.RootNode(), goLang, goPatterns)
}

var goPatterns = []struct {
    query    string
    nodeType string
}{
    {`(function_declaration name: (identifier) @name) @decl`, "function_declaration"},
    {`(method_declaration name: (field_identifier) @name) @decl`, "method_declaration"},
    {`(type_declaration (type_spec name: (type_identifier) @name)) @decl`, "type_declaration"},
}

func extractChunks(source []byte, root *sitter.Node, lang *sitter.Language, patterns []struct {
    query    string
    nodeType string
}) ([]Chunk, error) {
    var chunks []Chunk
    for _, pat := range patterns {
        q, err := sitter.NewQuery([]byte(pat.query), lang)
        if err != nil {
            return nil, fmt.Errorf("bad query %q: %w", pat.query, err)
        }
        qc := sitter.NewQueryCursor()
        qc.Exec(q, root)

        for {
            m, ok := qc.NextMatch()
            if !ok {
                break
            }
            var name string
            var declNode *sitter.Node
            for _, c := range m.Captures {
                switch q.CaptureNameForId(c.Index) {
                case "name":
                    name = c.Node.Content(source)
                case "decl":
                    declNode = c.Node
                }
            }
            if declNode == nil || name == "" {
                continue
            }
            chunks = append(chunks, Chunk{
                Name:      name,
                NodeType:  pat.nodeType,
                Language:  "go",
                StartLine: int(declNode.StartPoint().Row) + 1,
                EndLine:   int(declNode.EndPoint().Row) + 1,
                StartByte: int(declNode.StartByte()),
                Text:      string(source[declNode.StartByte():declNode.EndByte()]),
            })
        }
    }
    return chunks, nil
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/parser/ -run TestGoParser -v
```
Expected: both `TestGoParser_*` tests PASS.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add internal/parser/go.go internal/parser/parser_test.go
git commit -m "feat: Go language parser using tree-sitter"
```

---

## Task 5: TypeScript/JavaScript Parser

**Files:**
- Modify: `internal/parser/typescript.go`
- Modify: `internal/parser/parser_test.go`

- [ ] **Step 1: Add failing tests**

Append to `internal/parser/parser_test.go`:

```go
func TestTypeScriptParser_Symbols(t *testing.T) {
    src, err := os.ReadFile("../../testdata/fixtures/sample.ts")
    if err != nil {
        t.Fatal(err)
    }

    p := &parser.TypeScriptParser{}
    chunks, err := p.Parse(src, "typescript")
    if err != nil {
        t.Fatalf("Parse() error = %v", err)
    }

    names := make(map[string]bool)
    for _, c := range chunks {
        names[c.Name] = true
    }

    for _, want := range []string{"greet", "UserService", "getUser", "deleteUser", "formatEmail"} {
        if !names[want] {
            t.Errorf("missing symbol %q; got %v", want, chunkNames(chunks))
        }
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/parser/ -run TestTypeScriptParser -v
```
Expected: FAIL (empty chunks from stub)

- [ ] **Step 3: Implement `internal/parser/typescript.go`**

```go
package parser

import (
    "context"
    "fmt"

    sitter "github.com/smacker/go-tree-sitter"
    "github.com/smacker/go-tree-sitter/typescript/typescript"
)

type TypeScriptParser struct{}

var tsLang = sitter.NewLanguage(typescript.Language())

func (t *TypeScriptParser) Parse(source []byte, language string) ([]Chunk, error) {
    p := sitter.NewParser()
    p.SetLanguage(tsLang)
    tree, err := p.ParseCtx(context.Background(), nil, source)
    if err != nil {
        return nil, fmt.Errorf("typescript parser: %w", err)
    }
    return extractChunks(source, tree.RootNode(), tsLang, tsPatterns)
}

var tsPatterns = []struct {
    query    string
    nodeType string
}{
    {`(function_declaration name: (identifier) @name) @decl`, "function_declaration"},
    {`(class_declaration name: (type_identifier) @name) @decl`, "class_declaration"},
    {`(method_definition name: (property_identifier) @name) @decl`, "method_definition"},
    // const foo = () => {} or const foo = function() {}
    {`(lexical_declaration (variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)])) @decl`, "arrow_function"},
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/parser/ -run TestTypeScriptParser -v
```
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add internal/parser/typescript.go internal/parser/parser_test.go
git commit -m "feat: TypeScript/JavaScript parser using tree-sitter"
```

---

## Task 6: Java Parser

**Files:**
- Modify: `internal/parser/java.go`
- Modify: `internal/parser/parser_test.go`

- [ ] **Step 1: Add failing tests**

Append to `internal/parser/parser_test.go`:

```go
func TestJavaParser_Symbols(t *testing.T) {
    src, err := os.ReadFile("../../testdata/fixtures/Sample.java")
    if err != nil {
        t.Fatal(err)
    }

    p := &parser.JavaParser{}
    chunks, err := p.Parse(src, "java")
    if err != nil {
        t.Fatalf("Parse() error = %v", err)
    }

    names := make(map[string]bool)
    for _, c := range chunks {
        names[c.Name] = true
    }

    for _, want := range []string{"UserService", "getUser", "validateId", "Repository"} {
        if !names[want] {
            t.Errorf("missing symbol %q; got %v", want, chunkNames(chunks))
        }
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/parser/ -run TestJavaParser -v
```
Expected: FAIL

- [ ] **Step 3: Implement `internal/parser/java.go`**

```go
package parser

import (
    "context"
    "fmt"

    sitter "github.com/smacker/go-tree-sitter"
    "github.com/smacker/go-tree-sitter/java"
)

type JavaParser struct{}

var javaLang = sitter.NewLanguage(java.Language())

func (j *JavaParser) Parse(source []byte, language string) ([]Chunk, error) {
    p := sitter.NewParser()
    p.SetLanguage(javaLang)
    tree, err := p.ParseCtx(context.Background(), nil, source)
    if err != nil {
        return nil, fmt.Errorf("java parser: %w", err)
    }
    return extractChunks(source, tree.RootNode(), javaLang, javaPatterns)
}

var javaPatterns = []struct {
    query    string
    nodeType string
}{
    {`(class_declaration name: (identifier) @name) @decl`, "class_declaration"},
    {`(interface_declaration name: (identifier) @name) @decl`, "interface_declaration"},
    {`(method_declaration name: (identifier) @name) @decl`, "method_declaration"},
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/parser/ -run TestJavaParser -v
```
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add internal/parser/java.go internal/parser/parser_test.go
git commit -m "feat: Java parser using tree-sitter"
```

---

## Task 7: Fallback Parser

**Files:**
- Modify: `internal/parser/fallback.go`
- Modify: `internal/parser/parser_test.go`

The fallback parser indexes an entire file as one chunk if it is ≤ 8KB. Files larger than 8KB are skipped (return empty slice, no error).

- [ ] **Step 1: Add failing tests**

Append to `internal/parser/parser_test.go`:

```go
func TestFallbackParser_SmallFile(t *testing.T) {
    src := []byte(`{"key": "value"}`)
    p := &parser.FallbackParser{}
    chunks, err := p.Parse(src, "json")
    if err != nil {
        t.Fatal(err)
    }
    if len(chunks) != 1 {
        t.Fatalf("expected 1 chunk for small file, got %d", len(chunks))
    }
    if chunks[0].Text != string(src) {
        t.Errorf("chunk text mismatch")
    }
    if chunks[0].NodeType != "file" {
        t.Errorf("NodeType = %q, want %q", chunks[0].NodeType, "file")
    }
}

func TestFallbackParser_LargeFileSkipped(t *testing.T) {
    src := make([]byte, 9*1024) // 9KB > 8KB threshold
    p := &parser.FallbackParser{}
    chunks, err := p.Parse(src, "binary")
    if err != nil {
        t.Fatal(err)
    }
    if len(chunks) != 0 {
        t.Errorf("expected 0 chunks for large file, got %d", len(chunks))
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/parser/ -run TestFallbackParser -v
```
Expected: FAIL

- [ ] **Step 3: Implement `internal/parser/fallback.go`**

```go
package parser

const maxFallbackBytes = 8 * 1024

type FallbackParser struct{}

func (f *FallbackParser) Parse(source []byte, language string) ([]Chunk, error) {
    if len(source) > maxFallbackBytes {
        return nil, nil
    }
    return []Chunk{{
        NodeType:  "file",
        Language:  language,
        StartLine: 1,
        EndLine:   countLines(source),
        StartByte: 0,
        Text:      string(source),
    }}, nil
}

func countLines(b []byte) int {
    n := 1
    for _, c := range b {
        if c == '\n' {
            n++
        }
    }
    return n
}
```

- [ ] **Step 4: Run all parser tests**

```bash
go test ./internal/parser/ -v
```
Expected: all tests PASS.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add internal/parser/fallback.go internal/parser/parser_test.go
git commit -m "feat: fallback file-level parser for unsupported file types"
```

---

## Task 8: Ollama Embedder

**Files:**
- Create: `internal/embedder/embedder.go`
- Create: `internal/embedder/ollama.go`
- Create: `internal/embedder/embedder_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/embedder/embedder_test.go
package embedder_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/kovaron/codesearch/internal/embedder"
)

func TestOllamaEmbedder_Success(t *testing.T) {
    want := []float32{0.1, 0.2, 0.3}
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/embed" {
            t.Errorf("unexpected path %q", r.URL.Path)
        }
        resp := map[string]any{
            "embeddings": [][]float32{want},
        }
        json.NewEncoder(w).Encode(resp)
    }))
    defer srv.Close()

    e := embedder.NewOllama(srv.URL, "nomic-embed-text")
    got, err := e.Embed(context.Background(), "hello world")
    if err != nil {
        t.Fatalf("Embed() error = %v", err)
    }
    if len(got) != 3 {
        t.Fatalf("len(embedding) = %d, want 3", len(got))
    }
    for i := range want {
        if got[i] != want[i] {
            t.Errorf("embedding[%d] = %f, want %f", i, got[i], want[i])
        }
    }
}

func TestOllamaEmbedder_ServerError(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "model not loaded", http.StatusServiceUnavailable)
    }))
    defer srv.Close()

    e := embedder.NewOllama(srv.URL, "nomic-embed-text")
    _, err := e.Embed(context.Background(), "hello")
    if err == nil {
        t.Error("Embed() expected error for server error, got nil")
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/embedder/ -v
```
Expected: FAIL — package not found

- [ ] **Step 3: Implement `internal/embedder/embedder.go`**

```go
package embedder

import "context"

// Embedder converts text into a float32 vector.
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
}
```

- [ ] **Step 4: Implement `internal/embedder/ollama.go`**

```go
package embedder

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
)

type OllamaEmbedder struct {
    baseURL string
    model   string
    client  *http.Client
}

func NewOllama(baseURL, model string) *OllamaEmbedder {
    return &OllamaEmbedder{
        baseURL: baseURL,
        model:   model,
        client:  &http.Client{},
    }
}

func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
    body, _ := json.Marshal(map[string]string{
        "model": o.model,
        "input": text,
    })
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embed", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := o.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("ollama: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("ollama: unexpected status %d", resp.StatusCode)
    }

    var result struct {
        Embeddings [][]float32 `json:"embeddings"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("ollama: decode response: %w", err)
    }
    if len(result.Embeddings) == 0 {
        return nil, fmt.Errorf("ollama: empty embeddings in response")
    }
    return result.Embeddings[0], nil
}
```

- [ ] **Step 5: Run tests to confirm pass**

```bash
go test ./internal/embedder/ -v
```
Expected: both tests PASS.

- [ ] **Step 6: Format and commit**

```bash
go fmt ./...
git add internal/embedder/
git commit -m "feat: Ollama embedder with /api/embed HTTP client"
```

---

## Task 9: Qdrant Store

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/qdrant.go`
- Create: `internal/store/store_test.go`

Integration tests require Docker. Run with `-tags integration`.

- [ ] **Step 1: Write failing tests**

```go
// internal/store/store_test.go
//go:build integration

package store_test

import (
    "context"
    "testing"

    "github.com/kovaron/codesearch/internal/parser"
    "github.com/kovaron/codesearch/internal/store"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/wait"
)

func startQdrant(t *testing.T) (host string, grpcPort int, cleanup func()) {
    t.Helper()
    ctx := context.Background()
    req := testcontainers.ContainerRequest{
        Image:        "qdrant/qdrant:v1.9.0",
        ExposedPorts: []string{"6333/tcp", "6334/tcp"},
        WaitingFor:   wait.ForHTTP("/healthz").WithPort("6333"),
    }
    c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: req,
        Started:          true,
    })
    if err != nil {
        t.Fatalf("start qdrant: %v", err)
    }
    p, err := c.MappedPort(ctx, "6334")
    if err != nil {
        t.Fatalf("mapped port: %v", err)
    }
    return "localhost", p.Int(), func() { c.Terminate(ctx) }
}

func TestQdrantStore_UpsertAndSearch(t *testing.T) {
    host, port, cleanup := startQdrant(t)
    defer cleanup()

    ctx := context.Background()
    s, err := store.NewQdrant(ctx, host, port, "test-project", 3)
    if err != nil {
        t.Fatalf("NewQdrant: %v", err)
    }

    chunk := parser.Chunk{
        Name:      "HandleLogin",
        NodeType:  "function_declaration",
        Language:  "go",
        StartLine: 10,
        EndLine:   20,
        StartByte: 100,
        Text:      "func HandleLogin() {}",
    }
    vec := []float32{0.1, 0.2, 0.3}

    if err := s.Upsert(ctx, "pkg/auth.go", chunk, vec); err != nil {
        t.Fatalf("Upsert: %v", err)
    }

    results, err := s.SearchSemantic(ctx, vec, 10)
    if err != nil {
        t.Fatalf("SearchSemantic: %v", err)
    }
    if len(results) != 1 {
        t.Fatalf("expected 1 result, got %d", len(results))
    }
    if results[0].Name != "HandleLogin" {
        t.Errorf("Name = %q, want %q", results[0].Name, "HandleLogin")
    }
}

func TestQdrantStore_DeleteByFile(t *testing.T) {
    host, port, cleanup := startQdrant(t)
    defer cleanup()

    ctx := context.Background()
    s, err := store.NewQdrant(ctx, host, port, "test-del", 3)
    if err != nil {
        t.Fatal(err)
    }

    chunk := parser.Chunk{Name: "Foo", NodeType: "function_declaration", Language: "go", StartByte: 0}
    if err := s.Upsert(ctx, "foo.go", chunk, []float32{0.1, 0.2, 0.3}); err != nil {
        t.Fatal(err)
    }
    if err := s.DeleteByFile(ctx, "foo.go"); err != nil {
        t.Fatal(err)
    }

    results, err := s.SearchSemantic(ctx, []float32{0.1, 0.2, 0.3}, 10)
    if err != nil {
        t.Fatal(err)
    }
    if len(results) != 0 {
        t.Errorf("expected 0 results after delete, got %d", len(results))
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -tags integration ./internal/store/ -v
```
Expected: FAIL — package not found

- [ ] **Step 3: Implement `internal/store/store.go`**

```go
package store

import (
    "context"

    "github.com/kovaron/codesearch/internal/parser"
)

// SearchResult is a chunk returned from a store query.
type SearchResult struct {
    Filepath  string
    Name      string
    NodeType  string
    Language  string
    StartLine int
    EndLine   int
    Text      string
    Score     float32
}

// Store is the persistence layer for indexed chunks.
type Store interface {
    Upsert(ctx context.Context, filepath string, chunk parser.Chunk, vector []float32) error
    DeleteByFile(ctx context.Context, filepath string) error
    SearchSemantic(ctx context.Context, vector []float32, limit int) ([]SearchResult, error)
    SearchStructural(ctx context.Context, name, nodeType, language string, limit int) ([]SearchResult, error)
    ListByPath(ctx context.Context, pathPrefix string, limit int) ([]SearchResult, error)
    GetByName(ctx context.Context, filepath, name string) (*SearchResult, error)
    WriteHeartbeat(ctx context.Context) error
    HeartbeatAge(ctx context.Context) (int64, error) // seconds since last heartbeat; -1 if never
}
```

- [ ] **Step 4: Implement `internal/store/qdrant.go`**

```go
package store

import (
    "context"
    "crypto/sha256"
    "encoding/binary"
    "fmt"
    "strings"
    "time"

    "github.com/kovaron/codesearch/internal/parser"
    "github.com/qdrant/go-client/qdrant"
)

const heartbeatPointID = uint64(0) // reserved ID for daemon heartbeat

type QdrantStore struct {
    client     *qdrant.Client
    collection string
    dim        uint64
}

func NewQdrant(ctx context.Context, host string, port int, project string, dim int) (*QdrantStore, error) {
    client, err := qdrant.NewClient(&qdrant.Config{
        Host: host,
        Port: port,
    })
    if err != nil {
        return nil, fmt.Errorf("qdrant client: %w", err)
    }

    s := &QdrantStore{client: client, collection: project, dim: uint64(dim)}
    if err := s.ensureCollection(ctx); err != nil {
        return nil, err
    }
    return s, nil
}

func (s *QdrantStore) ensureCollection(ctx context.Context) error {
    exists, err := s.client.CollectionExists(ctx, s.collection)
    if err != nil {
        return fmt.Errorf("qdrant CollectionExists: %w", err)
    }
    if exists {
        return nil
    }
    return s.client.CreateCollection(ctx, &qdrant.CreateCollection{
        CollectionName: s.collection,
        VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
            Size:     s.dim,
            Distance: qdrant.Distance_Cosine,
        }),
    })
}

func chunkID(filepath, nodeType string, startByte int) uint64 {
    h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", filepath, nodeType, startByte)))
    id := binary.BigEndian.Uint64(h[:8])
    if id == heartbeatPointID {
        id = 1 // avoid collision with reserved ID
    }
    return id
}

func (s *QdrantStore) Upsert(ctx context.Context, filepath string, chunk parser.Chunk, vector []float32) error {
    id := chunkID(filepath, chunk.NodeType, chunk.StartByte)
    _, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
        CollectionName: s.collection,
        Points: []*qdrant.PointStruct{{
            Id:      qdrant.NewIDNum(id),
            Vectors: qdrant.NewVectors(vector...),
            Payload: qdrant.NewValueMap(map[string]any{
                "filepath":   filepath,
                "name":       chunk.Name,
                "node_type":  chunk.NodeType,
                "language":   chunk.Language,
                "start_line": int64(chunk.StartLine),
                "end_line":   int64(chunk.EndLine),
                "text":       chunk.Text,
            }),
        }},
    })
    return err
}

func (s *QdrantStore) DeleteByFile(ctx context.Context, filepath string) error {
    _, err := s.client.Delete(ctx, &qdrant.DeletePoints{
        CollectionName: s.collection,
        Points: qdrant.NewPointsSelectorFilter(&qdrant.Filter{
            Must: []*qdrant.Condition{
                qdrant.NewMatchKeyword("filepath", filepath),
            },
        }),
    })
    return err
}

func (s *QdrantStore) SearchSemantic(ctx context.Context, vector []float32, limit int) ([]SearchResult, error) {
    results, err := s.client.Search(ctx, &qdrant.SearchPoints{
        CollectionName: s.collection,
        Vector:         vector,
        Limit:          uint64(limit),
        WithPayload:    qdrant.NewWithPayload(true),
        Filter: &qdrant.Filter{
            MustNot: []*qdrant.Condition{
                qdrant.NewMatchKeyword("node_type", "__heartbeat__"),
            },
        },
    })
    if err != nil {
        return nil, err
    }
    return scoredPointsToResults(results), nil
}

func (s *QdrantStore) SearchStructural(ctx context.Context, name, nodeType, language string, limit int) ([]SearchResult, error) {
    var must []*qdrant.Condition
    if name != "" {
        must = append(must, qdrant.NewMatchKeyword("name", name))
    }
    if nodeType != "" {
        must = append(must, qdrant.NewMatchKeyword("node_type", nodeType))
    }
    if language != "" {
        must = append(must, qdrant.NewMatchKeyword("language", language))
    }

    results, err := s.client.Scroll(ctx, &qdrant.ScrollPoints{
        CollectionName: s.collection,
        Filter:         &qdrant.Filter{Must: must},
        WithPayload:    qdrant.NewWithPayload(true),
        Limit:          qdrant.PtrOf(uint64(limit)),
    })
    if err != nil {
        return nil, err
    }
    return scrollPointsToResults(results), nil
}

func (s *QdrantStore) ListByPath(ctx context.Context, pathPrefix string, limit int) ([]SearchResult, error) {
    // NewMatchText does tokenized keyword matching on the filepath field.
    // For exact prefix matching, store a separate "dirpath" payload field (the directory
    // component) and filter on that instead. This is a known limitation — exact subtree
    // filtering may return false positives for very short path prefixes.
    results, err := s.client.Scroll(ctx, &qdrant.ScrollPoints{
        CollectionName: s.collection,
        Filter: &qdrant.Filter{
            Must: []*qdrant.Condition{
                qdrant.NewMatchText("filepath", pathPrefix),
            },
        },
        WithPayload: qdrant.NewWithPayload(true),
        Limit:       qdrant.PtrOf(uint64(limit)),
    })
    if err != nil {
        return nil, err
    }
    return scrollPointsToResults(results), nil
}

func (s *QdrantStore) GetByName(ctx context.Context, filepath, name string) (*SearchResult, error) {
    results, err := s.client.Scroll(ctx, &qdrant.ScrollPoints{
        CollectionName: s.collection,
        Filter: &qdrant.Filter{
            Must: []*qdrant.Condition{
                qdrant.NewMatchKeyword("filepath", filepath),
                qdrant.NewMatchKeyword("name", name),
            },
        },
        WithPayload: qdrant.NewWithPayload(true),
        Limit:       qdrant.PtrOf(uint64(1)),
    })
    if err != nil {
        return nil, err
    }
    if len(results) == 0 {
        return nil, nil
    }
    r := scrollPointsToResults(results)
    return &r[0], nil
}

func (s *QdrantStore) WriteHeartbeat(ctx context.Context) error {
    _, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
        CollectionName: s.collection,
        Points: []*qdrant.PointStruct{{
            Id:      qdrant.NewIDNum(heartbeatPointID),
            Vectors: qdrant.NewVectors(make([]float32, s.dim)...),
            Payload: qdrant.NewValueMap(map[string]any{
                "node_type":  "__heartbeat__",
                "last_seen":  time.Now().Unix(),
            }),
        }},
    })
    return err
}

func (s *QdrantStore) HeartbeatAge(ctx context.Context) (int64, error) {
    pts, err := s.client.Get(ctx, &qdrant.GetPoints{
        CollectionName: s.collection,
        Ids:            []*qdrant.PointId{qdrant.NewIDNum(heartbeatPointID)},
        WithPayload:    qdrant.NewWithPayload(true),
    })
    if err != nil || len(pts) == 0 {
        return -1, err
    }
    ts := pts[0].Payload["last_seen"].GetIntegerValue()
    return time.Now().Unix() - ts, nil
}

func payloadToResult(p map[string]*qdrant.Value, score float32) SearchResult {
    return SearchResult{
        Filepath:  p["filepath"].GetStringValue(),
        Name:      p["name"].GetStringValue(),
        NodeType:  p["node_type"].GetStringValue(),
        Language:  p["language"].GetStringValue(),
        StartLine: int(p["start_line"].GetIntegerValue()),
        EndLine:   int(p["end_line"].GetIntegerValue()),
        Text:      p["text"].GetStringValue(),
        Score:     score,
    }
}

func scoredPointsToResults(pts []*qdrant.ScoredPoint) []SearchResult {
    out := make([]SearchResult, 0, len(pts))
    for _, pt := range pts {
        out = append(out, payloadToResult(pt.Payload, pt.Score))
    }
    return out
}

func scrollPointsToResults(pts []*qdrant.RetrievedPoint) []SearchResult {
    out := make([]SearchResult, 0, len(pts))
    for _, pt := range pts {
        if strings.HasPrefix(pt.Payload["node_type"].GetStringValue(), "__") {
            continue
        }
        out = append(out, payloadToResult(pt.Payload, 0))
    }
    return out
}
```

- [ ] **Step 5: Run integration tests**

```bash
go test -tags integration ./internal/store/ -v
```
Expected: both tests PASS (requires Docker running).

- [ ] **Step 6: Format and commit**

```bash
go fmt ./...
git add internal/store/
git commit -m "feat: Qdrant store with upsert, semantic/structural search, delete, heartbeat"
```

---

## Task 10: Indexer

**Files:**
- Create: `internal/indexer/indexer.go`
- Create: `internal/indexer/indexer_test.go`

The indexer orchestrates: read file → parse → embed → upsert to store. On delete: remove all points for the file.

- [ ] **Step 1: Write failing tests**

```go
// internal/indexer/indexer_test.go
//go:build integration

package indexer_test

import (
    "context"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/kovaron/codesearch/internal/embedder"
    "github.com/kovaron/codesearch/internal/indexer"
    "github.com/kovaron/codesearch/internal/parser"
    "github.com/kovaron/codesearch/internal/store"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/wait"
)

func startQdrant(t *testing.T) (string, int, func()) {
    t.Helper()
    ctx := context.Background()
    req := testcontainers.ContainerRequest{
        Image:        "qdrant/qdrant:v1.9.0",
        ExposedPorts: []string{"6333/tcp", "6334/tcp"},
        WaitingFor:   wait.ForHTTP("/healthz").WithPort("6333"),
    }
    c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: req,
        Started:          true,
    })
    if err != nil {
        t.Fatal(err)
    }
    p, _ := c.MappedPort(ctx, "6334")
    return "localhost", p.Int(), func() { c.Terminate(ctx) }
}

func TestIndexer_IndexAndDelete(t *testing.T) {
    host, port, cleanup := startQdrant(t)
    defer cleanup()

    // Mock Ollama returning a 3-dim embedding
    ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
    }))
    defer ollamaSrv.Close()

    ctx := context.Background()
    s, _ := store.NewQdrant(ctx, host, port, "idx-test", 3)
    e := embedder.NewOllama(ollamaSrv.URL, "nomic-embed-text")
    reg := parser.NewRegistry()
    idx := indexer.New(reg, e, s)

    // Write a temp Go file
    tmp, err := os.CreateTemp(t.TempDir(), "*.go")
    if err != nil {
        t.Fatal(err)
    }
    tmp.WriteString("package main\nfunc Hello() {}\n")
    tmp.Close()

    if err := idx.IndexFile(ctx, tmp.Name(), "go"); err != nil {
        t.Fatalf("IndexFile: %v", err)
    }

    results, err := s.SearchStructural(ctx, "Hello", "function_declaration", "go", 10)
    if err != nil {
        t.Fatal(err)
    }
    if len(results) != 1 {
        t.Fatalf("expected 1 result after indexing, got %d", len(results))
    }

    if err := idx.DeleteFile(ctx, tmp.Name()); err != nil {
        t.Fatalf("DeleteFile: %v", err)
    }

    results, err = s.SearchStructural(ctx, "Hello", "", "", 10)
    if err != nil {
        t.Fatal(err)
    }
    if len(results) != 0 {
        t.Errorf("expected 0 results after delete, got %d", len(results))
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -tags integration ./internal/indexer/ -v
```
Expected: FAIL — package not found

- [ ] **Step 3: Implement `internal/indexer/indexer.go`**

```go
package indexer

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "github.com/kovaron/codesearch/internal/embedder"
    "github.com/kovaron/codesearch/internal/parser"
    "github.com/kovaron/codesearch/internal/store"
)

type Indexer struct {
    registry *parser.Registry
    embedder embedder.Embedder
    store    store.Store
}

func New(registry *parser.Registry, emb embedder.Embedder, st store.Store) *Indexer {
    return &Indexer{registry: registry, embedder: emb, store: st}
}

func (idx *Indexer) IndexFile(ctx context.Context, filepath, language string) error {
    source, err := os.ReadFile(filepath)
    if err != nil {
        return fmt.Errorf("read %s: %w", filepath, err)
    }

    p, _ := idx.registry.For(filepath)
    chunks, err := p.Parse(source, language)
    if err != nil {
        return fmt.Errorf("parse %s: %w", filepath, err)
    }

    // Remove stale points for this file before upserting new ones
    if err := idx.store.DeleteByFile(ctx, filepath); err != nil {
        return fmt.Errorf("delete stale points for %s: %w", filepath, err)
    }

    for _, chunk := range chunks {
        vec, err := idx.embedder.Embed(ctx, chunk.Text)
        if err != nil {
            return fmt.Errorf("embed chunk %q in %s: %w", chunk.Name, filepath, err)
        }
        if err := idx.store.Upsert(ctx, filepath, chunk, vec); err != nil {
            return fmt.Errorf("upsert chunk %q: %w", chunk.Name, err)
        }
    }
    return nil
}

func (idx *Indexer) DeleteFile(ctx context.Context, fp string) error {
    return idx.store.DeleteByFile(ctx, fp)
}

func languageForExt(filename string) string {
    switch filepath.Ext(filename) {
    case ".go":
        return "go"
    case ".ts", ".tsx":
        return "typescript"
    case ".js", ".jsx":
        return "javascript"
    case ".java":
        return "java"
    default:
        return "unknown"
    }
}

// LanguageFor returns the language string for a given filename.
func LanguageFor(filename string) string {
    return languageForExt(filename)
}
```

- [ ] **Step 4: Run integration test**

```bash
go test -tags integration ./internal/indexer/ -v
```
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add internal/indexer/
git commit -m "feat: indexer orchestrating parse → embed → store pipeline"
```

---

## Task 11: Watcher Daemon

**Files:**
- Create: `internal/watcher/watcher.go`
- Create: `internal/watcher/watcher_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/watcher/watcher_test.go
package watcher_test

import (
    "context"
    "os"
    "path/filepath"
    "sync/atomic"
    "testing"
    "time"

    "github.com/kovaron/codesearch/internal/watcher"
)

func TestDebouncer(t *testing.T) {
    d := watcher.NewDebouncer(50 * time.Millisecond)
    var calls atomic.Int32
    fn := func() { calls.Add(1) }

    // Fire 5 rapid events — debouncer should collapse to 1 call
    for i := 0; i < 5; i++ {
        d.Add("key", fn)
    }
    time.Sleep(150 * time.Millisecond)

    if n := calls.Load(); n != 1 {
        t.Errorf("expected 1 debounced call, got %d", n)
    }
}

func TestWatcher_DetectsFileWrite(t *testing.T) {
    dir := t.TempDir()
    file := filepath.Join(dir, "test.go")
    os.WriteFile(file, []byte("package main\n"), 0644)

    var detected atomic.Bool
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    w, err := watcher.New([]string{dir}, func(path string, deleted bool) {
        if path == file && !deleted {
            detected.Store(true)
        }
    })
    if err != nil {
        t.Fatal(err)
    }
    go w.Run(ctx)

    time.Sleep(100 * time.Millisecond) // let watcher initialize
    os.WriteFile(file, []byte("package main\nfunc Foo() {}\n"), 0644)
    time.Sleep(500 * time.Millisecond)

    if !detected.Load() {
        t.Error("watcher did not detect file write")
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/watcher/ -v
```
Expected: FAIL — package not found

- [ ] **Step 3: Implement `internal/watcher/watcher.go`**

```go
package watcher

import (
    "context"
    "log"
    "sync"
    "time"

    "github.com/fsnotify/fsnotify"
)

// Handler is called when a file changes. deleted=true means the file was removed.
type Handler func(path string, deleted bool)

// Debouncer collapses rapid repeated events on the same key into one call.
type Debouncer struct {
    duration time.Duration
    mu       sync.Mutex
    timers   map[string]*time.Timer
}

func NewDebouncer(d time.Duration) *Debouncer {
    return &Debouncer{duration: d, timers: make(map[string]*time.Timer)}
}

func (d *Debouncer) Add(key string, fn func()) {
    d.mu.Lock()
    defer d.mu.Unlock()
    if t, ok := d.timers[key]; ok {
        t.Reset(d.duration)
        return
    }
    d.timers[key] = time.AfterFunc(d.duration, func() {
        d.mu.Lock()
        delete(d.timers, key)
        d.mu.Unlock()
        fn()
    })
}

// Watcher watches directories and invokes a handler on file changes.
type Watcher struct {
    fsw      *fsnotify.Watcher
    handler  Handler
    debounce *Debouncer
}

func New(dirs []string, handler Handler) (*Watcher, error) {
    fsw, err := fsnotify.NewWatcher()
    if err != nil {
        return nil, err
    }
    for _, dir := range dirs {
        if err := fsw.Add(dir); err != nil {
            fsw.Close()
            return nil, err
        }
    }
    return &Watcher{
        fsw:      fsw,
        handler:  handler,
        debounce: NewDebouncer(200 * time.Millisecond),
    }, nil
}

func (w *Watcher) Run(ctx context.Context) {
    defer w.fsw.Close()
    for {
        select {
        case <-ctx.Done():
            return
        case event, ok := <-w.fsw.Events:
            if !ok {
                return
            }
            path := event.Name
            if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
                w.debounce.Add(path, func() { w.handler(path, true) })
            } else if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
                w.debounce.Add(path, func() { w.handler(path, false) })
            }
        case err, ok := <-w.fsw.Errors:
            if !ok {
                return
            }
            log.Printf("watcher error: %v", err)
        }
    }
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/watcher/ -v
```
Expected: both tests PASS.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add internal/watcher/
git commit -m "feat: fsnotify-based file watcher with 200ms debouncer"
```

---

## Task 12: MCP Server

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/tools.go`
- Create: `internal/mcp/mcp_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/mcp/mcp_test.go
package mcp_test

import (
    "context"
    "testing"

    internalmcp "github.com/kovaron/codesearch/internal/mcp"
    "github.com/kovaron/codesearch/internal/parser"
    "github.com/kovaron/codesearch/internal/store"
)

type fakeStore struct {
    results []store.SearchResult
}

func (f *fakeStore) Upsert(ctx context.Context, fp string, chunk parser.Chunk, vec []float32) error {
    return nil
}
func (f *fakeStore) DeleteByFile(ctx context.Context, fp string) error { return nil }
func (f *fakeStore) SearchSemantic(ctx context.Context, vec []float32, limit int) ([]store.SearchResult, error) {
    return f.results, nil
}
func (f *fakeStore) SearchStructural(ctx context.Context, name, nodeType, language string, limit int) ([]store.SearchResult, error) {
    return f.results, nil
}
func (f *fakeStore) ListByPath(ctx context.Context, pathPrefix string, limit int) ([]store.SearchResult, error) {
    return f.results, nil
}
func (f *fakeStore) GetByName(ctx context.Context, fp, name string) (*store.SearchResult, error) {
    if len(f.results) > 0 {
        return &f.results[0], nil
    }
    return nil, nil
}
func (f *fakeStore) WriteHeartbeat(ctx context.Context) error  { return nil }
func (f *fakeStore) HeartbeatAge(ctx context.Context) (int64, error) { return 5, nil }

func TestNewServer_NotNil(t *testing.T) {
    fs := &fakeStore{results: []store.SearchResult{
        {Name: "HandleLogin", Filepath: "pkg/auth.go", NodeType: "function_declaration"},
    }}
    srv := internalmcp.NewServer(fs, &fakeEmbedder{})
    if srv == nil {
        t.Error("NewServer() returned nil")
    }
}

type fakeEmbedder struct{}

func (f *fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
    return []float32{0.1, 0.2, 0.3}, nil
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/mcp/ -v
```
Expected: FAIL — package not found

- [ ] **Step 3: Implement `internal/mcp/server.go`**

```go
package mcp

import (
    "github.com/kovaron/codesearch/internal/embedder"
    "github.com/kovaron/codesearch/internal/store"
    "github.com/mark3labs/mcp-go/server"
)

func NewServer(st store.Store, emb embedder.Embedder) *server.MCPServer {
    s := server.NewMCPServer("codesearch", "1.0.0",
        server.WithToolCapabilities(true),
    )
    registerTools(s, st, emb)
    return s
}

func Serve(st store.Store, emb embedder.Embedder) error {
    s := NewServer(st, emb)
    return server.ServeStdio(s)
}
```

- [ ] **Step 4: Implement `internal/mcp/tools.go`**

```go
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
    s.AddTool(
        mcp.NewTool("search_semantic",
            mcp.WithDescription("Search code by natural language query using vector similarity"),
            mcp.WithString("query", mcp.Required(), mcp.Description("Natural language search query")),
            mcp.WithString("project", mcp.Required(), mcp.Description("Project name from .codesearch.yaml")),
            mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
        ),
        func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
            query := req.Params.Arguments["query"].(string)
            limit := 10
            if l, ok := req.Params.Arguments["limit"].(float64); ok {
                limit = int(l)
            }
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

    s.AddTool(
        mcp.NewTool("search_structural",
            mcp.WithDescription("Search code by exact or prefix symbol name"),
            mcp.WithString("query", mcp.Required(), mcp.Description("Symbol name to search")),
            mcp.WithString("project", mcp.Required()),
            mcp.WithString("type", mcp.Description("Node type filter: function, method, class")),
            mcp.WithString("language", mcp.Description("Language filter: go, typescript, java")),
            mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
        ),
        func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
            name := req.Params.Arguments["query"].(string)
            nodeType, _ := req.Params.Arguments["type"].(string)
            language, _ := req.Params.Arguments["language"].(string)
            limit := 20
            if l, ok := req.Params.Arguments["limit"].(float64); ok {
                limit = int(l)
            }
            results, err := st.SearchStructural(ctx, name, nodeType, language, limit)
            if err != nil {
                return nil, fmt.Errorf("search_structural: %w", err)
            }
            return jsonResult(results)
        },
    )

    s.AddTool(
        mcp.NewTool("list_symbols",
            mcp.WithDescription("List all indexed symbols in a file or directory"),
            mcp.WithString("project", mcp.Required()),
            mcp.WithString("filepath", mcp.Required(), mcp.Description("File path or directory prefix")),
            mcp.WithNumber("limit", mcp.Description("Max results (default 200)")),
        ),
        func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
            fp := req.Params.Arguments["filepath"].(string)
            limit := 200
            if l, ok := req.Params.Arguments["limit"].(float64); ok {
                limit = int(l)
            }
            results, err := st.ListByPath(ctx, fp, limit)
            if err != nil {
                return nil, fmt.Errorf("list_symbols: %w", err)
            }
            return jsonResult(results)
        },
    )

    s.AddTool(
        mcp.NewTool("get_chunk",
            mcp.WithDescription("Get the full source text of a specific symbol"),
            mcp.WithString("project", mcp.Required()),
            mcp.WithString("filepath", mcp.Required()),
            mcp.WithString("name", mcp.Required(), mcp.Description("Symbol name")),
        ),
        func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
            fp := req.Params.Arguments["filepath"].(string)
            name := req.Params.Arguments["name"].(string)
            result, err := st.GetByName(ctx, fp, name)
            if err != nil {
                return nil, fmt.Errorf("get_chunk: %w", err)
            }
            if result == nil {
                return mcp.NewToolResultText(`{"error":"symbol not found"}`), nil
            }
            return jsonResult(result)
        },
    )

    s.AddTool(
        mcp.NewTool("index_status",
            mcp.WithDescription("Get indexing status and daemon liveness"),
            mcp.WithString("project", mcp.Required()),
        ),
        func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
            age, err := st.HeartbeatAge(ctx)
            if err != nil {
                return nil, fmt.Errorf("index_status: %w", err)
            }
            running := age >= 0 && age < 30
            status := map[string]any{
                "daemon_running":       running,
                "heartbeat_age_secs":  age,
            }
            b, _ := json.Marshal(status)
            return mcp.NewToolResultText(string(b)), nil
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
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/mcp/ -v
```
Expected: `TestNewServer_NotNil` PASS.

- [ ] **Step 6: Format and commit**

```bash
go fmt ./...
git add internal/mcp/
git commit -m "feat: MCP stdio server with 5 tools (search_semantic, search_structural, list_symbols, get_chunk, index_status)"
```

---

## Task 13: CLI Subcommands — `init`, `daemon`, `mcp`

**Files:**
- Modify: `cmd/codesearch/main.go`
- Create: `cmd/codesearch/cmd_init.go`
- Create: `cmd/codesearch/cmd_daemon.go`
- Create: `cmd/codesearch/cmd_mcp.go`

- [ ] **Step 1: Implement `cmd/codesearch/cmd_init.go`**

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "init [dir]",
        Short: "Generate .codesearch.yaml and run initial full index",
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            dir := "."
            if len(args) > 0 {
                dir = args[0]
            }
            cfgPath := filepath.Join(dir, ".codesearch.yaml")
            if _, err := os.Stat(cfgPath); err == nil {
                fmt.Fprintln(cmd.OutOrStdout(), ".codesearch.yaml already exists — skipping generation")
                return nil
            }
            base := filepath.Base(dir)
            if base == "." {
                base, _ = os.Getwd()
                base = filepath.Base(base)
            }
            content := fmt.Sprintf(`project: %s
languages: [go, typescript, java]
include: ["**/*"]
exclude: ["vendor/**", "node_modules/**", ".git/**"]
qdrant_url: http://localhost:6334
ollama_url: http://localhost:11434
ollama_model: nomic-embed-text
workers: 4
`, base)
            if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
                return err
            }
            fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", cfgPath)
            fmt.Fprintln(cmd.OutOrStdout(), "Run 'codesearch daemon' to start indexing.")
            return nil
        },
    }
}
```

- [ ] **Step 2: Implement `cmd/codesearch/cmd_daemon.go`**

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "path/filepath"
    "syscall"
    "time"

    "github.com/kovaron/codesearch/internal/config"
    "github.com/kovaron/codesearch/internal/embedder"
    "github.com/kovaron/codesearch/internal/indexer"
    "github.com/kovaron/codesearch/internal/parser"
    "github.com/kovaron/codesearch/internal/store"
    "github.com/kovaron/codesearch/internal/watcher"
    "github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "daemon [dir]",
        Short: "Watch files and keep the index up to date",
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            dir := "."
            if len(args) > 0 {
                dir = args[0]
            }
            cfg, err := config.Load(dir)
            if err != nil {
                return err
            }

            ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
            defer stop()

            qdrantHost, qdrantPort := parseQdrantURL(cfg.QdrantURL)
            st, err := store.NewQdrant(ctx, qdrantHost, qdrantPort, cfg.Project, 768)
            if err != nil {
                return err
            }

            emb := embedder.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
            reg := parser.NewRegistry()
            idx := indexer.New(reg, emb, st)

            // Heartbeat goroutine
            go func() {
                t := time.NewTicker(15 * time.Second)
                defer t.Stop()
                for {
                    select {
                    case <-ctx.Done():
                        return
                    case <-t.C:
                        _ = st.WriteHeartbeat(ctx)
                    }
                }
            }()
            _ = st.WriteHeartbeat(ctx)

            handler := func(path string, deleted bool) {
                rel, _ := filepath.Rel(dir, path)
                if deleted {
                    log.Printf("delete %s", rel)
                    if err := idx.DeleteFile(ctx, path); err != nil {
                        log.Printf("delete error: %v", err)
                    }
                    return
                }
                log.Printf("index %s", rel)
                if err := idx.IndexFile(ctx, path, indexer.LanguageFor(path)); err != nil {
                    log.Printf("index error: %v", err)
                }
            }

            w, err := watcher.New([]string{dir}, handler)
            if err != nil {
                return err
            }

            log.Printf("daemon: watching %s for project %q", dir, cfg.Project)
            w.Run(ctx)
            log.Println("daemon: stopped")
            return nil
        },
    }
}

// parseQdrantURL extracts host and gRPC port from a URL like http://localhost:6334
func parseQdrantURL(rawURL string) (string, int) {
    // Default
    host, port := "localhost", 6334
    // Simple split — full URL parsing not needed for typical configs
    var h string
    var p int
    if n, _ := fmt.Sscanf(rawURL, "http://%s", &h); n == 1 {
        // h is "localhost:6334"
        fmt.Sscanf(h, "%[^:]:%d", &host, &port)
    }
    if port == 0 {
        port = 6334
    }
    return host, port
}
```

Add `"fmt"` to imports in cmd_daemon.go.

- [ ] **Step 3: Implement `cmd/codesearch/cmd_mcp.go`**

```go
package main

import (
    "context"

    "github.com/kovaron/codesearch/internal/config"
    "github.com/kovaron/codesearch/internal/embedder"
    internalmcp "github.com/kovaron/codesearch/internal/mcp"
    "github.com/kovaron/codesearch/internal/store"
    "github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "mcp",
        Short: "Start stdio MCP server for AI agent integration",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg, err := config.Load(".")
            if err != nil {
                return err
            }
            ctx := context.Background()
            qdrantHost, qdrantPort := parseQdrantURL(cfg.QdrantURL)
            st, err := store.NewQdrant(ctx, qdrantHost, qdrantPort, cfg.Project, 768)
            if err != nil {
                return err
            }
            emb := embedder.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
            return internalmcp.Serve(st, emb)
        },
    }
}
```

- [ ] **Step 4: Wire subcommands in `cmd/codesearch/main.go`**

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

func main() {
    root := &cobra.Command{
        Use:   "codesearch",
        Short: "Code indexing and search tool for AI agents",
    }
    root.AddCommand(
        newInitCmd(),
        newDaemonCmd(),
        newMCPCmd(),
    )
    if err := root.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

- [ ] **Step 5: Build to verify**

```bash
go build ./cmd/codesearch/
./codesearch --help
```
Expected: help text showing `init`, `daemon`, `mcp` subcommands.

- [ ] **Step 6: Format and commit**

```bash
go fmt ./...
git add cmd/codesearch/
git commit -m "feat: CLI subcommands init, daemon, mcp"
```

---

## Task 14: Export / Import Archive

**Files:**
- Create: `pkg/archive/archive.go`
- Create: `pkg/archive/archive_test.go`
- Create: `cmd/codesearch/cmd_export.go`
- Create: `cmd/codesearch/cmd_import.go`
- Modify: `cmd/codesearch/main.go`

The `.csi` archive is a gzip-compressed tar containing:
- `manifest.json` — project name, codesearch version, export timestamp
- `qdrant-snapshot.bin` — raw bytes of the Qdrant snapshot downloaded via REST
- `meta.json` — file checksums at export time (future use for delta import)

Qdrant snapshot REST endpoints (port 6333, not 6334):
- `POST /collections/{name}/snapshots` → `{"result":{"name":"snapshot-xxx.snapshot"}}`
- `GET /collections/{name}/snapshots/{snapshot-name}` → binary download
- `PUT /collections/{name}/snapshots/upload?collection_name={name}` → restore

- [ ] **Step 1: Write failing tests**

```go
// pkg/archive/archive_test.go
package archive_test

import (
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/kovaron/codesearch/pkg/archive"
)

func TestManifestRoundTrip(t *testing.T) {
    dir := t.TempDir()
    outPath := filepath.Join(dir, "test.csi")

    m := archive.Manifest{
        Project:   "test-project",
        Version:   "1.0.0",
        ExportedAt: time.Now().UTC().Truncate(time.Second),
    }

    // Write a fake snapshot
    snapshotBytes := []byte("fake qdrant snapshot data")

    if err := archive.Write(outPath, m, snapshotBytes); err != nil {
        t.Fatalf("Write: %v", err)
    }

    gotManifest, gotSnapshot, err := archive.Read(outPath)
    if err != nil {
        t.Fatalf("Read: %v", err)
    }
    if gotManifest.Project != m.Project {
        t.Errorf("Project = %q, want %q", gotManifest.Project, m.Project)
    }
    if string(gotSnapshot) != string(snapshotBytes) {
        t.Errorf("snapshot bytes mismatch")
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./pkg/archive/ -v
```
Expected: FAIL — package not found

- [ ] **Step 3: Implement `pkg/archive/archive.go`**

```go
package archive

import (
    "archive/tar"
    "compress/gzip"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "time"
)

const Version = "1.0.0"

type Manifest struct {
    Project    string    `json:"project"`
    Version    string    `json:"version"`
    ExportedAt time.Time `json:"exported_at"`
}

// Write creates a .csi archive at path with the given manifest and Qdrant snapshot bytes.
func Write(path string, m Manifest, snapshot []byte) error {
    f, err := os.Create(path)
    if err != nil {
        return err
    }
    defer f.Close()

    gz := gzip.NewWriter(f)
    defer gz.Close()
    tw := tar.NewWriter(gz)
    defer tw.Close()

    manifestBytes, err := json.Marshal(m)
    if err != nil {
        return err
    }
    if err := writeEntry(tw, "manifest.json", manifestBytes); err != nil {
        return err
    }
    if err := writeEntry(tw, "qdrant-snapshot.bin", snapshot); err != nil {
        return err
    }
    return nil
}

func writeEntry(tw *tar.Writer, name string, data []byte) error {
    hdr := &tar.Header{
        Name: name,
        Mode: 0644,
        Size: int64(len(data)),
    }
    if err := tw.WriteHeader(hdr); err != nil {
        return err
    }
    _, err := tw.Write(data)
    return err
}

// Read extracts the manifest and snapshot bytes from a .csi archive.
func Read(path string) (Manifest, []byte, error) {
    f, err := os.Open(path)
    if err != nil {
        return Manifest{}, nil, err
    }
    defer f.Close()

    gz, err := gzip.NewReader(f)
    if err != nil {
        return Manifest{}, nil, err
    }
    defer gz.Close()

    tr := tar.NewReader(gz)
    var m Manifest
    var snapshot []byte

    for {
        hdr, err := tr.Next()
        if err == io.EOF {
            break
        }
        if err != nil {
            return Manifest{}, nil, fmt.Errorf("read archive: %w", err)
        }
        data, err := io.ReadAll(tr)
        if err != nil {
            return Manifest{}, nil, err
        }
        switch hdr.Name {
        case "manifest.json":
            if err := json.Unmarshal(data, &m); err != nil {
                return Manifest{}, nil, err
            }
        case "qdrant-snapshot.bin":
            snapshot = data
        }
    }
    return m, snapshot, nil
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./pkg/archive/ -v
```
Expected: `TestManifestRoundTrip` PASS.

- [ ] **Step 5: Implement `cmd/codesearch/cmd_export.go`**

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    "github.com/kovaron/codesearch/internal/config"
    "github.com/kovaron/codesearch/pkg/archive"
    "github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "export <output.csi>",
        Short: "Export index snapshot to a portable .csi archive",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            outPath := args[0]
            cfg, err := config.Load(".")
            if err != nil {
                return err
            }
            restURL := strings.Replace(cfg.QdrantURL, "6334", "6333", 1)

            // Create snapshot
            resp, err := http.Post(restURL+"/collections/"+cfg.Project+"/snapshots", "application/json", nil)
            if err != nil {
                return fmt.Errorf("create snapshot: %w", err)
            }
            defer resp.Body.Close()
            var snapResult struct {
                Result struct {
                    Name string `json:"name"`
                } `json:"result"`
            }
            if err := json.NewDecoder(resp.Body).Decode(&snapResult); err != nil {
                return fmt.Errorf("parse snapshot response: %w", err)
            }

            // Download snapshot
            dlResp, err := http.Get(restURL + "/collections/" + cfg.Project + "/snapshots/" + snapResult.Result.Name)
            if err != nil {
                return fmt.Errorf("download snapshot: %w", err)
            }
            defer dlResp.Body.Close()
            snapBytes, err := io.ReadAll(dlResp.Body)
            if err != nil {
                return err
            }

            m := archive.Manifest{
                Project:    cfg.Project,
                Version:    archive.Version,
                ExportedAt: time.Now().UTC(),
            }
            if err := archive.Write(outPath, m, snapBytes); err != nil {
                return err
            }
            fmt.Fprintf(cmd.OutOrStdout(), "Exported %d bytes to %s\n", len(snapBytes), outPath)
            return nil
        },
    }
}
```

- [ ] **Step 6: Implement `cmd/codesearch/cmd_import.go`**

```go
package main

import (
    "bytes"
    "fmt"
    "net/http"
    "strings"

    "github.com/kovaron/codesearch/internal/config"
    "github.com/kovaron/codesearch/pkg/archive"
    "github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "import <input.csi>",
        Short: "Restore index from a .csi archive",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            inPath := args[0]
            cfg, err := config.Load(".")
            if err != nil {
                return err
            }
            restURL := strings.Replace(cfg.QdrantURL, "6334", "6333", 1)

            m, snapBytes, err := archive.Read(inPath)
            if err != nil {
                return fmt.Errorf("read archive: %w", err)
            }
            fmt.Fprintf(cmd.OutOrStdout(), "Importing snapshot for project %q (exported at %s)\n",
                m.Project, m.ExportedAt.Format("2006-01-02 15:04:05 UTC"))

            uploadURL := restURL + "/collections/" + cfg.Project + "/snapshots/upload?collection_name=" + cfg.Project
            req, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(snapBytes))
            if err != nil {
                return err
            }
            req.Header.Set("Content-Type", "application/octet-stream")
            resp, err := http.DefaultClient.Do(req)
            if err != nil {
                return fmt.Errorf("upload snapshot: %w", err)
            }
            defer resp.Body.Close()
            if resp.StatusCode >= 400 {
                return fmt.Errorf("qdrant returned status %d during upload", resp.StatusCode)
            }
            fmt.Fprintln(cmd.OutOrStdout(), "Import complete. Run 'codesearch daemon' to catch up on changes.")
            return nil
        },
    }
}
```

- [ ] **Step 7: Wire export/import into main.go**

```go
root.AddCommand(
    newInitCmd(),
    newDaemonCmd(),
    newMCPCmd(),
    newExportCmd(),
    newImportCmd(),
)
```

- [ ] **Step 8: Build and verify**

```bash
go build ./cmd/codesearch/
./codesearch --help
```
Expected: help shows `init`, `daemon`, `mcp`, `export`, `import`.

- [ ] **Step 9: Format and commit**

```bash
go fmt ./...
git add pkg/archive/ cmd/codesearch/
git commit -m "feat: export/import .csi archive with Qdrant snapshot"
```

---

## Task 15: End-to-End Integration Test + Final Verification

**Files:**
- Create: `internal/indexer/e2e_test.go`

This test starts Qdrant via testcontainers, mocks Ollama, indexes a Go fixture, runs a structural search, and verifies the result.

- [ ] **Step 1: Write the end-to-end test**

```go
// internal/indexer/e2e_test.go
//go:build integration

package indexer_test

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/kovaron/codesearch/internal/embedder"
    "github.com/kovaron/codesearch/internal/indexer"
    "github.com/kovaron/codesearch/internal/parser"
    "github.com/kovaron/codesearch/internal/store"
)

func TestEndToEnd_IndexAndSearch(t *testing.T) {
    host, port, cleanup := startQdrant(t)
    defer cleanup()

    ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
    }))
    defer ollamaSrv.Close()

    ctx := context.Background()
    st, err := store.NewQdrant(ctx, host, port, "e2e-test", 3)
    if err != nil {
        t.Fatal(err)
    }

    emb := embedder.NewOllama(ollamaSrv.URL, "nomic-embed-text")
    reg := parser.NewRegistry()
    idx := indexer.New(reg, emb, st)

    if err := idx.IndexFile(ctx, "../../testdata/fixtures/sample.go", "go"); err != nil {
        t.Fatalf("IndexFile: %v", err)
    }

    // Structural search for known symbol
    results, err := st.SearchStructural(ctx, "Add", "function_declaration", "go", 10)
    if err != nil {
        t.Fatal(err)
    }
    if len(results) == 0 {
        t.Fatal("expected at least 1 result for 'Add', got 0")
    }
    if results[0].Name != "Add" {
        t.Errorf("Name = %q, want %q", results[0].Name, "Add")
    }
    if results[0].StartLine == 0 {
        t.Error("StartLine should not be 0")
    }

    // Semantic search (vector is same mock for all chunks; just verify it returns results)
    semantic, err := st.SearchSemantic(ctx, []float32{0.1, 0.2, 0.3}, 5)
    if err != nil {
        t.Fatal(err)
    }
    if len(semantic) == 0 {
        t.Error("expected semantic search results, got 0")
    }
}
```

- [ ] **Step 2: Run end-to-end test**

```bash
go test -tags integration ./internal/indexer/ -run TestEndToEnd -v
```
Expected: PASS.

- [ ] **Step 3: Run all non-integration tests**

```bash
go test ./...
```
Expected: all packages PASS with no failures.

- [ ] **Step 4: Run all integration tests**

```bash
go test -tags integration ./...
```
Expected: all packages PASS (requires Docker).

- [ ] **Step 5: Final format pass**

```bash
go fmt ./...
go vet ./...
```
Expected: no output (no format or vet issues).

- [ ] **Step 6: Final commit**

```bash
git add internal/indexer/e2e_test.go
git commit -m "test: end-to-end integration test for full index and search pipeline"
```

---

## MCP Registration (Claude Code / Claude Desktop)

After the binary is built, register it in your MCP config:

**Claude Code** — add to `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "codesearch": {
      "command": "/path/to/codesearch",
      "args": ["mcp"],
      "cwd": "/your/project/root"
    }
  }
}
```

**Claude Desktop** — add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "codesearch": {
      "command": "/path/to/codesearch",
      "args": ["mcp"],
      "cwd": "/your/project/root"
    }
  }
}
```

---

## Prerequisites

Before running:

```bash
# Start Qdrant
docker run -p 6333:6333 -p 6334:6334 qdrant/qdrant:v1.9.0

# Start Ollama and pull embedding model
ollama pull nomic-embed-text
```
