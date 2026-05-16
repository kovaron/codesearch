package parser

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

type TypeScriptParser struct{}

var tsLang = typescript.GetLanguage()

func (t *TypeScriptParser) Parse(source []byte, language string) ([]Chunk, error) {
	p := sitter.NewParser()
	p.SetLanguage(tsLang)
	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, fmt.Errorf("typescript parser: %w", err)
	}
	return extractChunks(source, tree.RootNode(), tsLang, tsPatterns)
}

var tsPatterns = []struct {
	query    string
	nodeType string
}{
	{`(function_declaration name: (identifier) @name) @decl`, "function_declaration"},
	{`(class_declaration name: (type_identifier) @name) @decl`, "class_declaration"},
	{`(method_definition name: (property_identifier) @name) @decl`, "method_definition"},
	{`(lexical_declaration (variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)])) @decl`, "arrow_function"},
}
