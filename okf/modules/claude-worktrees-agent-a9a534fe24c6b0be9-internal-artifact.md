---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/artifact'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/artifact/export.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/artifact/import.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/artifact/schema.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/artifact/w4_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/artifact
type: Module
---

## What it does

The `artifact` package implements export and import of codebase analysis data as standalone SQLite files. Export copies a single codebase's chunks, symbols, edges, source files, and indexed file metadata from the main database into a new artifact database; import reads that artifact back into a local database with codebase-level deduplication. Both operations use SQLite's `ATTACH DATABASE` to bulk-copy rows without intermediate serialization.

## Public interface

```go
func Export(ctx context.Context, srcDB *sql.DB, opts ExportOptions) error

type ExportOptions struct {
    CodebaseID  int64
    OutputPath  string
    StripSource bool
}

func Import(ctx context.Context, dstDB *sql.DB, opts ImportOptions) error

type ImportOptions struct {
    ArtifactPath string
    NameOverride string
}

func applyArtifactSchema(ctx context.Context, db *sql.DB) error
var SupportedSchemaVersions []string
var ArtifactDDL string
```

## Key invariants

- `Export` verifies the codebase exists before doing any work; it returns an error for unknown `CodebaseID`.
- `Export` always writes `schema_version = 2` into the artifact's `meta` table, regardless of what the source database uses.
- `Export` writes `closed_source` and `source_stripped` to both the global `artifact.meta` and the per-codebase `codebase_meta` table; `Import` reads from `codebase_meta` first and falls back to global `meta` for older artifacts.
- `Import` validates `schema_version` against `SupportedSchemaVersions` before reading any data.
- `Import` deletes all existing rows for the target `codebase_id` from `chunks`, `symbols`, `edges`, `source_files`, and `indexed_files` before inserting, making imports idempotent for a given codebase.
- `Import` matches existing codebases by `root_path` (not by `id`), using `ON CONFLICT(root_path) DO UPDATE`.
- `target_codebase_id` on edges is preserved as-is during both export and import; it is a foreign key reference that may point to a codebase not present in the artifact.
- `applyArtifactSchema` silently skips FTS5 virtual table and trigger creation if the SQLite build lacks `fts5` support, allowing artifacts to be opened on minimal SQLite builds.

## Non-obvious decisions

- **ATTACH DATABASE for bulk copy**: Rather than reading rows into memory and re-inserting, the code attaches the artifact file to the source/destination connection and runs cross-database `INSERT … SELECT`. This avoids serialization overhead but means both databases must be accessible from the same connection — it cannot work over a network or with an in-memory source and file-based artifact without careful path handling.
- **Strip source at export, not at query time**: `StripSource` replaces `signature`, `snippet`, `doc_comment`, and `body_snippet` with empty strings during export rather than providing a view or post-processing step. This means the artifact file itself contains no source-bearing text, which is a one-way decision — once exported with stripping, the original text cannot be recovered from the artifact.
- **Schema split between global and per-codebase meta**: `closed_source` and `source_stripped` are duplicated in both `meta` and `codebase_meta`. The export path writes to both; the import path prefers `codebase_meta` with a fallback. This dual-write exists to remain backward-compatible with version 1 artifacts that only had global metadata.
- **Trigger-aware DDL splitter**: `splitArtifactStatements` handles `CREATE TRIGGER` bodies specially because triggers contain semicolons inside `BEGIN … END` blocks that would break a naive `strings.Split(sql, ";")` approach.

## Unclear intent

- **`target_codebase_id` semantics**: The field is preserved faithfully, but the codebase has no documentation on what a non-null `target_codebase_id` means operationally — whether it implies a cross-codebase edge that should be resolved, ignored, or surfaced differently in search/orient. The import/export code treats it as an opaque integer, which is correct for round-tripping but leaves downstream consumers without guidance.
