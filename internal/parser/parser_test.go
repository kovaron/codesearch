package parser_test

import (
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
