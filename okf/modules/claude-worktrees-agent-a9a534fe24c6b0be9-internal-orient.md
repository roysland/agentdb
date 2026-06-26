---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/orient'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/orient/orient.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/orient/orient_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/orient/retrieve.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/orient/retrieve_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/orient
type: Module
---

## What it does

The `orient` package classifies documentation files by type (README, design docs, architecture, agent instructions, etc.) using configurable glob patterns with priorities, and retrieves their content from the database by constructing SQL queries from those patterns. It supports loading custom configuration from `agentdb.yml` or `.kiro/agentdb.yml`, falling back to a built-in default set.

## Public interface

```go
func DefaultConfig() Config
func Load(codebaseRoot string, logger *observe.Logger) (Config, error)
func Classify(filePath string, config Config) ClassifyResult
func Retrieve(ctx context.Context, db *sql.DB, cfg RetrieveConfig) ([]OrientationDoc, error)
```

Types:
- `DocType` — string enum for document categories (`DocTypeReadme`, `DocTypeDesign`, `DocTypeArchitecture`, `DocTypeAgentInstructions`, `DocTypeTodo`, `DocTypeContributing`, `DocTypeFeatureList`, `DocTypeGeneral`)
- `PatternSet` — holds `Patterns []string`, `Priority int`, `Excludes []string`, `MaxItems int`
- `Config` — `map[DocType]PatternSet`
- `ClassifyResult` — `{ DocType DocType; Priority int }`
- `OrientationDoc` — `{ FilePath, Content string; DocType DocType; Priority int }`
- `RetrieveConfig` — `{ CodebaseIDs []int64; Config Config }`

## Key invariants

- `Classify` only matches against the **basename** of the file path, not the full path.
- Non-root files receive a **+10 priority penalty** (root-level files are preferred).
- Ties at the same priority are broken **lexicographically by DocType string** to ensure deterministic output.
- `globToSQL` always **prepends `%`** to every LIKE pattern, making SQL queries intentionally broad; precise filtering happens in the application-layer `Classify` call.
- `enforceMaxItems` respects `MaxItems == 0` as **unlimited** (no cap applied).
- `Load` returns **defaults** (without error) when the config file is missing or contains malformed YAML — it never propagates a parse error to the caller.
- `yamlKeyToDocType` silently **skips unknown keys** rather than erroring.

## Non-obvious decisions

- **SQL query is broad, classification is precise**: `buildQuery` prepends `%` to every LIKE pattern, meaning the database may return many false-positive rows (e.g., a `*.md` pattern matches every markdown file in any directory). The actual filtering happens in Go via `Classify` after retrieval. This two-stage approach means the SQL query cannot be the sole filter — callers must not assume the returned rows are all relevant.

- **Priority penalty for non-root files is hardcoded to +10**: Rather than making this configurable or using a multiplier, a flat `+10` is added. Since the default priorities are 1–8, this guarantees non-root matches never outrank root matches within the same pattern set, but a custom config with priorities 11+ would break this assumption.

- **`Load` swallows malformed YAML**: A malformed config file logs a warning and returns defaults rather than an error. This means a typo in `agentdb.yml` silently degrades to default behavior with only a log message as signal.

## Unclear intent

- **`globMatchBasename` in `retrieve.go`**: This helper function is defined but never called within the package. It appears to be exported for future use or was left behind after a refactor. Its purpose relative to the existing `matchesPatternSet` in `orient.go` (which does the same thing internally) is unclear.
