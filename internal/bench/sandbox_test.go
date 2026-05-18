package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneSandbox_CopiesFiles(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), "hello")
	mustWrite(t, filepath.Join(src, "sub", "b.txt"), "world")

	dst, cleanup, err := CloneSandbox(src)
	if err != nil {
		t.Fatalf("CloneSandbox: %v", err)
	}
	defer cleanup()

	got := readFile(t, filepath.Join(dst, "a.txt"))
	if got != "hello" {
		t.Errorf("a.txt=%q", got)
	}
	got = readFile(t, filepath.Join(dst, "sub", "b.txt"))
	if got != "world" {
		t.Errorf("sub/b.txt=%q", got)
	}
}

func TestCloneSandbox_SkipsDotGitAndNodeModules(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, ".git", "HEAD"), "ref: x")
	mustWrite(t, filepath.Join(src, "node_modules", "x", "p.js"), "x")
	mustWrite(t, filepath.Join(src, "keep.go"), "package x")

	dst, cleanup, err := CloneSandbox(src)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		t.Error(".git should not be copied")
	}
	if _, err := os.Stat(filepath.Join(dst, "node_modules")); err == nil {
		t.Error("node_modules should not be copied")
	}
	if _, err := os.Stat(filepath.Join(dst, "keep.go")); err != nil {
		t.Error("keep.go should exist")
	}
}

func TestCloneSandbox_CleanupRemoves(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "x"), "x")
	dst, cleanup, err := CloneSandbox(src)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(dst); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("dst should be removed, stat err=%v", err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
