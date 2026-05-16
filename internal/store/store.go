package store

import (
	"context"

	"github.com/kovaron/codesearch/internal/parser"
)

// SearchResult is a single result returned from the store.
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

// Store is the interface for the vector/metadata store.
type Store interface {
	Upsert(ctx context.Context, filepath string, chunk parser.Chunk, vector []float32) error
	DeleteByFile(ctx context.Context, filepath string) error
	SearchSemantic(ctx context.Context, vector []float32, limit int) ([]SearchResult, error)
	SearchStructural(ctx context.Context, name, nodeType, language string, limit int) ([]SearchResult, error)
	ListByPath(ctx context.Context, pathPrefix string, limit int) ([]SearchResult, error)
	GetByName(ctx context.Context, filepath, name string) (*SearchResult, error)
	WriteHeartbeat(ctx context.Context) error
	HeartbeatAge(ctx context.Context) (int64, error)
}
