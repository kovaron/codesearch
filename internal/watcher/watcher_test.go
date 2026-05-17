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
