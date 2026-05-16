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
		Image:        "qdrant/qdrant:v1.13.0",
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
	return "localhost", int(p.Num()), func() { c.Terminate(ctx) }
}

func TestIndexer_IndexAndDelete(t *testing.T) {
	host, port, cleanup := startQdrant(t)
	defer cleanup()

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
