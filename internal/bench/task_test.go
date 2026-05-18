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

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
