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

	semantic, err := st.SearchSemantic(ctx, []float32{0.1, 0.2, 0.3}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(semantic) == 0 {
		t.Error("expected semantic search results, got 0")
	}
}
