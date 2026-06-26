---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/bootstrap.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/bootstrap_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/bug_condition_connection_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/connection.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/open.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/open_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/schema_embed.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/schema_version.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db/schema_version_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/db
type: Module
---

## What it does

The `db` package manages SQLite database lifecycle for the agentdb system: opening single-connection pools with WAL mode, bootstrapping schema from an embedded SQL file, applying incremental migrations, and exposing a `ConnectionHandle` that serializes writes through a channel-based semaphore with strict timeout enforcement.

## Public interface

- `NewConnectionHandle(ctx context.Context, cfg config.Runtime, logger *observe.Logger) (*ConnectionHandle, error)` — opens a persistent connection, runs PRAGMAs, returns a handle.
- `(*ConnectionHandle).ReadContext(parent context.Context) (context.Context, context.CancelFunc)` — returns a context bounded by `readTTL` (default 5s).
- `(*ConnectionHandle).WriteContext(parent context.Context) (context.Context, context.CancelFunc, error)` — acquires the write semaphore (bounded by `mutexTTL`), returns a context bounded by `writeTTL` (default 5m). Caller must call `ReleaseWrite` afterward.
- `(*ConnectionHandle).ReleaseWrite()` — releases the write semaphore; panics if not held.
- `(*ConnectionHandle).DB() *sql.DB` — returns the underlying connection (safe for reads; writes must go through `WriteContext`).
- `(*ConnectionHandle).HealthCheck(ctx context.Context) error` — pings the connection and transparently reconnects on failure.
- `(*ConnectionHandle).Close() error` — closes the connection and releases resources.
- `(*ConnectionHandle).EnsureSchema(ctx context.Context) error` — bootstraps schema if missing and applies all migrations.
- `BootstrapSchema(ctx context.Context, db *sql.DB, schemaPath string) (BootstrapStats, error)` — applies a SQL schema file, gracefully skipping FTS5 objects when the module is unavailable.
- `MigrateSchema(ctx context.Context, db *sql.DB) error` — applies all incremental migrations idempotently.

## Key invariants

- **Single connection**: `SetMaxOpenConns(1)` is set on every opened pool; the `ConnectionHandle` assumes exclusive access and serializes writes at the application layer.
- **Write serialization**: All writes must go through `WriteContext` → `ReleaseWrite`. The semaphore has capacity 1, so concurrent writers block (with timeout) rather than race.
- **Schema bootstrap is conditional**: `EnsureSchema` only calls `BootstrapSchema` when fewer than 3 of the core tables (`meta`, `codebases`, `memories`) exist, preventing re-bootstrap on an already-initialized database.
- **Migrations are idempotent**: Every migration uses `IF NOT EXISTS` or checks for column/table existence before altering, so `MigrateSchema` is safe to call on every startup.
- **FTS5 is optional**: Both `BootstrapSchema` and `MigrateSchema` detect `no such module` errors and skip FTS5-related objects rather than failing; the search layer falls back to in-memory scan.
- **`splitStatements` handles trigger bodies**: Semicolons inside `BEGIN...END` trigger blocks are not treated as statement terminators, so multi-statement triggers are applied as a single unit.

## Non-obvious decisions

- **Channel-based semaphore instead of `sync.Mutex`**: `WriteContext` uses a buffered channel (`writeSem chan struct{}, 1`) rather than a mutex so that acquisition can be bounded by `mutexTTL` via a `select` with a timer. A `sync.Mutex` has no built-in timeout, which was the root cause of Bug 1.1 (goroutine leak on timeout).
- **`writeTTL` defaults to 5 minutes, not seconds**: Indexing large codebases can exceed the original 3-second default, causing spurious `context deadline exceeded` errors (Bug 1.9). The 5-minute value accommodates realistic single-file indexing workloads.
- **`auto_vacuum = INCREMENTAL` is best-effort and silently ignored**: The Turso driver rejects this PRAGMA on databases created without autovacuum enabled. Since it is an optimization rather than a correctness requirement, the error is discarded rather than propagated.
- **`tableInfoPragma` uses an allowlist**: Only `chunks`, `indexed_files`, `edges`, and `memories` are accepted for `PRAGMA table_info`. This prevents arbitrary table names from being passed to the PRAGMA, which could be a vector for unexpected behavior if the function were ever exposed more broadly.
- **Trigger creation errors on `chunks_fts` are swallowed in `BootstrapSchema`**: Because SQLite allows creating triggers against nonexistent tables, the code proactively skips any statement containing `chunks_fts` once FTS5 is detected as unavailable, rather than relying on error handling after the fact.

## Unclear intent

No genuine ambiguity detected. All symbols imported from other modules (`config`, `observe`) are used in conventional ways consistent with their module names.
