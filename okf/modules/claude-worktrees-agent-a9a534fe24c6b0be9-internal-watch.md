---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/watch'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/watch/debounce.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/watch/watcher.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/watch/watcher_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/watch
type: Module
---

## What it does

The `watch` package monitors a codebase directory for filesystem events using `fsnotify`, coalesces rapid changes through a debounce window, and triggers incremental re-indexing (chunking, hashing, and optional symbol extraction) via the store and index layers. It implements a small state machine so that events arriving during an ongoing re-index are queued for the next pass rather than dropped.

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

- **Debounce signal is non-blocking**: The debouncer's `ready` channel is buffered to 1 and uses a `select`/`default` send, so a slow consumer never blocks the timer goroutine; at most one signal is queued.
- **Same-path event coalescing**: `debouncer.Add` overwrites the pending operation for a given path, so only the latest `fsnotify.Op` per path survives in a batch.
- **State machine guards against overlapping re-indexes**: Events that arrive while `state == stateIndexing` transition to `stateIndexingQueued`, which causes the next debounce fire to re-enter `stateAccumulating` rather than going back to `stateIdle`.
- **Newly created directories are watched**: When `fsnotify` reports a `Create` event that resolves to a directory, it is added to the watcher (unless filtered by `ShouldSkipDirName`).
- **Removed files are cleaned up everywhere**: `reindex` deletes chunks, indexed-file records, symbols, edges, and source-file records for paths in `delta.Removed`.
- **Timer is always reset on new events**: `Add` stops and replaces the timer on every call, implementing a sliding-window (not fixed-window) debounce.

## Non-obvious decisions

- **Sliding debounce instead of fixed**: The timer is stopped and re-created on every `Add` call rather than using `time.Timer` with `Reset`. This means a file that is saved every 100 ms with a 500 ms window will never trigger — the window only fires after genuine quiet. This is intentional for editors that emit rapid successive writes.
- **`reindex` is called synchronously in the `Run` loop**: The entire re-index (including optional `runAnalyze`) runs on the same goroutine as the event loop. This serializes indexing but means long re-indexes will delay processing of new filesystem events — acceptable because the state machine captures them for the next pass.
- **`filepath.ToSlash` on relative paths**: Relative paths are converted to forward slashes before being stored or debounced, ensuring cross-platform key consistency in the database and pending map.
- **FTS triggers are dropped in tests**: The test setup drops `chunks_ai`/`chunks_ad`/`chunks_au` triggers when the FTS table doesn't exist, working around the fact that the test schema bootstrap may not include FTS support.

## Unclear intent

- **`countLines` is defined but never called in production code**: It is only used inside `runAnalyze` as a fallback when no parser matches. Its presence suggests it may be intended for broader use, but the current codebase only exercises it in a single fallback path.
