---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/watch'
files:
- internal/watch/debounce.go
- internal/watch/watcher.go
- internal/watch/watcher_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/watch
type: Module
---

## What it does

The `watch` package monitors a codebase directory for filesystem changes using `fsnotify`, coalesces rapid events through a debouncer, and triggers incremental re-indexing (chunking, hashing, and optional symbol extraction) into the database. It operates as a state machine that queues incoming changes during active indexing so no events are lost.

## Public interface

```go
// Config holds watcher configuration.
type Config struct {
    CodebaseID   int64
    CodebasePath string
    DebounceMs   int
    Analyze      bool
}

// New creates a Watcher from the given config and database connection.
func New(cfg Config, db *sql.DB, logger *observe.Logger) (*Watcher, error)

// Run starts the filesystem watcher and blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error
```

## Key invariants

- **State machine progress**: The watcher cycles through `stateIdle → stateAccumulating → stateIndexing` (optionally `stateIndexingQueued`). It never transitions directly from `stateIdle` to `stateIndexing`; events must first accumulate.
- **No event loss during indexing**: While in `stateIndexing`, new events transition to `stateIndexingQueued` rather than being dropped. After indexing completes, the state returns to `stateAccumulating` so the debouncer's already-queued events will fire again.
- **Debouncer non-blocking signal**: The `ready` channel is buffered to 1, and sends are non-blocking. A missed signal is safe because `Drain` always returns the full pending set.
- **Path normalization**: All relative paths are converted to forward-slash form (`filepath.ToSlash`) before storage, ensuring cross-platform consistency in the database.
- **New directories are watched**: When a directory creation event is detected, it is added to the fsnotify watcher at runtime, so newly created subdirectories are monitored without restart.
- **Graceful shutdown**: `Run` returns `nil` (not an error) on context cancellation, distinguishing graceful shutdown from filesystem errors.

## Non-obvious decisions

- **Debouncer resets timer on every event rather than using a fixed window**: Each call to `Add` stops and restarts the timer. This means a stream of rapid-fire events (e.g., `git checkout`) keeps postponing the re-index until the filesystem goes quiet, avoiding many redundant partial re-indexes.
- **`reindex` is invoked directly from `Run`'s event loop (not in a goroutine)**: This means indexing blocks the watcher's event loop. The `stateIndexingQueued` state exists specifically to capture events that arrive during this blocking indexing, preventing them from being silently dropped while the loop is stuck in `reindex`.
- **`addDirectories` uses `filepath.SkipDir` on permission errors rather than failing**: A single unreadable subdirectory does not abort watching the entire codebase. The error is logged and the walk continues.
- **Test schema bootstrapping drops FTS triggers conditionally**: The test helper checks whether the FTS table exists before dropping its triggers, accommodating test environments where FTS support may not be compiled in.

## Unclear intent

- **`findParserForFile` is defined in `watcher.go` but not part of the `parse` package**: This function duplicates what could reasonably live in the `parse` package (e.g., as a dispatcher). Its placement in the watch module suggests it may be a temporary bridge, but the intent is not clear from the code alone.
