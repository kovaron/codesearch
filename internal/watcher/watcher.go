package watcher

import (
	"context"
	"log"
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

// Watcher watches directories and invokes a handler on file changes.
type Watcher struct {
	fsw      *fsnotify.Watcher
	handler  Handler
	debounce *Debouncer
}

// New creates a Watcher that monitors dirs and calls handler on file changes.
func New(dirs []string, handler Handler) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if err := fsw.Add(dir); err != nil {
			fsw.Close()
			return nil, err
		}
	}
	return &Watcher{
		fsw:      fsw,
		handler:  handler,
		debounce: NewDebouncer(200 * time.Millisecond),
	}, nil
}

// Run starts the watch loop and blocks until ctx is cancelled.
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
