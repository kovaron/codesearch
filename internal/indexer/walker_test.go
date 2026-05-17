package indexer_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/kovaron/codesearch/internal/indexer"
)

func TestWalkFiles_FiltersByExtensionAndNoiseDirs(t *testing.T) {
	dir := t.TempDir()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	must(os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main"), 0644))
	must(os.WriteFile(filepath.Join(dir, "bar.txt"), []byte("text"), 0644))
	must(os.WriteFile(filepath.Join(dir, "README.md"), []byte("# readme"), 0644))

	must(os.MkdirAll(filepath.Join(dir, "sub"), 0755))
	must(os.WriteFile(filepath.Join(dir, "sub", "baz.ts"), []byte("export {}"), 0644))
	must(os.WriteFile(filepath.Join(dir, "sub", "Sample.java"), []byte("class Sample {}"), 0644))

	must(os.MkdirAll(filepath.Join(dir, ".git"), 0755))
	must(os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref"), 0644))
	must(os.WriteFile(filepath.Join(dir, ".git", "config.go"), []byte("package config"), 0644))

	must(os.MkdirAll(filepath.Join(dir, "vendor", "pkg"), 0755))
	must(os.WriteFile(filepath.Join(dir, "vendor", "pkg", "x.go"), []byte("package pkg"), 0644))

	must(os.MkdirAll(filepath.Join(dir, "node_modules"), 0755))
	must(os.WriteFile(filepath.Join(dir, "node_modules", "lib.js"), []byte("module.exports={}"), 0644))

	var got []string
	err := indexer.WalkFiles(dir, func(path, lang string) error {
		rel, _ := filepath.Rel(dir, path)
		got = append(got, rel+":"+lang)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}

	sort.Strings(got)
	want := []string{
		"foo.go:go",
		"sub/Sample.java:java",
		"sub/baz.ts:typescript",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WalkFiles got %v, want %v", got, want)
	}
}

func TestIsNoiseDir(t *testing.T) {
	cases := map[string]bool{
		".git":         true,
		"vendor":       true,
		"node_modules": true,
		".claude":      true,
		".vscode":      true,
		"dist":         true,
		"build":        true,
		"target":       true,
		"src":          false,
		"internal":     false,
		"cmd":          false,
	}
	for name, want := range cases {
		if got := indexer.IsNoiseDir(name); got != want {
			t.Errorf("IsNoiseDir(%q) = %v, want %v", name, got, want)
		}
	}
}
