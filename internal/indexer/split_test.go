package indexer

import (
	"strings"
	"testing"

	"github.com/kovaron/codesearch/internal/parser"
)

func TestSplitChunk_SmallChunkUnchanged(t *testing.T) {
	t.Parallel()
	c := parser.Chunk{
		Name:      "Foo",
		NodeType:  "function_declaration",
		Language:  "go",
		StartLine: 10,
		EndLine:   12,
		StartByte: 100,
		Text:      "func Foo() {\n\treturn 1\n}\n",
	}
	parts := SplitChunk(c, 1024)
	if len(parts) != 1 {
		t.Fatalf("want 1 part for small chunk, got %d", len(parts))
	}
	if parts[0] != c {
		t.Errorf("expected chunk passed through unchanged")
	}
}

func TestSplitChunk_BigChunkSplitsAtLines(t *testing.T) {
	t.Parallel()
	// 20 lines × ~100 bytes = ~2000 bytes; split at 500 → expect ~4 parts.
	line := strings.Repeat("x", 99) + "\n"
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString(line)
	}
	c := parser.Chunk{
		Name:      "Big",
		NodeType:  "function_declaration",
		Language:  "go",
		StartLine: 1,
		EndLine:   20,
		StartByte: 0,
		Text:      sb.String(),
	}
	parts := SplitChunk(c, 500)
	if len(parts) < 2 {
		t.Fatalf("expected multiple parts, got %d", len(parts))
	}

	// Every part keeps the identity of the original.
	for i, p := range parts {
		if p.Name != c.Name {
			t.Errorf("part %d: Name=%q want %q", i, p.Name, c.Name)
		}
		if p.NodeType != c.NodeType || p.Language != c.Language {
			t.Errorf("part %d: NodeType/Language drifted", i)
		}
		if len(p.Text) > 500+100 { // allow one-line overshoot
			t.Errorf("part %d: text len %d exceeds maxBytes+1 line", i, len(p.Text))
		}
		if strings.TrimSpace(p.Text) == "" {
			t.Errorf("part %d: empty text", i)
		}
	}

	// Distinct StartByte across parts (ensures distinct Qdrant point IDs).
	seen := map[int]bool{}
	for _, p := range parts {
		if seen[p.StartByte] {
			t.Errorf("duplicate StartByte %d across parts", p.StartByte)
		}
		seen[p.StartByte] = true
	}

	// Concatenating all part texts reconstructs the original.
	var joined strings.Builder
	for _, p := range parts {
		joined.WriteString(p.Text)
	}
	if joined.String() != c.Text {
		t.Errorf("concat of parts != original (lengths %d vs %d)", joined.Len(), len(c.Text))
	}

	// StartByte offsets are contiguous and absolute (relative to the
	// original file, not to the chunk).
	expected := c.StartByte
	for i, p := range parts {
		if p.StartByte != expected {
			t.Errorf("part %d: StartByte=%d want %d", i, p.StartByte, expected)
		}
		expected += len(p.Text)
	}

	// Line ranges add up.
	if parts[0].StartLine != c.StartLine {
		t.Errorf("first part StartLine=%d want %d", parts[0].StartLine, c.StartLine)
	}
	last := parts[len(parts)-1]
	if last.EndLine != c.EndLine {
		t.Errorf("last part EndLine=%d want %d", last.EndLine, c.EndLine)
	}
}

func TestSplitChunk_SingleOversizedLineIsolated(t *testing.T) {
	t.Parallel()
	// One huge line surrounded by normal lines. The huge line should land in
	// its own part rather than being merged with a neighbour.
	huge := strings.Repeat("x", 2000)
	c := parser.Chunk{
		Name:      "OneLong",
		NodeType:  "function_declaration",
		Language:  "go",
		StartLine: 1,
		EndLine:   3,
		StartByte: 0,
		Text:      "short\n" + huge + "\nshort\n",
	}
	parts := SplitChunk(c, 500)
	if len(parts) < 2 {
		t.Fatalf("expected at least 2 parts, got %d", len(parts))
	}
	// At least one part should contain the huge line by itself.
	found := false
	for _, p := range parts {
		if strings.Contains(p.Text, huge) {
			found = true
			break
		}
	}
	if !found {
		t.Error("oversized line did not survive into a part")
	}
}
