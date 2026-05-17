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
func (f *fakeStore) WriteHeartbeat(ctx context.Context) error        { return nil }
func (f *fakeStore) HeartbeatAge(ctx context.Context) (int64, error) { return 5, nil }

type fakeEmbedder struct{}

func (f *fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func TestNewServer_NotNil(t *testing.T) {
	fs := &fakeStore{results: []store.SearchResult{
		{Name: "HandleLogin", Filepath: "pkg/auth.go", NodeType: "function_declaration"},
	}}
	srv := internalmcp.NewServer(fs, &fakeEmbedder{})
	if srv == nil {
		t.Error("NewServer() returned nil")
	}
}
