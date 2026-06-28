---
commit: 99a4eb8721ee6257678f63653cf97d856a65aa60
description: 'Codebase knowledge for module: internal/index'
files:
- internal/index/incremental.go
- internal/index/incremental_test.go
tags:
- module
timestamp: '2026-06-28'
title: internal/index
type: Module
---

# internal/index

## What it does

This module implements incremental file change detection and hash management for codebase indexing. It computes deltas between the current filesystem state and a stored manifest of file hashes, categorizing files as added, changed, removed, or unchanged. It also supports migration from legacy MD5 hashes to SHA-256 and database maintenance operations after migration.

## Public interface

```go
type DeltaResult struct {
    Changed   []string
    Added     []string
    Removed   []string
    Unchanged []string
}

func FilesToProcess(delta DeltaResult) []string
func IsLegacyHash(hash string) bool
func HashFile(path string) (string, error)
func ComputeDelta(ctx context.Context, codebaseID int64, rootPath string, storedHashes map[string]string) (DeltaResult, error)
type MigrationResult struct {
    FilesReindexed  int
    OrphanedRemoved int
    PagesReclaimed  int64
}
func RunPostMigrationMaintenance(ctx context.Context, db *sql.DB) (MigrationResult, error)
func VerifyIntegrity(ctx context.Context, db *sql.DB, codebaseID int64) (orphanCount int, err error)
```

## Key invariants

- File paths in `DeltaResult` are normalized to relative paths using forward slashes (`filepath.ToSlash`), ensuring consistent keys regardless of OS.
- A file is only classified as "unchanged" if it has a stored SHA-256 hash (64 hex characters) that exactly matches its current hash.
- Legacy MD5 hashes (any hex string shorter than 64 characters) are always treated as "changed" to force re-indexing with SHA-256.
- Files are skipped if they are not regular files, are outside the `rootPath` tree, or are filtered out by `filefilter.IsCodeFile`.
- Context cancellation is checked during directory traversal and causes `ComputeDelta` to return `ctx.Err()`.

## Non-obvious decisions

- **Legacy hash detection via length**: The decision to treat any hex string shorter than 64 characters as a legacy MD5 hash (and thus always re-index) avoids the need for explicit versioning metadata while ensuring backward compatibility. This is non-obvious because one might expect a separate version field or magic prefix, but length alone suffices given SHA-256's fixed 64-character hex representation.
- **Incremental vacuum instead of full vacuum**: Using `PRAGMA incremental_vacuum` after migration (rather than `VACUUM`) allows reclaiming free pages without locking the database for extended periods, which is critical for a long-running indexing operation. This is a deliberate performance trade-off.
- **Direct freelist count comparison for pages reclaimed**: Calculating `PagesReclaimed` as `freelist_count` before minus after `incremental_vacuum` is used instead of querying actual freed space, likely because `PRAGMA freelist_count` is the standard SQLite way to measure vacuuming impact and avoids complex space calculations.

## Unclear intent

None.
