---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/index'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/index/incremental.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/index/incremental_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/index
type: Module
---

## What it does

Implements incremental indexing by comparing current file hashes on disk against a stored manifest, categorizing each file as Added, Changed, Removed, or Unchanged. It also provides utilities for legacy hash detection (MD5 → SHA-256 migration), post-migration SQLite maintenance, and orphan chunk integrity verification.

## Public interface

```go
func HashFile(path string) (string, error)
func IsLegacyHash(hash string) bool
func FilesToProcess(delta DeltaResult) []string
func ComputeDelta(ctx context.Context, codebaseID int64, rootPath string, storedHashes map[string]string) (DeltaResult, error)
func RunPostMigrationMaintenance(ctx context.Context, db *sql.DB) (MigrationResult, error)
func VerifyIntegrity(ctx context.Context, db *sql.DB, codebaseID int64) (orphanCount int, err error)
```

**Types:**

```go
type DeltaResult struct {
    Changed   []string
    Added     []string
    Removed   []string
    Unchanged []string
}

type MigrationResult struct {
    FilesReindexed  int
    OrphanedRemoved int
    PagesReclaimed  int64
}
```

## Key invariants

- `FilesToProcess` returns only Changed and Added files — Removed and Unchanged are never re-indexed.
- Relative paths are normalized to forward slashes (`filepath.ToSlash`) for consistent map keys across platforms.
- Legacy hashes (< 64 hex chars) are always treated as Changed, even if the current file content would match, to force re-indexing with SHA-256.
- `VerifyIntegrity` checks for chunks whose `file_hash` has no matching row in `indexed_files` for the same `codebase_id`.

## Non-obvious decisions

- **`codebaseID` parameter in `ComputeDelta` is unused** — the doc comment says "included for future use." A caller might assume it filters results or scopes the walk; it does not. This could be surprising and may warrant removal or documentation if the function signature is part of a stable API.
- **`RunPostMigrationMaintenance` uses `PRAGMA incremental_vacuum` without an explicit page count argument** — this vacuums *all* freelist pages. For large databases this could be a long-running operation. The alternative (`PRAGMA incremental_vacuum(N)`) would limit work per call, but the current design defers pacing responsibility to the caller.

## Unclear intent

- `MigrationResult.FilesReindexed` and `MigrationResult.OrphanedRemoved` are declared in the struct but **never populated** by `RunPostMigrationMaintenance`. It's unclear whether these fields are reserved for future use, populated elsewhere, or if the function is incomplete.
