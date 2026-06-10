// Package watch provides filesystem monitoring with debounced incremental re-indexing.
package watch

import (
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debouncer coalesces rapid filesystem events into batched triggers.
// When events arrive within the configured window, the timer resets.
// Once the window expires with no new events, the ready channel fires.
type debouncer struct {
	window  time.Duration
	timer   *time.Timer
	pending map[string]fsnotify.Op // path → latest operation
	mu      sync.Mutex
	ready   chan struct{} // signals that the debounce window has expired
}

// newDebouncer creates a debouncer with the given quiet window duration.
func newDebouncer(window time.Duration) *debouncer {
	return &debouncer{
		window:  window,
		pending: make(map[string]fsnotify.Op),
		ready:   make(chan struct{}, 1),
	}
}

// Add records a filesystem event and resets the debounce timer.
// If the timer was not running, it starts a new one. When the timer
// fires (no new events within the window), a signal is sent on Ready().
// For the same path, the latest operation overwrites any previous one.
func (d *debouncer) Add(path string, op fsnotify.Op) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.pending[path] = op

	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.window, func() {
		// Non-blocking send: if a signal is already pending, skip.
		select {
		case d.ready <- struct{}{}:
		default:
		}
	})
}

// Drain returns and clears all pending events. The caller should invoke
// Drain after receiving a signal from Ready().
func (d *debouncer) Drain() map[string]fsnotify.Op {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.pending) == 0 {
		return nil
	}

	batch := d.pending
	d.pending = make(map[string]fsnotify.Op)
	return batch
}

// Ready returns the channel that fires when the debounce window expires
// with accumulated events waiting to be processed.
func (d *debouncer) Ready() <-chan struct{} {
	return d.ready
}
