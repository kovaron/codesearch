package bench

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kovaron/codesearch/internal/parser"
	"github.com/kovaron/codesearch/internal/store"
)

// fakeStore implements store.Store for unit tests.
type fakeStore struct {
	semantic   []store.SearchResult
	structural []store.SearchResult
	listed     []store.SearchResult
	chunk      *store.SearchResult
}

func (f *fakeStore) Upsert(_ context.Context, _ string, _ parser.Chunk, _ []float32) error {
	return nil
}
func (f *fakeStore) DeleteByFile(_ context.Context, _ string) error { return nil }
func (f *fakeStore) SearchSemantic(_ context.Context, _ []float32, _ int) ([]store.SearchResult, error) {
	return f.semantic, nil
}
func (f *fakeStore) SearchStructural(_ context.Context, _, _, _ string, _ int) ([]store.SearchResult, error) {
	return f.structural, nil
}
func (f *fakeStore) ListByPath(_ context.Context, _ string, _ int) ([]store.SearchResult, error) {
	return f.listed, nil
}
func (f *fakeStore) GetByName(_ context.Context, _, _ string) (*store.SearchResult, error) {
	return f.chunk, nil
}
func (f *fakeStore) WriteHeartbeat(_ context.Context) error        { return nil }
func (f *fakeStore) HeartbeatAge(_ context.Context) (int64, error) { return 5, nil }

type fakeEmb struct{}

func (fakeEmb) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2}, nil
}

func TestMCPDispatch_SearchStructural(t *testing.T) {
	t.Parallel()
	d := NewMCPDispatcher("demo", &fakeStore{structural: []store.SearchResult{{Name: "Foo"}}}, fakeEmb{})
	args, _ := json.Marshal(map[string]any{"query": "Foo"})
	out, err := d.Call(context.Background(), "search_structural", args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"Foo"`) {
		t.Errorf("out=%s", out)
	}
}

func TestMCPDispatch_GetChunk(t *testing.T) {
	t.Parallel()
	d := NewMCPDispatcher("demo", &fakeStore{chunk: &store.SearchResult{Text: "func Foo() {}"}}, fakeEmb{})
	args, _ := json.Marshal(map[string]any{"filepath": "a.go", "name": "Foo"})
	out, err := d.Call(context.Background(), "get_chunk", args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"func Foo() {}"`) {
		t.Errorf("out=%s", out)
	}
}

func TestMCPDispatch_UnknownTool(t *testing.T) {
	t.Parallel()
	d := NewMCPDispatcher("demo", &fakeStore{}, fakeEmb{})
	if _, err := d.Call(context.Background(), "nope", []byte("{}")); err == nil || !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("err=%v want ErrUnknownTool", err)
	}
}

func TestMCPDispatch_SearchSemantic(t *testing.T) {
	t.Parallel()
	d := NewMCPDispatcher("demo", &fakeStore{semantic: []store.SearchResult{{Name: "Bar", Score: 0.9}}}, fakeEmb{})
	args, _ := json.Marshal(map[string]any{"query": "bar function"})
	out, err := d.Call(context.Background(), "search_semantic", args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"Bar"`) {
		t.Errorf("out=%s", out)
	}
}

func TestMCPDispatch_ListSymbols(t *testing.T) {
	t.Parallel()
	d := NewMCPDispatcher("demo", &fakeStore{listed: []store.SearchResult{{Name: "MyFunc", Filepath: "main.go"}}}, fakeEmb{})
	args, _ := json.Marshal(map[string]any{"filepath": "main.go"})
	out, err := d.Call(context.Background(), "list_symbols", args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"MyFunc"`) {
		t.Errorf("out=%s", out)
	}
}

func TestMCPDispatch_IndexStatus(t *testing.T) {
	t.Parallel()
	d := NewMCPDispatcher("demo", &fakeStore{}, fakeEmb{})
	out, err := d.Call(context.Background(), "index_status", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"daemon_running"`) {
		t.Errorf("out=%s", out)
	}
	// age=5 < 30 → daemon_running=true
	if !strings.Contains(out, `true`) {
		t.Errorf("expected daemon_running=true in out=%s", out)
	}
}

func TestMCPDispatch_TracePath_Inbound(t *testing.T) {
	t.Parallel()
	listed := []store.SearchResult{
		{Name: "Caller", Text: "func Caller() { Target() }", Filepath: "a.go"},
		{Name: "Target", Text: "func Target() {}", Filepath: "b.go"},
	}
	d := NewMCPDispatcher("demo", &fakeStore{listed: listed}, fakeEmb{})
	args, _ := json.Marshal(map[string]any{"symbol": "Target", "direction": "inbound"})
	out, err := d.Call(context.Background(), "trace_path", args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"Caller"`) {
		t.Errorf("expected Caller in out=%s", out)
	}
}

func TestMCPDispatch_TracePath_InvalidDirection(t *testing.T) {
	t.Parallel()
	d := NewMCPDispatcher("demo", &fakeStore{}, fakeEmb{})
	args, _ := json.Marshal(map[string]any{"symbol": "Foo", "direction": "sideways"})
	if _, err := d.Call(context.Background(), "trace_path", args); err == nil {
		t.Fatal("expected error for invalid direction")
	}
}
