package parser

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
)

type JavaParser struct{}

var javaLang = java.GetLanguage()

func (j *JavaParser) Parse(source []byte, language string) ([]Chunk, error) {
	p := sitter.NewParser()
	p.SetLanguage(javaLang)
	// ParseCtx with Background — Parser interface is sync; callers expecting
	// cancellation should bound input size or wrap parsing in their own context.
	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, fmt.Errorf("java parser: %w", err)
	}
	return extractChunks(source, tree.RootNode(), javaLang, javaPatterns)
}

var javaPatterns = []struct {
	query    string
	nodeType string
}{
	{`(class_declaration name: (identifier) @name) @decl`, "class_declaration"},
	{`(interface_declaration name: (identifier) @name) @decl`, "interface_declaration"},
	{`(method_declaration name: (identifier) @name) @decl`, "method_declaration"},
}
