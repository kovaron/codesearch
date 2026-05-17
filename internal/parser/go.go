package parser

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

type GoParser struct{}

var goLang = golang.GetLanguage()

func (g *GoParser) Parse(source []byte, language string) ([]Chunk, error) {
	p := sitter.NewParser()
	p.SetLanguage(goLang)
	// ParseCtx with Background — Parser interface is sync; callers expecting
	// cancellation should bound input size or wrap parsing in their own context.
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
