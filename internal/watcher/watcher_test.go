package watcher_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kovaron/codesearch/internal/watcher"
)

func TestDebouncer(t *testing.T) {
	d := watcher.NewDebouncer(50 * time.Millisecond)
	var calls atomic.Int32
	fn := func() { calls.Add(1) }

	for i := 0; i < 5; i++ {
		d.Add("key", fn)
	}
	time.Sleep(150 * time.Millisecond)

	if n := calls.Load(); n != 1 {
		t.Errorf("expected 1 debounced call, got %d", n)
	}
}

func TestWatcher_DetectsFileWrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.go")
	os.WriteFile(file, []byte("package main\n"), 0644)

	var detected atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	w, err := watcher.New([]string{dir}, func(path string, deleted bool) {
		if path == file && !deleted {
			detected.Store(true)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	go w.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	os.WriteFile(file, []byte("package main\nfunc Foo() {}\n"), 0644)
	time.Sleep(500 * time.Millisecond)

	if !detected.Load() {
		t.Error("watcher did not detect file write")
	}
}

func TestWatcher_DetectsSubdirWrite(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub", "nested")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "test.go")

	var detected atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	w, err := watcher.New([]string{dir}, func(path string, deleted bool) {
		if path == file && !deleted {
			detected.Store(true)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	go w.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	os.WriteFile(file, []byte("package main\n"), 0644)
	time.Sleep(500 * time.Millisecond)

	if !detected.Load() {
		t.Error("watcher did not detect file write in nested subdirectory")
	}
}

func TestWatcher_DetectsNewSubdir(t *testing.T) {
	dir := t.TempDir()
	var detected atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	w, err := watcher.New([]string{dir}, func(path string, deleted bool) {
		if filepath.Base(path) == "new.go" && !deleted {
			detected.Store(true)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	go w.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	sub := filepath.Join(dir, "newdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // let watcher pick up the new dir
	os.WriteFile(filepath.Join(sub, "new.go"), []byte("package main\n"), 0644)
	time.Sleep(500 * time.Millisecond)

	if !detected.Load() {
		t.Error("watcher did not detect file in newly-created subdirectory")
	}
}

func TestWatcher_SkipsNoiseDirs(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing noise dirs should not be watched. We check this indirectly:
	// a write inside .git should NOT trigger the handler.
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	var triggered atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w, err := watcher.New([]string{dir}, func(path string, deleted bool) {
		triggered.Store(true)
	})
	if err != nil {
		t.Fatal(err)
	}
	go w.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref"), 0644)
	time.Sleep(400 * time.Millisecond)

	if triggered.Load() {
		t.Error("watcher fired for a write inside .git — noise dirs should be skipped")
	}
}
