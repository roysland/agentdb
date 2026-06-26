---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/db'
files:
- internal/db/bootstrap.go
- internal/db/bootstrap_test.go
- internal/db/bug_condition_connection_test.go
- internal/db/connection.go
- internal/db/open.go
- internal/db/open_test.go
- internal/db/schema_embed.go
- internal/db/schema_version.go
- internal/db/schema_version_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/db
type: Module
---

# `internal/db` Module

## What it does

Manages SQLite database lifecycle for agentdb: opening connections with WAL mode, bootstrapping schema from an embedded SQL file, and applying incremental migrations to evolve existing databases. It provides a `ConnectionHandle` that serializes writes through a semaphore with strict timeout enforcement to prevent unbounded goroutine growth under contention.

## Public interface

```go
// ConnectionHandle — primary type for callers
func NewConnectionHandle(ctx context.Context, cfg config.Runtime, logger *observe.Logger) (*ConnectionHandle, error)
func (ch *ConnectionHandle) ReadContext(parent context.Context) (context.Context, context.CancelFunc)
func (ch *ConnectionHandle) WriteContext(parent context.Context) (context.Context, context.CancelFunc, error)
func (ch *ConnectionHandle) ReleaseWrite()
func (ch *ConnectionHandle) DB() *sql.DB
func (ch *ConnectionHandle) HealthCheck(ctx context.Context) error
func (ch *ConnectionHandle) Close() error
func (ch *ConnectionHandle) EnsureSchema(ctx context.Context) error

// Open — low-level connection (returns raw *sql.DB)
func Open(ctx context.Context, cfg config.Runtime) (*sql.DB, error)

// Schema bootstrapping and migration
func BootstrapSchema(ctx context.Context, db *sql.DB, schemaPath string) (BootstrapStats, error)
func MigrateSchema(ctx context.Context, db *sql.DB) error
func UpsertSchemaVersion(ctx context.Context, db *sql.DB) error // in schema_version.go

// Schema helpers
func GetEmbeddedSchema() (string, error) // in schema_embed.go
```

## Key invariants

- **Single persistent connection**: `SetMaxOpenConns(1)` is always applied; the pool is never used.
- **Write serialization**: Only one writer can hold the semaphore at a time via `writeSem` (buffered channel, capacity 1). `ReleaseWrite` must be called after every successful `WriteContext`.
- **Goroutine safety under timeout**: `WriteContext` does not spawn goroutines; it uses a `select` with a timer so timed-out callers do not leave dangling goroutines blocked on the mutex.
- **Schema bootstrap is idempotent**: `CREATE TABLE IF NOT EXISTS` and `CREATE TRIGGER IF NOT EXISTS` are used throughout; `MigrateSchema` checks for column/table existence before altering.
- **FTS5 is best-effort**: If the SQLite build lacks FTS5, the migration is silently skipped and the search layer falls back to in-memory scan.
- **`tableInfoPragma` allowlist**: Only four tables (`chunks`, `indexed_files`, `edges`, `memories`) are accepted; any other table returns an error rather than constructing arbitrary SQL.

## Non-obvious decisions

- **Channel-based semaphore instead of `sync.Mutex`**: A `chan struct{}` (capacity 1) serializes writes so that `WriteContext` can participate in `select` with a timeout and parent cancellation. A `sync.Mutex` would require spawning a goroutine to call `Lock()`, and that goroutine would remain blocked after timeout — the exact bug (1.1) these tests guard against.
- **`splitStatements` handles trigger bodies**: Naive semicolon splitting breaks `CREATE TRIGGER ... BEGIN ... END` blocks. The function tracks an `inTrigger` flag and only emits a statement when it sees a line ending with `END;`.
- **`tableInfoPragma` uses an explicit allowlist instead of string formatting**: Prevents SQL injection via the table name parameter, even though this is an internal function. The allowlist is intentionally narrow — only tables that need runtime schema introspection are supported.
- **`PRAGMA auto_vacuum = INCREMENTAL` is silently ignored on failure**: The Turso/libsql driver rejects this pragma on databases not created with autovacuum enabled. Rather than failing the connection, the error is discarded because autovacuum is an optimization, not a correctness requirement.
- **`BootstrapSchema` tries embedded schema before disk**: When the default path `"data/schema.sql"` is used, the function first attempts to load from the Go embed via `GetEmbeddedSchema()`. This allows the compiled binary to bootstrap without shipping the SQL file.

## Unclear intent

- **`resolveDriver` and `isLocalFilePath`**: These are referenced in `openSingleConn` and `Open` but their definitions are not in the provided files. They likely live in `config` or another module — their behavior (what drivers are supported, what constitutes a "local file path") is not determinable from this module alone.
- **`UpsertSchemaVersion`**: Declared in `schema_version.go` (not shown). The version numbering scheme and what triggers a version bump are not visible here.
- **`Open` vs `NewConnectionHandle`**: Both open connections, but `Open` returns a raw `*sql.DB` while `NewConnectionHandle` wraps it with serialization and health checking. The intended caller for each is not obvious from the code alone — `Open` appears to be a lower-level escape hatch, but it's unclear when it should be preferred.
