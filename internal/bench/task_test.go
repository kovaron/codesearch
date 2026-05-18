package bench

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTask_Valid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.yaml")
	writeFile(t, path, `
id: find-foo
kind: search
prompt: "Find foo."
arms: [codesearch, baseline]
turn_cap: 20
timeout_seconds: 120
golden:
  type: answer_match
  expected: ["a.go:1"]
  match: set_equal
`)
	task, err := LoadTask(path)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if task.ID != "find-foo" {
		t.Errorf("ID=%q want find-foo", task.ID)
	}
	if task.Kind != "search" {
		t.Errorf("Kind=%q want search", task.Kind)
	}
	if task.TurnCap != 20 {
		t.Errorf("TurnCap=%d want 20", task.TurnCap)
	}
	if task.Golden.Type != "answer_match" || task.Golden.Match != "set_equal" {
		t.Errorf("Golden=%+v", task.Golden)
	}
	if len(task.Golden.Expected) != 1 || task.Golden.Expected[0] != "a.go:1" {
		t.Errorf("Expected=%v", task.Golden.Expected)
	}
}

func TestLoadTask_MissingID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	writeFile(t, path, `kind: search
prompt: x
golden: {type: answer_match, expected: [], match: set_equal}
`)
	if _, err := LoadTask(path); err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestLoadTask_UnknownField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "extra.yaml")
	writeFile(t, path, `
id: x
kind: search
prompt: x
arms: [codesearch]
turn_cap: 5
timeout_seconds: 5
unknown_field: oops
golden: {type: answer_match, expected: [a], match: set_equal}
`)
	if _, err := LoadTask(path); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestLoadTask_BadGoldenType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bg.yaml")
	writeFile(t, path, `
id: x
kind: search
prompt: x
arms: [codesearch]
turn_cap: 5
timeout_seconds: 5
golden: {type: nope, expected: [a], match: set_equal}
`)
	if _, err := LoadTask(path); err == nil {
		t.Fatal("expected error for bad golden.type")
	}
}

func TestLoadTask_EmptyArms(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "noarms.yaml")
	writeFile(t, path, `
id: x
kind: search
prompt: x
turn_cap: 5
timeout_seconds: 5
golden: {type: answer_match, expected: [a], match: substring}
`)
	if _, err := LoadTask(path); err == nil {
		t.Fatal("expected error for empty arms")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestEvaluateGolden_AnswerMatchSetEqual(t *testing.T) {
	t.Parallel()
	g := Golden{Type: "answer_match", Match: "set_equal", Expected: []string{"a.go:1", "b.go:2"}}
	if ok, _ := EvaluateGolden(g, "b.go:2\na.go:1\n", ""); !ok {
		t.Fatal("expected match on reordered set")
	}
	if ok, _ := EvaluateGolden(g, "a.go:1\n", ""); ok {
		t.Fatal("missing entry should fail")
	}
}

func TestEvaluateGolden_AnswerMatchSubstring(t *testing.T) {
	t.Parallel()
	g := Golden{Type: "answer_match", Match: "substring", Expected: []string{"NewIndexer"}}
	if ok, _ := EvaluateGolden(g, "use NewIndexer here", ""); !ok {
		t.Fatal("expected substring match")
	}
	if ok, _ := EvaluateGolden(g, "nothing here", ""); ok {
		t.Fatal("missing substring should fail")
	}
}

func TestEvaluateGolden_AnswerMatchRegex(t *testing.T) {
	t.Parallel()
	g := Golden{Type: "answer_match", Match: "regex", Expected: []string{`^id=\d+$`}}
	if ok, _ := EvaluateGolden(g, "id=42", ""); !ok {
		t.Fatal("expected regex match")
	}
	if ok, _ := EvaluateGolden(g, "id=foo", ""); ok {
		t.Fatal("non-matching regex should fail")
	}
}

func TestEvaluateGolden_FileExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	g := Golden{Type: "file_exists", Expected: []string{"foo.txt"}}
	if ok, _ := EvaluateGolden(g, "", dir); ok {
		t.Fatal("foo.txt should not exist yet")
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, _ := EvaluateGolden(g, "", dir); !ok {
		t.Fatal("foo.txt should exist")
	}
}

func TestEvaluateGolden_UnknownType(t *testing.T) {
	t.Parallel()
	g := Golden{Type: "nope"}
	if ok, _ := EvaluateGolden(g, "", ""); ok {
		t.Fatal("unknown type must not pass")
	}
}
