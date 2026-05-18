package bench

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFile_OK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "hello")
	got, err := ReadFileTool(dir, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestReadFile_RejectsEscape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := ReadFileTool(dir, "../../etc/passwd"); err == nil {
		t.Fatal("expected error on path escape")
	}
}

func TestEditFile_ReplacesUniqueMatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")
	mustWrite(t, p, "func Foo() {}\n")
	if err := EditFileTool(dir, "x.go", "Foo", "Bar"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "func Bar() {}\n" {
		t.Errorf("got %q", string(got))
	}
}

func TestEditFile_ErrorsOnNoMatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "x.go"), "func Foo() {}\n")
	if err := EditFileTool(dir, "x.go", "Zzz", "Yyy"); err == nil {
		t.Fatal("expected error on missing match")
	}
}
