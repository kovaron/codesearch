package parser_test

import (
	"os"
	"testing"

	"github.com/kovaron/codesearch/internal/parser"
)

func TestRegistryReturnsParserForGo(t *testing.T) {
	reg := parser.NewRegistry()
	p, ok := reg.For("sample.go")
	if !ok {
		t.Fatal("expected parser for .go file, got none")
	}
	if p == nil {
		t.Fatal("parser is nil")
	}
}

func TestRegistryReturnsParserForTS(t *testing.T) {
	reg := parser.NewRegistry()
	_, ok := reg.For("sample.ts")
	if !ok {
		t.Fatal("expected parser for .ts file, got none")
	}
}

func TestRegistryReturnsFallbackForUnknown(t *testing.T) {
	reg := parser.NewRegistry()
	p, ok := reg.For("config.yaml")
	if !ok {
		t.Fatal("expected fallback parser for .yaml file, got none")
	}
	if p == nil {
		t.Fatal("fallback parser is nil")
	}
}

func TestGoParser_Functions(t *testing.T) {
	src, err := os.ReadFile("../../testdata/fixtures/sample.go")
	if err != nil {
		t.Fatal(err)
	}

	p := &parser.GoParser{}
	chunks, err := p.Parse(src, "go")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	names := make(map[string]bool)
	for _, c := range chunks {
		names[c.Name] = true
	}

	for _, want := range []string{"Add", "Greet", "Area", "Perimeter"} {
		if !names[want] {
			t.Errorf("missing symbol %q in parsed chunks; got %v", want, chunkNames(chunks))
		}
	}
}

func TestGoParser_LineNumbers(t *testing.T) {
	src := []byte(`package main

func Foo() int {
    return 1
}
`)
	p := &parser.GoParser{}
	chunks, err := p.Parse(src, "go")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Name != "Foo" {
		t.Errorf("Name = %q, want %q", chunks[0].Name, "Foo")
	}
	if chunks[0].StartLine != 3 {
		t.Errorf("StartLine = %d, want 3", chunks[0].StartLine)
	}
}

func TestTypeScriptParser_Symbols(t *testing.T) {
	src, err := os.ReadFile("../../testdata/fixtures/sample.ts")
	if err != nil {
		t.Fatal(err)
	}

	p := &parser.TypeScriptParser{}
	chunks, err := p.Parse(src, "typescript")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	names := make(map[string]bool)
	for _, c := range chunks {
		names[c.Name] = true
	}

	for _, want := range []string{"greet", "UserService", "getUser", "deleteUser", "formatEmail"} {
		if !names[want] {
			t.Errorf("missing symbol %q; got %v", want, chunkNames(chunks))
		}
	}
}

func chunkNames(chunks []parser.Chunk) []string {
	var names []string
	for _, c := range chunks {
		names = append(names, c.Name)
	}
	return names
}
