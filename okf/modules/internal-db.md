---
commit: cc77d6b1c1d919b608eb80f7d9faa82f9dba6bc0
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
timestamp: '2026-08-03'
title: internal/db
type: Module
---

### What it does
The `internal/db` module provides a thread-safe, persistent SQLite connection manager that enforces application-level write serialization and strict operation timeouts. It handles database bootstrapping, schema migrations, and health monitoring to ensure the database remains in a consistent state across application restarts and concurrent access patterns.

### Public interface
*   **`NewConnectionHandle(ctx context.Context, cfg config.Runtime, logger *observe.Logger) (*ConnectionHandle, error)`**: Initializes a persistent database connection with WAL mode and required PRAGMAs.
*   **`(*ConnectionHandle) WriteContext(parent context.Context) (context.Context, context.CancelFunc, error)`**: Acquires a write semaphore with a timeout and returns a context with a 5-minute deadline.
*   **`(*ConnectionHandle) ReleaseWrite()`**: Releases the write semaphore. Must be called after a successful `WriteContext`.
*   **`(*ConnectionHandle) ReadContext(parent context.Context) (context.Context, context.CancelFunc)`**: Returns a context with a 5-second read deadline.
*   **`(*ConnectionHandle) EnsureSchema(ctx context.Context) error`**: Bootstraps the database schema and applies incremental migrations.
*   **`(*ConnectionHandle) HealthCheck(ctx context.Context) error`**: Verifies the connection and performs an automatic reconnection if the database is unreachable.
*   **`BootstrapSchema(ctx context.Context, db *sql.DB, schemaPath string) (BootstrapStats, error)`**: Executes raw SQL schema statements, including support for embedded schema files.

### Key invariants
*   **Write Serialization**: Only one write operation can proceed at a time, enforced by the `writeSem` channel semaphore.
*   **Connection Persistence**: The `ConnectionHandle` maintains a single `*sql.DB` connection (configured via `SetMaxOpenConns(1)`) to avoid SQLite locking contention.
*   **Idempotency**: All migration functions are designed to be safe to execute multiple times against an existing database.
*   **Resource Cleanup**: Every `WriteContext` acquisition must be paired with a `ReleaseWrite` call to prevent deadlocks.

### Non-obvious decisions
*   **Application-Layer Write Serialization**: Instead of relying solely on SQLite's internal locking, the module uses a `chan struct{}` semaphore and `mutexTTL`. This is done to provide granular control over write timeouts and to prevent long-running indexing tasks from blocking the entire application process indefinitely.
*   **Manual Trigger Splitting**: The `splitStatements` function manually parses SQL to handle semicolons within `BEGIN...END` blocks. This is necessary because the standard `database/sql` driver executes statements based on semicolon delimiters, which would otherwise prematurely terminate trigger definitions.
*   **Graceful FTS5 Degradation**: The migration logic explicitly checks for "no such module" errors when creating FTS5 tables. This allows the application to function in environments where the SQLite build lacks FTS5 support, falling back to in-memory scanning as per requirement 1.5.
*   **Lazy Semaphore Initialization**: `ensureWriteSem` uses `sync.Once` to support `ConnectionHandle` instances created manually in tests, which bypass the standard `NewConnectionHandle` constructor.
