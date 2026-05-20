package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/internal/parser"
	"github.com/kovaron/codesearch/internal/store"
)

// maxChunkBytes is the per-sub-chunk target size before embedding. Set well
// below the embedder's hard truncation cap (~30KB) so we never lose tail.
// Each oversized parser chunk is split into roughly equal parts at line
// boundaries by SplitChunk before being upserted as distinct Qdrant points.
const maxChunkBytes = 20 * 1024

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
		for _, part := range SplitChunk(chunk, maxChunkBytes) {
			vec, err := idx.embedder.Embed(ctx, part.Text)
			if err != nil {
				return fmt.Errorf("embed chunk %q in %s: %w", part.Name, fp, err)
			}
			if err := idx.store.Upsert(ctx, fp, part, vec); err != nil {
				return fmt.Errorf("upsert chunk %q: %w", part.Name, err)
			}
		}
	}
	return nil
}

// SplitChunk splits chunk.Text into one or more sub-chunks each at most
// maxBytes long, splitting only on line boundaries so we never cut a
// statement in half. Returns the original chunk unchanged when its text
// already fits. Each sub-chunk gets a distinct StartByte (so the Qdrant
// point ID is unique) and accurate StartLine/EndLine.
func SplitChunk(chunk parser.Chunk, maxBytes int) []parser.Chunk {
	if len(chunk.Text) <= maxBytes {
		return []parser.Chunk{chunk}
	}

	lines := strings.SplitAfter(chunk.Text, "\n")
	var out []parser.Chunk

	cur := strings.Builder{}
	curStart := chunk.StartLine // 1-based line in original file
	curStartByte := chunk.StartByte
	linesInCur := 0

	flush := func(isLast bool) {
		if cur.Len() == 0 {
			return
		}
		text := cur.String()
		part := parser.Chunk{
			Name:      chunk.Name,
			NodeType:  chunk.NodeType,
			Language:  chunk.Language,
			StartLine: curStart,
			EndLine:   curStart + linesInCur - 1,
			StartByte: curStartByte,
			Text:      text,
		}
		// Ensure EndLine doesn't exceed chunk's original EndLine, even on
		// trailing-newline weirdness.
		if part.EndLine > chunk.EndLine {
			part.EndLine = chunk.EndLine
		}
		out = append(out, part)

		// Advance pointers for the next part.
		curStartByte += len(text)
		curStart += linesInCur
		cur.Reset()
		linesInCur = 0
		_ = isLast
	}

	for _, line := range lines {
		// If the next line would overflow and we already have content, flush.
		if cur.Len() > 0 && cur.Len()+len(line) > maxBytes {
			flush(false)
		}
		cur.WriteString(line)
		linesInCur++

		// A single line longer than maxBytes is the degenerate case — emit it
		// as its own oversized part. The embedder's byte cap will truncate.
		if cur.Len() > maxBytes && linesInCur == 1 {
			flush(false)
		}
	}
	flush(true)

	return out
}

func (idx *Indexer) DeleteFile(ctx context.Context, fp string) error {
	return idx.store.DeleteByFile(ctx, fp)
}

// LanguageFor returns the language string for a given filename.
func LanguageFor(filename string) string {
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
