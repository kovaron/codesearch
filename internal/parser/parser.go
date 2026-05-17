package parser

import "path/filepath"

// Chunk represents a single indexed code unit (function, method, class, or file).
type Chunk struct {
	Name      string // symbol name; empty for file-level chunks
	NodeType  string // "function_declaration", "method_declaration", etc.
	Language  string
	StartLine int
	EndLine   int
	StartByte int
	Text      string
}

// Parser extracts Chunks from source bytes.
type Parser interface {
	Parse(source []byte, language string) ([]Chunk, error)
}

// Registry maps file extensions to parsers.
type Registry struct {
	parsers  map[string]Parser
	fallback Parser
}

func NewRegistry() *Registry {
	r := &Registry{
		parsers:  make(map[string]Parser),
		fallback: &FallbackParser{},
	}
	goP := &GoParser{}
	r.parsers[".go"] = goP

	tsP := &TypeScriptParser{}
	r.parsers[".ts"] = tsP
	r.parsers[".tsx"] = tsP
	r.parsers[".js"] = tsP
	r.parsers[".jsx"] = tsP

	javaP := &JavaParser{}
	r.parsers[".java"] = javaP

	return r
}

// For returns a Parser for filename. The boolean is always true; it exists
// for ergonomic `if p, ok := reg.For(name); ok { ... }` style and to signal
// future API evolution. The fallback parser handles unknown extensions.
func (r *Registry) For(filename string) (Parser, bool) {
	ext := filepath.Ext(filename)
	p, ok := r.parsers[ext]
	if !ok {
		return r.fallback, true
	}
	return p, ok
}
