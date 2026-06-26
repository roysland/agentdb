---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/store'
files:
- internal/store/catalog_repo.go
- internal/store/chunk_repo.go
- internal/store/cross_repo.go
- internal/store/edge_repo.go
- internal/store/helpers.go
- internal/store/indexed_file_repo.go
- internal/store/memory_repo.go
- internal/store/memory_repo_test.go
- internal/store/source_file_repo.go
- internal/store/symbol_repo.go
- internal/store/workspace_repo.go
tags:
- module
timestamp: '2026-06-26'
title: internal/store
type: Module
---

## What it does

The `internal/store` package provides the data access layer for AgentDB, wrapping a SQLite database with repository structs for each domain entity (codebases, symbols, chunks, edges, source files, indexed files, memories, and workspaces). It uses generated SQL queries from `data/gen` alongside hand-written SQL for more complex operations, and supports scoped queries across multiple codebases via cross-repo fan-out methods.

## Public interface

### CatalogRepo
```go
func NewCatalogRepo(db *sql.DB) *CatalogRepo
func (r *CatalogRepo) RegisterCodebase(ctx context.Context, rootPath, name string) (int64, error)
func (r *CatalogRepo) GetByID(ctx context.Context, id int64) (Codebase, error)
func (r *CatalogRepo) ListCodebases(ctx context.Context) ([]Codebase, error)
```

### SymbolRepo
```go
func NewSymbolRepo(db *sql.DB) *SymbolRepo
func (r *SymbolRepo) Create(ctx context.Context, codebaseID int64, d SymbolData) error
func (r *SymbolRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error
func (r *SymbolRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error
func (r *SymbolRepo) FindByName(ctx context.Context, codebaseID int64, name string) ([]Symbol, error)
func (r *SymbolRepo) FindByKind(ctx context.Context, codebaseID int64, kind string) ([]Symbol, error)
func (r *SymbolRepo) GetByFile(ctx context.Context, codebaseID int64, filePath string) ([]Symbol, error)
func (r *SymbolRepo) Stats(ctx context.Context, codebaseID int64) (map[string]int, error)
func (r *SymbolRepo) TopFilesBySymbolCount(ctx context.Context, codebaseID int64, limit int) ([]map[string]any, error)
func (r *SymbolRepo) ExportedFuncs(ctx context.Context, codebaseID int64, limit int) ([]Symbol, error)
func (r *SymbolRepo) FindByNameMulti(ctx context.Context, codebaseIDs []int64, name string) ([]Symbol, error)
```

### ChunkRepo
```go
func NewChunkRepo(db *sql.DB) *ChunkRepo
func (r *ChunkRepo) Create(ctx context.Context, codebaseID int64, chunk ChunkData) error
func (r *ChunkRepo) CreateReturningID(ctx context.Context, codebaseID int64, chunk ChunkData) (int64, error)
func (r *ChunkRepo) GetByCodebase(ctx context.Context, codebaseID int64) ([]Chunk, error)
func (r *ChunkRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error
func (r *ChunkRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error
```

### EdgeRepo
```go
func NewEdgeRepo(db *sql.DB) *EdgeRepo
func (r *EdgeRepo) Create(ctx context.Context, codebaseID int64, d EdgeData) error
func (r *EdgeRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error
func (r *EdgeRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error
func (r *EdgeRepo) GetCallers(ctx context.Context, codebaseID int64, targetRef string) ([]Edge, error)
func (r *EdgeRepo) GetCallees(ctx context.Context, codebaseID int64, fromRef string) ([]Edge, error)
func (r *EdgeRepo) GetImports(ctx context.Context, codebaseID int64, filePath string) ([]Edge, error)
func (r *EdgeRepo) GetDependents(ctx context.Context, codebaseID int64, targetRef string) ([]Edge, error)
func (r *EdgeRepo) FindUsages(ctx context.Context, codebaseID int64, targetRef string) ([]Edge, error)
func (r *EdgeRepo) GetUnresolvedImports(ctx context.Context, codebaseID int64) ([]Edge, error)
func (r *EdgeRepo) ResolveCrossRepoEdge(ctx context.Context, edgeID int64, targetCodebaseID int64) error
func (r *EdgeRepo) GetCallersMulti(ctx context.Context, codebaseIDs []int64, targetRef string) ([]Edge, error)
func (r *EdgeRepo) FindUsagesMulti(ctx context.Context, codebaseIDs []int64, targetRef string) ([]Edge, error)
```

### SourceFileRepo
```go
func NewSourceFileRepo(db *sql.DB) *SourceFileRepo
func (r *SourceFileRepo) Upsert(ctx context.Context, codebaseID int64, d SourceFileData) error
func (r *SourceFileRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error
func (r *SourceFileRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error
func (r *SourceFileRepo) GetHashesByCodebase(ctx context.Context, codebaseID int64) (map[string]string, error)
func (r *SourceFileRepo) GetByCodebase(ctx context.Context, codebaseID int64) ([]SourceFile, error)
func (r *SourceFileRepo) Stats(ctx context.Context, codebaseID int64) (map[string]any, error)
func (r *SourceFileRepo) PackageList(ctx context.Context, codebaseID int64) ([]string, error)
```

### IndexedFileRepo
```go
func NewIndexedFileRepo(db *sql.DB) *IndexedFileRepo
func (r *IndexedFileRepo) GetHashesByCodebase(ctx context.Context, codebaseID int64) (map[string]string, error)
func (r *IndexedFileRepo) Upsert(ctx context.Context, codebaseID int64, filePath, fileHash string, chunkCount int64, indexedAt int64) error
func (r *IndexedFileRepo) UpsertWithStatus(ctx context.Context, codebaseID int64, filePath, fileHash string, chunkCount int64, indexedAt int64, indexStatus, statusReason string) error
func (r *IndexedFileRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error
func (r *IndexedFileRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error
func (r *IndexedFileRepo) GetDegradedFiles(ctx context.Context, codebaseID int64, filePaths []string) (map[string]DegradationInfo, error)
```

### MemoryRepo
```go
func NewMemoryRepo(db *sql.DB) *MemoryRepo
func (r *MemoryRepo) Create(ctx context.Context, m Memory) error
func (r *MemoryRepo) GetByID(ctx context.Context, id string) (Memory, error)
func (r *MemoryRepo) List(ctx context.Context, params ListMemoryParams) ([]Memory, error)
func (r *MemoryRepo) DeleteByID(ctx context.Context, id string) (bool, error)
func (r *MemoryRepo) UpdateByID(ctx context.Context, id string, params UpdateMemoryParams) (Memory, error)
func (r *MemoryRepo) SearchLexical(ctx context.Context, query, category string, limit int, workspaceID, codebaseID int64) ([]Memory, error)
func (r *MemoryRepo) MarkRetrieved(ctx context.Context, id string, now int64) (Memory, error)
func (r *MemoryRepo) MarkRetrievedMany(ctx context.Context, ids []string, now int64) error
```

### Cross-repo helpers (in `cross_repo.go`)
```go
func buildPlaceholders(n int) string
func idsToArgs(ids []int64) []interface{}
```

## Key invariants

- **Codebase scoping**: Nearly every query filters by `codebaseID`; the multi-repo variants use `codebase_id IN (...)` with dynamically built placeholders.
- **Upsert on `(codebase_id, file_path)`**: `source_files`, `indexed_files`, and `chunks` use composite unique constraints keyed by codebase + file path, with `ON CONFLICT ... DO UPDATE` for idempotent re-indexing.
- **Soft deletion by file**: `DeleteByFile` methods on `ChunkRepo`, `EdgeRepo`, `SymbolRepo`, `SourceFileRepo`, and `IndexedFileRepo` enable incremental re-indexing without wiping the entire codebase.
- **Edge resolution tracking**: Edges carry a `resolved` boolean and optional `target_codebase_id`; `GetUnresolvedImports` feeds a resolution pass, and `ResolveCrossRepoEdge` marks them complete.
- **Memory scoping**: Memories can be filtered by `workspace_id` and `codebase_id`; zero or NULL values are treated as "global" scope (unscoped).
- **Degradation tracking**: `indexed_files` carries `index_status` and `status_reason` columns; `GetDegradedFiles` enables callers to detect and re-process files that did not index cleanly.

## Non-obvious decisions

- **Hybrid query approach**: Some repos (`CatalogRepo`, `MemoryRepo`, `ChunkRepo`) delegate to generated `*dbgen.Queries` for standard CRUD, while others (`SymbolRepo`, `EdgeRepo`, `SourceFileRepo`, `IndexedFileRepo`) write raw SQL directly. The split appears to follow complexity: simple CRUD uses the generator, while queries requiring `LIKE` matching, `ORDER BY CASE`, or dynamic `IN (...)` clauses are hand-written because sqlc does not express them cleanly.

- **`MarkRetrievedMany` deduplicates IDs in-memory before issuing updates**: The method builds a `seen` map to avoid issuing duplicate `UPDATE` statements for the same ID. This suggests callers may pass redundant ID lists (e.g., from overlapping search results), and the dedup prevents inflating `retrieval_count` or triggering unnecessary writes.

- **`DeleteByFile` on `EdgeRepo` uses `from_ref = ? OR from_ref LIKE ?` with `filePath+"%"**: Edges store `file_path` or `file_path:symbol` as `from_ref`. The `LIKE` with a `:` suffix ensures both file-level edges (e.g., imports) and symbol-level edges (e.g., calls from a function in that file) are captured without a separate join.

- **`FindByName` ordering uses a `CASE` expression**: Results are ranked so exact `name` matches come first, then exact `qualified_name` matches, then substring matches. This prioritizes precision over alphabetical order, which matters when a short symbol name could match many qualified names across a large codebase.

- **`nullIfEmpty` in `helpers.go`**: Empty or whitespace-only strings are stored as SQL `NULL` rather than `""`. This keeps `LIKE` queries from matching empty-string noise and allows `IS NULL` checks for "unset" semantics.

## Unclear intent

- **`ChunkRepo.CreateReturningID` exists alongside `ChunkRepo.Create`**: `Create` uses the generated query (which does not return the ID), while `CreateReturningID` uses a raw `ExecContext` + `LastInsertId()`. It is unclear why some callers need the ID immediately while others do not; the generated query could likely be configured to return the ID, making the hand-written variant redundant.

- **`SourceFileRepo` vs `IndexedFileRepo` both store `file_path` + `file_hash`**: These two tables appear to track overlapping concepts (indexed files and their hashes). `SourceFileRepo` carries richer metadata (language, package, LOC, symbol count), while `IndexedFileRepo` tracks chunk count and index status. The exact division of responsibility—when a file appears in one table but not the other—is not documented and may reflect an evolving schema.
