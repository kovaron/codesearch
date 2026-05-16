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
		Image:        "qdrant/qdrant:v1.13.0",
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
	return "localhost", int(p.Num()), func() { c.Terminate(ctx) }
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
