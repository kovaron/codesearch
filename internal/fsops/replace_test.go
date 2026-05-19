package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

// mkFile creates a file at path (relative to dir) with given content.
func mkFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile reads a file relative to dir.
func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReplaceInFiles_RecursiveGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mkFile(t, dir, "a.go", "nomic-embed-text version")
	mkFile(t, dir, "sub/b.go", "nomic-embed-text again")
	mkFile(t, dir, "sub/deep/c.go", "nomic-embed-text deep")
	mkFile(t, dir, "other.txt", "nomic-embed-text txt") // should not match **/*.go

	changed, total, err := ReplaceInFiles(dir, "**/*.go", "nomic-embed-text", "mxbai-embed-large", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 3 {
		t.Fatalf("want 3 changed files, got %d: %v", len(changed), changed)
	}
	if total != 3 {
		t.Errorf("want total=3, got %d", total)
	}
	// Verify writes happened.
	if got := readFile(t, dir, "a.go"); got != "mxbai-embed-large version" {
		t.Errorf("a.go: %q", got)
	}
	if got := readFile(t, dir, "sub/b.go"); got != "mxbai-embed-large again" {
		t.Errorf("sub/b.go: %q", got)
	}
	if got := readFile(t, dir, "sub/deep/c.go"); got != "mxbai-embed-large deep" {
		t.Errorf("sub/deep/c.go: %q", got)
	}
	// .txt should be unchanged.
	if got := readFile(t, dir, "other.txt"); got != "nomic-embed-text txt" {
		t.Errorf("other.txt was modified: %q", got)
	}
}

func TestReplaceInFiles_DryRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mkFile(t, dir, "a.go", "OLD value")
	mkFile(t, dir, "b/c.go", "OLD value again")

	changed, total, err := ReplaceInFiles(dir, "**/*.go", "OLD", "NEW", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 {
		t.Fatalf("want 2 files in dry-run result, got %d: %v", len(changed), changed)
	}
	if total != 2 {
		t.Errorf("want total=2, got %d", total)
	}
	// Files must be unchanged.
	if got := readFile(t, dir, "a.go"); got != "OLD value" {
		t.Errorf("a.go was modified in dry-run: %q", got)
	}
	if got := readFile(t, dir, "b/c.go"); got != "OLD value again" {
		t.Errorf("b/c.go was modified in dry-run: %q", got)
	}
}

func TestReplaceInFiles_RejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, _, err := ReplaceInFiles(dir, "", "old", "new", false)
	if err == nil {
		t.Error("expected error for empty pattern")
	}

	_, _, err = ReplaceInFiles(dir, "**/*.go", "", "new", false)
	if err == nil {
		t.Error("expected error for empty old")
	}
}

func TestReplaceInFiles_SkipsHeavyDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Files inside heavy dirs should be skipped.
	mkFile(t, dir, ".git/hooks/pre-commit.go", "OLD")
	mkFile(t, dir, "node_modules/x.go", "OLD")
	mkFile(t, dir, "vendor/lib.go", "OLD")
	mkFile(t, dir, ".codesearch/idx.go", "OLD")
	// Normal file should be matched.
	mkFile(t, dir, "main.go", "OLD")

	changed, total, err := ReplaceInFiles(dir, "**/*.go", "OLD", "NEW", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("want 1 changed file (main.go only), got %d: %v", len(changed), changed)
	}
	if changed[0] != "main.go" {
		t.Errorf("want main.go, got %q", changed[0])
	}
	if total != 1 {
		t.Errorf("want total=1, got %d", total)
	}
	// Verify heavy-dir files are unchanged.
	if got := readFile(t, dir, ".git/hooks/pre-commit.go"); got != "OLD" {
		t.Errorf(".git file was modified: %q", got)
	}
}

func TestReplaceInFiles_NonRecursiveGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mkFile(t, dir, "top.go", "OLD")
	mkFile(t, dir, "sub/nested.go", "OLD")

	// "*.go" without "**/" should only match top-level files.
	changed, total, err := ReplaceInFiles(dir, "*.go", "OLD", "NEW", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("want 1 changed file, got %d: %v", len(changed), changed)
	}
	if changed[0] != "top.go" {
		t.Errorf("want top.go, got %q", changed[0])
	}
	if total != 1 {
		t.Errorf("want total=1, got %d", total)
	}
}
