---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/artifact'
files:
- internal/artifact/export.go
- internal/artifact/import.go
- internal/artifact/schema.go
- internal/artifact/w4_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/artifact
type: Module
---

**What it does**

The `artifact` module implements export and import of a single codebase's indexed data as a standalone SQLite file. Export copies rows (chunks, symbols, edges, source_files, indexed_files) from the main database into a new SQLite file with the artifact schema, optionally stripping source-bearing text fields. Import reads that artifact file, validates its schema version, and upserts the data into a destination database with codebase matching by `root_path`.

**Public interface**

- `func Export(ctx context.Context, srcDB *sql.DB, opts ExportOptions) error`
- `type ExportOptions struct { CodebaseID int64; OutputPath string; StripSource bool }`
- `func Import(ctx context.Context, dstDB *sql.DB, opts ImportOptions) error`
- `type ImportOptions struct { ArtifactPath string; NameOverride string }`
- `var SupportedSchemaVersions []string`
- `func applyArtifactSchema(ctx context.Context, db *sql.DB) error`

**Key invariants**

- `Export` always writes `schema_version = 2` into the artifact's `meta` table, regardless of what the source database uses.
- `Import` rejects any artifact whose `schema_version` is not in `{"1", "2"}`.
- `Import` upserts the codebase row by `root_path` (not by `id`), so the local `codebase_id` may differ from the artifact's original.
- All bulk-copy operations in both `Export` and `Import` run inside a single transaction.
- `target_codebase_id` on edges is preserved as-is (not remapped) during both export and import.
- When `StripSource` is true, `signature`, `snippet`, `doc_comment`, and `body_snippet` are replaced with empty strings, and `closed_source` / `source_stripped` metadata keys are set to `"true"` in both `meta` and `codebase_meta`.
- `codebase_meta` is keyed by `(codebase_id, key)`, making it multi-codebase-safe, while `meta` remains a flat global table.

**Non-obvious decisions**

- **ATTACH-based bulk copy instead of reading into Go and writing back**: Both export and import attach the other database file to the current connection and use `INSERT ... SELECT` across the attach boundary. This avoids loading entire tables into memory and is significantly faster for large codebases, but it requires the SQLite driver to support `ATTACH` (the test code explicitly skips on drivers that don't).
- **Schema split between `meta` and `codebase_meta`**: `meta` carries global/legacy keys (`schema_version`, `closed_source`, `source_stripped`), while `codebase_meta` stores per-codebase metadata. During import, `closed_source` and `source_stripped` are excluded from the global `meta` copy and routed to `codebase_meta` instead. This is a forward-looking split for multi-codebase correctness, but older artifacts only have the global keys, so `importCodebaseMeta` falls back to reading them from `meta`.
- **FTS5 virtual table is silently skipped if unavailable**: `applyArtifactSchema` catches `no such module: fts5` errors and continues without the `chunks_fts` table and its triggers. This allows the same schema to be applied on builds/drivers that don't include FTS5, at the cost of full-text search capability in the artifact.
- **Statement splitter handles `CREATE TRIGGER` bodies**: The `splitArtifactStatements` function tracks `BEGIN ... END` trigger bodies to avoid splitting on semicolons inside them. This is necessary because the DDL embeds trigger logic with internal semicolons.

**Unclear intent**

- The `workspaces` and `workspace_members` tables are defined in `ArtifactDDL` and created in every exported artifact, but no code in this module ever reads or writes data to them. Their presence in the artifact schema appears to be for structural parity with the main database, but whether they should carry data during export is not addressed anywhere.
