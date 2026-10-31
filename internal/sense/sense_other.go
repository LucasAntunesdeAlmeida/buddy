//go:build !windows

// Package sense watches the desktop. Only Windows is supported; elsewhere the
// watcher reports a user who is always present and doing something else, so
// the rest of Buddy still builds and runs.
package sense

import (
	"context"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
)

// Watcher is a stand-in that never changes.
type Watcher struct{ changes chan struct{} }

// NewWatcher returns a stand-in watcher.
func NewWatcher() *Watcher { return &Watcher{changes: make(chan struct{})} }

// Snapshot returns a fixed snapshot.
func (w *Watcher) Snapshot() activity.Snapshot { return activity.Snapshot{Activity: 0.3, Back: -1} }

// Changes never fires.
func (w *Watcher) Changes() <-chan struct{} { return w.changes }

// Run waits for ctx.
func (w *Watcher) Run(ctx context.Context) { <-ctx.Done() }

// CursorPos is not available.
func CursorPos() (x, y int, ok bool) { return 0, 0, false }
