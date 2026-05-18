package bench

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBash_RunsInWorkdir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "marker"), "x")
	out, err := RunBash(dir, "ls", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "marker") {
		t.Errorf("ls did not show marker: %q", out)
	}
}

func TestBash_PathRestricted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, err := RunBash(dir, "rg --version", 5*time.Second)
	if err == nil && !strings.Contains(out, "not found") {
		t.Errorf("rg should not resolve under restricted PATH, got %q err=%v", out, err)
	}
}

func TestBash_Timeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	start := time.Now()
	_, err := RunBash(dir, "sleep 5", 300*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout took %v, want <2s", elapsed)
	}
}
