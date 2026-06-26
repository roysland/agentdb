---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/config'
files:
- internal/config/persist.go
- internal/config/runtime.go
- internal/config/runtime_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/config
type: Module
---

## What it does

The `config` module resolves runtime configuration for AgentDB by layering environment variables, a user-editable TOML config file, and hardcoded defaults. It handles database connection strings, the project path, and indexing parameters like lines-per-chunk, always expanding `~` to the user's home directory.

## Public interface

```go
func LoadDefaultDatabaseURL() string
func LoadDefaultDatabaseDriver() string
func LoadDefaultProjectPath() string
func LoadDefaultLinesPerChunk() int

func SaveDefaultDatabaseURL(dbURL string) error
func SaveDefaultProjectPath(projectPath string) error

func DefaultDatabasePath() string

type Runtime struct {
    DatabaseURL              string
    DatabaseDriver           string
    ProjectPath              string
    SuppressBootstrapWarning bool
    IndexLinesPerChunk       int
}

func Resolve(input Runtime) Runtime
```

## Key invariants

- **Resolution precedence is fixed and strict:** for each field, the order is `input` → `AGENTDB_*` env var → config file value → hardcoded default. Env vars always win over config-file values.
- **Config file location follows XDG Base Directory spec:** `$XDG_CONFIG_HOME/agentdb/config.toml` if set, otherwise `~/.config/agentdb/config.toml`. The default database path follows `$XDG_DATA_HOME/agentdb/agentdb.db` or `~/.local/share/agentdb/agentdb.db`.
- **Empty or whitespace-only values are treated as unset** — `SaveDefaultDatabaseURL` and `SaveDefaultProjectPath` silently no-op on blank input, and `loadConfigValue` returns `""` for missing keys.
- **`IndexLinesPerChunk` floors at 50** if no positive value is supplied by any source.
- **`DatabaseDriver` defaults to `"auto"`** when no source provides a value.
- **Config file writes are atomic at the file level** (single `os.WriteFile` call) and keys are written in a deterministic order with the three canonical keys (`AGENTDB_DB_URL`, `AGENTDB_DB_DRIVER`, `AGENTDB_PROJECT_PATH`) pinned to the top for readability.

## Non-obvious decisions

- **Config file format is TOML-like but parsed with a custom hand-rolled parser** rather than a TOML library. The parser only supports flat `key = "value"` pairs with `"`-quoting and `\"` escaping — no sections, arrays, or inline tables. This keeps the dependency surface minimal but means any value containing characters beyond what the simple `strings.SplitN(..., "=", 2)` logic handles will be silently mangled.
- **`SuppressBootstrapWarning` is a struct field on `Runtime` but is never set or read by `Resolve`.** It appears to be a passthrough field intended for callers to set externally, though no code in this module touches it.
- **Legacy env var `AGENTDB_URL` is explicitly ignored** — the test `TestResolveDatabaseURLIgnoresLegacyEnv` confirms this is intentional, suggesting a migration from an older naming convention to the `AGENTDB_DB_*` prefix.
