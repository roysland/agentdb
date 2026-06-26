---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/config'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/config/persist.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/config/runtime.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/config/runtime_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/config
type: Module
---

## What it does

This module handles configuration resolution for the agentdb CLI tool. It loads settings from a TOML-style config file (stored under XDG-compliant paths) and environment variables, then resolves final runtime values with a defined precedence: explicit input > environment variable > config file > built-in default.

## Public interface

```go
func Resolve(input Runtime) Runtime
```

```go
type Runtime struct {
    DatabaseURL              string
    DatabaseDriver           string
    ProjectPath              string
    SuppressBootstrapWarning bool
    IndexLinesPerChunk       int
}
```

```go
func LoadDefaultDatabaseURL() string
func LoadDefaultDatabaseDriver() string
func LoadDefaultProjectPath() string
func LoadDefaultLinesPerChunk() int
func SaveDefaultDatabaseURL(dbURL string) error
func SaveDefaultProjectPath(projectPath string) error
func DefaultDatabasePath() string
```

## Key invariants

- **Precedence is always input → env → config file → hardcoded default.** Every `Resolve` field follows this chain; no field skips a layer silently.
- **Environment variables use the `AGENTDB_` prefix exclusively.** The code explicitly ignores legacy keys like `AGENTDB_URL` — only `AGENTDB_DB_URL` is canonical.
- **Config file is not a real TOML file.** Despite the `.toml` extension, parsing is a custom `key = "value"` line splitter with `#` comments — no TOMOLib is used. Values are always double-quoted on write.
- **`IndexLinesPerChunk` floors at 50** if no source provides a positive value.
- **`DatabaseDriver` defaults to `"auto"`** when no source provides a value.
- **Config writes are deterministic.** Keys `AGENTDB_DB_URL`, `AGENTDB_DB_DRIVER`, and `AGENTDB_PROJECT_PATH` are pinned to the top of the file for readability; remaining keys are sorted alphabetically.
- **`SaveDefaultDatabaseURL` and `SaveDefaultProjectPath` silently no-op on empty/whitespace-only input** rather than returning an error.
- **Tilde expansion (`~`) is applied to `DatabaseURL` and `ProjectPath`** after all resolution layers, including when loaded from the config file.

## Non-obvious decisions

- **Config file uses `.toml` extension but is parsed with a hand-rolled parser.** A competent developer would expect a TOML library. The custom parser only handles flat `key = "value"` pairs and will silently drop any TOML features (nested tables, arrays, inline tables). This means the file format cannot grow in complexity without replacing the parser.
- **`upsertConfigValue` re-reads and rewrites the entire file on every save.** There is no locking, no atomic rename, and no concurrent access guard. In a multi-process scenario this could silently drop writes.
- **`expandTilde` is applied to config-file values at read time but not at save time.** A user who writes `~/myproject` into the config file will get it expanded on read, but the original `~/myproject` remains in the file. This is asymmetric — environment variable values are not expanded, only config-file values.
- **`SuppressBootstrapWarning` is a `Runtime` field but `Resolve` never sets or reads it.** It exists on the struct for callers to consume, but this module treats it as pass-through only.

## Unclear intent

- **The `AGENTDB_LINES_PER_CHUNK` env var is read directly in `Resolve` rather than going through `loadConfigValue`.** All other env vars are read in `Resolve` and then passed through `loadConfigValue` for the config-file fallback. `IndexLinesPerChunk` short-circuits: it reads the env var inline with `strconv.Atoi` and only falls through to `LoadDefaultLinesPerChunk` (which reads the config file) if the env var is unset or unparseable. The inconsistency with how `DatabaseURL`/`DatabaseDriver`/`ProjectPath` are resolved (where env is checked first, then config) is structurally different — it's unclear whether this is intentional (to avoid tilde expansion on the chunk value) or an oversight.
