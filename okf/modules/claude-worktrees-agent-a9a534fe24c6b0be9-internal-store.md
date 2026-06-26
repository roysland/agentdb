---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/catalog_repo.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/chunk_repo.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/cross_repo.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/edge_repo.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/helpers.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/indexed_file_repo.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/memory_repo.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/memory_repo_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/source_file_repo.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/symbol_repo.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store/workspace_repo.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/store
type: Module
---

## What it does

The `store` package provides data access layer implementations for the agentdb system, wrapping a SQLite database with repository types for codebases, symbols, chunks, edges (call/import graphs), source files, indexed files, and memories. Each repository type maps between Go structs and database tables, supporting the full lifecycle of indexing and querying codebases.

## Public interface

### CatalogRepo
- `NewCatalogRepo(db *sql.DB) *CatalogRepo`
- `RegisterCodebase(ctx, rootPath, name string) (int64, error)`
- `GetByID(ctx, id int64) (Codebase, error)`
- `ListCodebases(ctx) ([]Codebase, error)`

### SymbolRepo
- `NewSymbolRepo(db *sql.DB) *SymbolRepo`
- `Create(ctx, codebaseID int64, d SymbolData) error`
- `DeleteByCodebase(ctx, codebaseID int64) error`
- `DeleteByFile(ctx, codebaseID int64, filePath string) error`
- `FindByName(ctx, codebaseID int64, name string) ([]Symbol, error)`
- `FindByKind(ctx, codebaseID int64, kind string) ([]Symbol, error)`
- `GetByFile(ctx, codebaseID int64, filePath string) ([]Symbol, error)`
- `Stats(ctx, codebaseID int64) (map[string]int, error)`
- `TopFilesBySymbolCount(ctx, codebaseID int64, limit int) ([]map[string]any, error)`
- `FindByNameMulti(ctx, codebaseIDs []int64, name string) ([]Symbol, error)` (in `cross_repo.go`)

### ChunkRepo
- `NewChunkRepo(db *sql.DB) *ChunkRepo`
- `Create(ctx, codebaseID int64, chunk ChunkData) error`
- `CreateReturningID(ctx, codebaseID int64, chunk ChunkData) (int64, error)`
- `GetByCodebase(ctx, codebaseID int64) ([]Chunk, error)`
- `DeleteByCodebase(ctx, codebaseID int64) error`
- `DeleteByFile(ctx, codebaseID int64, filePath string) error`

### EdgeRepo
- `NewEdgeRepo(db *sql.DB) *EdgeRepo`
- `Create(ctx, codebaseID int64, d EdgeData) error`
- `DeleteByCodebase(ctx, codebaseID int64) error`
- `DeleteByFile(ctx, codebaseID int64, filePath string) error`
- `GetCallers(ctx, codebaseID int64, targetRef string) ([]Edge, error)`
- `GetCallees(ctx, codebaseID int64, fromRef string) ([]Edge, error)`
- `GetImports(ctx, codebaseID int64, filePath string) ([]Edge, error)`
- `GetDependents(ctx, codebaseID int64, targetRef string) ([]Edge, error)`
- `FindUsages(ctx, codebaseID int64, targetRef string) ([]Edge, error)`
- `GetUnresolvedImports(ctx, codebaseID int64) ([]Edge, error)`
- `ResolveCrossRepoEdge(ctx, edgeID int64, targetCodebaseID int64) error`
- `GetCallersMulti(ctx, codebaseIDs []int64, targetRef string) ([]Edge, error)` (in `cross_repo.go`)
- `FindUsagesMulti(ctx, codebaseIDs []int64, targetRef string) ([]Edge, error)` (in `cross_repo.go`)

### SourceFileRepo
- `NewSourceFileRepo(db *sql.DB) *SourceFileRepo`
- `Upsert(ctx, codebaseID int64, d SourceFileData) error`
- `DeleteByCodebase(ctx, codebaseID int64) error`
- `DeleteByFile(ctx, codebaseID int64, filePath string) error`
- `GetHashesByCodebase(ctx, codebaseID int64) (map[string]string, error)`
- `GetByCodebase(ctx, codebaseID int64) ([]SourceFile, error)`
- `Stats(ctx, codebaseID int64) (map[string]any, error)`
- `PackageList(ctx, codebaseID int64) ([]string, error)`

### IndexedFileRepo
- `NewIndexedFileRepo(db *sql.DB) *IndexedFileRepo`
- `GetHashesByCodebase(ctx, codebaseID int64) (map[string]string, error)`
- `Upsert(ctx, codebaseID int64, filePath, fileHash string, chunkCount, indexedAt int64) error`
- `UpsertWithStatus(ctx, codebaseID int64, filePath, fileHash string, chunkCount, indexedAt int64, indexStatus, statusReason string) error`
- `DeleteByFile(ctx, codebaseID int64, filePath string) error`
- `DeleteByCodebase(ctx, codebaseID int64) error`
- `GetDegradedFiles(ctx, codebaseID int64, filePaths []string) (map[string]DegradationInfo, error)`

### MemoryRepo
- `NewMemoryRepo(db *sql.DB) *MemoryRepo`
- `Create(ctx, m Memory) error`
- `GetByID(ctx, id string) (Memory, error)`
- `List(ctx, params ListMemoryParams) ([]Memory, error)`
- `DeleteByID(ctx, id string) (bool, error)`
- `UpdateByID(ctx, id string, params UpdateMemoryParams) (Memory, error)`
- `SearchLexical(ctx, query, category string, limit, workspaceID, codebaseID int64) ([]Memory, error)`
- `MarkRetrieved(ctx, id string, now int64) (Memory, error)`
- `MarkRetrievedMany(ctx, ids []string, now int64) error`

### Helper functions (helpers.go)
- `nullIfEmpty(v string) any`

## Key invariants

- All repositories hold a `*sql.DB` (or `*dbgen.Queries` for generated queries) and never manage connection lifecycle — the caller owns opening/closing.
- `DeleteByFile` on EdgeRepo matches both file-level edges (`from_ref = filePath`) and symbol-level edges (`from_ref LIKE filePath:%`), ensuring complete cleanup.
- `FindByName` and `FindByNameMulti` rank results by exact name match first, then exact qualified name match, then substring matches — this ordering is load-bearing for symbol resolution quality.
- `MemoryRepo.UpdateByID` uses a read-modify-write pattern: it fetches the current record, applies non-nil pointer fields from `UpdateMemoryParams`, then writes back. This means concurrent updates can silently overwrite each other.
- `MarkRetrievedMany` deduplicates IDs internally, so callers passing duplicate IDs will not inflate retrieval counts.
- `IndexedFileRepo.UpsertWithStatus` defaults `indexStatus` to `"complete"` when empty, ensuring backward compatibility with code that calls `Upsert`.
- Cross-repo queries (`FindByNameMulti`, `GetCallersMulti`, `FindUsagesMulti`) return empty slices (not errors) when given an empty `codebaseIDs` slice, avoiding invalid SQL.

## Non-obvious decisions

- **ChunkRepo.CreateReturningID bypasses generated queries**: It uses a raw `INSERT` via `db.ExecContext` instead of the `dbgen.Queries` path used by `Create`. This is necessary because the generated query path doesn't return the last insert ID, but it means `CreateReturningID` and `Create` could theoretically diverge if the schema changes and only one is updated.
- **EdgeRepo stores `from_ref` as a string key, not a foreign key to symbols**: The `from_ref` and `to_ref` columns store string identifiers (file paths, symbol qualified names) rather than referencing symbol IDs directly. This decouples edge creation from symbol insertion order and allows edges to reference symbols that may not yet exist (unresolved imports), but it means referential integrity is not enforced by the database.
- **MemoryRepo uses `sql.NullInt64` for optional scope fields**: `WorkspaceID` and `CodebaseID` are `*int64` in Go but stored as nullable integers. The helper `nullablePositiveInt64` treats zero and negative values as NULL, meaning a workspace or codebase ID of 0 cannot be stored — this is intentional since 0 is not a valid ID in this system.
- **`GetDegradedFiles` filters by both `index_status != 'complete'` and a provided file path list**: This is a batch lookup pattern designed to avoid N+1 queries when checking many files during incremental indexing. The caller must supply the candidate paths; the repo doesn't scan the whole codebase.

## Unclear intent

- The `Symbol` struct has an `Embedding` and `EmbeddingModel` field referenced in the `cross_repo.go` `FindByNameMulti` query SELECT, but these fields are not present in the `Symbol` struct definition or the `SymbolData` struct. This suggests either the struct is incomplete in the provided source or the query selects columns that don't map to the struct, which would cause a scan error at runtime.
