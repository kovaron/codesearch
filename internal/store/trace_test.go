package store

import (
	"context"
	"testing"

	"github.com/kovaron/codesearch/internal/parser"
)

// traceTestStore is a local fake Store for isolation.
type traceTestStore struct {
	listed     []SearchResult
	structural []SearchResult
}

func (f *traceTestStore) Upsert(_ context.Context, _ string, _ parser.Chunk, _ []float32) error {
	return nil
}
func (f *traceTestStore) DeleteByFile(_ context.Context, _ string) error { return nil }
func (f *traceTestStore) SearchSemantic(_ context.Context, _ []float32, _ int) ([]SearchResult, error) {
	return nil, nil
}
func (f *traceTestStore) SearchStructural(_ context.Context, name, _, _ string, _ int) ([]SearchResult, error) {
	var out []SearchResult
	for _, r := range f.structural {
		if r.Name == name {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *traceTestStore) ListByPath(_ context.Context, _ string, _ int) ([]SearchResult, error) {
	return f.listed, nil
}
func (f *traceTestStore) GetByName(_ context.Context, _, _ string) (*SearchResult, error) {
	return nil, nil
}
func (f *traceTestStore) WriteHeartbeat(_ context.Context) error        { return nil }
func (f *traceTestStore) HeartbeatAge(_ context.Context) (int64, error) { return 5, nil }

// TestFindCallers_FindsMatchesExcludingDef verifies that call sites are
// returned and the definition chunk itself is excluded.
func TestFindCallers_FindsMatchesExcludingDef(t *testing.T) {
	t.Parallel()
	chunks := []SearchResult{
		{Name: "Foo", Text: "func Foo() { return 0 }", Filepath: "foo.go"},   // definition — excluded
		{Name: "Bar", Text: "func Bar() { Foo() }", Filepath: "bar.go"},      // caller 1
		{Name: "Baz", Text: "func Baz() { x := Foo() }", Filepath: "baz.go"}, // caller 2
		{Name: "Qux", Text: "func Qux() { other() }", Filepath: "qux.go"},    // not a caller
	}
	st := &traceTestStore{listed: chunks}
	results, err := FindCallers(context.Background(), st, "Foo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %v", len(results), results)
	}
	for _, r := range results {
		if r.Name == "Foo" {
			t.Error("definition chunk must be excluded from callers")
		}
	}
}

// TestFindCallers_RespectsLimit verifies the limit parameter is honoured.
func TestFindCallers_RespectsLimit(t *testing.T) {
	t.Parallel()
	chunks := []SearchResult{
		{Name: "A", Text: "Target()", Filepath: "a.go"},
		{Name: "B", Text: "Target()", Filepath: "b.go"},
		{Name: "C", Text: "Target()", Filepath: "c.go"},
		{Name: "D", Text: "Target()", Filepath: "d.go"},
		{Name: "E", Text: "Target()", Filepath: "e.go"},
	}
	st := &traceTestStore{listed: chunks}
	results, err := FindCallers(context.Background(), st, "Target", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) > 2 {
		t.Fatalf("got %d results, limit=2", len(results))
	}
}

// TestFindCallees_ParsesCallsAndSkipsKeywords verifies that function call
// identifiers are extracted from the body, keyword calls are filtered, and
// resolvable definitions are returned.
func TestFindCallees_ParsesCallsAndSkipsKeywords(t *testing.T) {
	t.Parallel()
	// Body contains: return Foo(bar) + len(xs)
	// Foo should be resolved; len is a keyword and must be skipped.
	body := "func MyFunc() { return Foo(bar) + len(xs) }"
	myFuncDef := SearchResult{Name: "MyFunc", Filepath: "main.go", Text: body}
	fooDef := SearchResult{Name: "Foo", Filepath: "foo.go", Text: "func Foo() {}"}
	st := &traceTestStore{
		// structural holds both so SearchStructural can find MyFunc (the target)
		// and Foo (the callee).
		structural: []SearchResult{myFuncDef, fooDef},
		listed:     []SearchResult{myFuncDef},
	}
	results, err := FindCallees(context.Background(), st, "MyFunc", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Foo should be present
	var foundFoo bool
	for _, r := range results {
		if r.Name == "Foo" {
			foundFoo = true
		}
		if r.Name == "len" || r.Name == "return" {
			t.Errorf("keyword %q must not appear in callees", r.Name)
		}
	}
	if !foundFoo {
		t.Errorf("expected Foo in callees, got: %v", results)
	}
}

// TestFindCallees_EmptyWhenNoDef verifies that a nil slice is returned when
// the target symbol is not in the index.
func TestFindCallees_EmptyWhenNoDef(t *testing.T) {
	t.Parallel()
	st := &traceTestStore{} // no structural entries
	results, err := FindCallees(context.Background(), st, "Missing", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}
