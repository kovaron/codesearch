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

func (idx *Indexer) IndexFile(ctx context.Context, fp, language string) error {
	source, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Errorf("read %s: %w", fp, err)
	}

	p, _ := idx.registry.For(fp)
	chunks, err := p.Parse(source, language)
	if err != nil {
		return fmt.Errorf("parse %s: %w", fp, err)
	}

	if err := idx.store.DeleteByFile(ctx, fp); err != nil {
		return fmt.Errorf("delete stale points for %s: %w", fp, err)
	}

	for _, chunk := range chunks {
		vec, err := idx.embedder.Embed(ctx, chunk.Text)
		if err != nil {
			return fmt.Errorf("embed chunk %q in %s: %w", chunk.Name, fp, err)
		}
		if err := idx.store.Upsert(ctx, fp, chunk, vec); err != nil {
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
