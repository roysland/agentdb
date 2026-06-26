---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/orient'
files:
- internal/orient/orient.go
- internal/orient/orient_test.go
- internal/orient/retrieve.go
- internal/orient/retrieve_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/orient
type: Module
---

# `internal/orient`

## What it does

Classifies documentation files in a codebase into typed categories (readme, design, architecture, etc.) based on filename patterns, then retrieves their content from the database as ordered, priority-sorted orientation documents. It supports user-defined configuration via `agentdb.yml` and falls back to a built-in default pattern set.

## Public interface

```go
func DefaultConfig() Config
func Load(codebaseRoot string, logger *observe.Logger) (Config, error)
func Classify(filePath string, config Config) ClassifyResult
func Retrieve(ctx context.Context, db *sql.DB, cfg RetrieveConfig) ([]OrientationDoc, error)
```

**Types:**

```go
type DocType string
type PatternSet struct {
    Patterns []string
    Priority int
    Excludes []string
    MaxItems int
}
type Config map[DocType]PatternSet
type ClassifyResult struct {
    DocType  DocType
    Priority int
}
type OrientationDoc struct {
    FilePath string
    Content  string
    DocType  DocType
    Priority int
}
type RetrieveConfig struct {
    CodebaseIDs []int64
    Config      Config
}
```

## Key invariants

- **Priority is 1-based and lower-is-higher.** Root-level files get their raw `Priority`; non-root files add 10, so root files always rank above subdirectory files of the same pattern set.
- **Tie-breaking is deterministic.** When two doc types have equal effective priority, the lexicographically smaller `DocType` string wins. This is guaranteed by iterating doc types in sorted order.
- **SQL query is intentionally broad; classification is precise.** `globToSQL` prepends `%` to every pattern so the query matches any directory prefix. Final matching fidelity is provided by `Classify` at the application layer using `filepath.Match` on the basename.
- **`MaxItems = 0` means unlimited.** Only positive values enforce a cap.
- **Malformed config falls back to defaults.** If `agentdb.yml` exists but has invalid YAML or no valid patterns, `Load` returns `DefaultConfig()` with a warning rather than an error.

## Non-obvious decisions

- **Priority penalty of 10 for non-root files.** The constant `10` is hardcoded in `Classify` rather than being configurable. This means the gap between root and non-root priority is always exactly 10, which could theoretically overlap with a user's custom priority values if they use values ≥ 10. A user setting a priority of 12 for a doc type would find subdirectory files of that type (priority 22) ranked below root files of a type with priority 1 (priority 1), but the 10-point gap is not documented or validated anywhere.

- **`globToSQL` always prepends `%` even when the glob already starts with `*`.** For a pattern like `*.md`, the result is `%%.md` (double `%`). This is noted in a comment but is a deliberate choice — the broad SQL query is a performance-versus-precision tradeoff where the application-layer `Classify` function is the real filter. The alternative would be a more precise SQL query per pattern (using `LIKE '%/*.md'` OR `LIKE '*.md'`), which would be more complex.

- **`Retrieve` concatenates chunks with `\n` separator.** When multiple chunks belong to the same file, they are joined with a newline. There is no marker indicating chunk boundaries in the output `Content` field, so downstream consumers cannot distinguish chunk boundaries from actual newlines in the source file.

## Unclear intent

- **No unclear symbols.** All types, functions, and imports are defined within this module or in the listed dependency modules (`internal/observe`).
