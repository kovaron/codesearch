package watcher

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Handler is called when a file changes. deleted=true means the file was removed.
type Handler func(path string, deleted bool)

// Debouncer collapses rapid repeated events on the same key into one call.
type Debouncer struct {
	duration time.Duration
	mu       sync.Mutex
	timers   map[string]*time.Timer
}

// NewDebouncer returns a new Debouncer that collapses events within duration d.
func NewDebouncer(d time.Duration) *Debouncer {
	return &Debouncer{duration: d, timers: make(map[string]*time.Timer)}
}

// Add schedules fn to run after the debounce duration, resetting the timer if key is pending.
func (d *Debouncer) Add(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[key]; ok {
		t.Reset(d.duration)
		return
	}
	d.timers[key] = time.AfterFunc(d.duration, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
}

// Watcher watches directories recursively and invokes a handler on file changes.
type Watcher struct {
	fsw      *fsnotify.Watcher
	handler  Handler
	debounce *Debouncer
}

// noiseDirs are skipped during recursive subscription. Kept in sync with
// internal/indexer.IsNoiseDir; duplicated here to avoid an import cycle and
// because watcher should not depend on indexer's package.
var noiseDirs = map[string]struct{}{
	".git":         {},
	"vendor":       {},
	"node_modules": {},
	".claude":      {},
	".codesearch":  {},
	".idea":        {},
	".vscode":      {},
	"dist":         {},
	"build":        {},
	"target":       {},
}

func isNoiseDir(name string) bool {
	_, ok := noiseDirs[name]
	return ok
}

// New creates a Watcher that monitors dirs recursively and calls handler on
// file changes. Every subdirectory of every dir is added to the underlying
// fsnotify watcher, except those matching the noise list (.git, vendor,
// node_modules, IDE/build artifacts).
func New(dirs []string, handler Handler) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:      fsw,
		handler:  handler,
		debounce: NewDebouncer(200 * time.Millisecond),
	}
	for _, dir := range dirs {
		if err := w.addRecursive(dir); err != nil {
			fsw.Close()
			return nil, err
		}
	}
	return w, nil
}

// addRecursive walks root and adds every non-noise directory to the fsnotify
// watcher. The root itself is always added regardless of its name.
func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && isNoiseDir(d.Name()) {
			return filepath.SkipDir
		}
		return w.fsw.Add(path)
	})
}

// Run starts the watch loop and blocks until ctx is cancelled. Newly-created
// directories are added to the watch set on the fly, so files written inside
// them are detected without a daemon restart.
func (w *Watcher) Run(ctx context.Context) {
	defer w.fsw.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			path := event.Name
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					if err := w.addRecursive(path); err != nil {
						log.Printf("watcher: add new dir %s: %v", path, err)
					}
					continue
				}
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				w.debounce.Add(path, func() { w.handler(path, true) })
			} else if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				w.debounce.Add(path, func() { w.handler(path, false) })
			}
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
}
