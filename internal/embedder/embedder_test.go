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
