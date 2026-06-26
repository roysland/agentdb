---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/index'
files:
- internal/index/incremental.go
- internal/index/incremental_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/index
type: Module
---

## What it does

The `index` module provides file-level change detection for codebases by computing SHA-256 hashes of files on disk and comparing them against a stored manifest. It categorizes each file as Added, Changed, Removed, or Unchanged, and includes utilities for legacy hash migration and post-migration SQLite maintenance.

## Public interface

```go
func HashFile(path string) (string, error)
func ComputeDelta(ctx context.Context, codebaseID int64, rootPath string, storedHashes map[string]string) (DeltaResult, error)
func FilesToProcess(delta DeltaResult) []string
func IsLegacyHash(hash string) bool
func RunPostMigrationMaintenance(ctx context.Context, db *sql.DB) (MigrationResult, error)
func VerifyIntegrity(ctx context.Context, db *sql.DB, codebaseID int64) (orphanCount int, err error)
```

**Types:**
- `DeltaResult` — struct with fields `Changed`, `Added`, `Removed`, `Unchanged` (each `[]string`)
- `MigrationResult` — struct with fields `FilesReindexed`, `OrphanedRemoved`, `PagesReclaimed`

## Key invariants

- `FilesToProcess` always returns `Changed` concatenated with `Added` in that order; callers must not rely on sorted output.
- Relative paths are normalized to forward slashes (`filepath.ToSlash`) before being used as map keys, ensuring cross-platform consistency.
- `IsLegacyHash` returns `false` for any hash ≥ 64 hex characters, `false` for empty strings, and `true` only for non-empty hex strings shorter than 64 characters.
- `ComputeDelta` treats legacy hashes (shorter than 64 hex chars) as stale regardless of current file content, forcing re-indexing.
- `VerifyIntegrity` only checks chunks whose `codebase_id` matches the provided `codebaseID`; it does not inspect other codebases.

## Non-obvious decisions

- **`codebaseID` parameter in `ComputeDelta` is unused.** The function signature accepts `codebaseID int64` but the implementation never references it. This is explicitly noted in a comment as "included for future use," meaning the API was designed ahead of the implementation to avoid a breaking change later.
- **`RunPostMigrationMaintenance` uses `PRAGMA incremental_vacuum` without a page count argument.** This reclaims *all* free pages rather than a bounded number, which is appropriate for a migration that may free a large number of pages at once, but could be surprising if someone expected incremental behavior with a page limit.
- **`HashFile` always streams through `io.Copy` regardless of file size.** The doc comment mentions a 10MB threshold for streaming, but the implementation unconditionally streams. The comment appears to be aspirational or leftover from a planned optimization that was never implemented.

## Unclear intent

- The 10MB streaming threshold mentioned in `HashFile`'s doc comment does not correspond to any branching logic in the implementation. It is unclear whether the intent was to add a size-based optimization (e.g., `mmap` for small files) that was never built, or if the comment is simply stale.
