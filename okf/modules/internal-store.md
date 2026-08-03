---
commit: cc77d6b1c1d919b608eb80f7d9faa82f9dba6bc0
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
- internal/store/metrics_store.go
- internal/store/source_file_repo.go
- internal/store/symbol_repo.go
- internal/store/workspace_repo.go
tags:
- module
timestamp: '2026-08-03'
title: internal/store
type: Module
---

### What it does
The `internal/store` module provides a repository layer for managing codebase metadata, source file indexing, symbol definitions, and dependency edges within a SQLite database. It facilitates cross-codebase querying and tracks system performance metrics for indexing and analysis operations.

### Public interface

#### Repositories
- `NewCatalogRepo(db *sql.DB) *CatalogRepo`
  - `RegisterCodebase(ctx, rootPath, name) (int64, error)`
  - `ListCodebases(ctx) ([]Codebase, error)`
  - `GetByID(ctx, id) (Codebase, error)`
  - `DeleteCodebase(ctx, id) error`

- `NewChunkRepo(db *sql.DB) *ChunkRepo`
  - `Create(ctx, codebaseID, ChunkData) error`
  - `GetByCodebase(ctx, codebaseID) ([]Chunk, error)`
  - `DeleteByCodebase(ctx, codebaseID) error`

- `NewEdgeRepo(db *sql.DB) *EdgeRepo`
  - `GetCallers(ctx, codebaseID, targetRef) ([]Edge, error)`
  - `GetCallersMulti(ctx, codebaseIDs, targetRef) ([]Edge, error)`
  - `FindUsages(ctx, codebaseID, targetRef) ([]Edge, error)`
  - `ResolveCrossRepoEdge(ctx, edgeID, targetCodebaseID) error`

- `NewIndexedFileRepo(db *sql.DB) *IndexedFileRepo`
  - `Upsert(ctx, codebaseID, filePath, fileHash, chunkCount, indexedAt) error`
  - `GetDegradedFiles(ctx, codebaseID, filePaths) (map[string]DegradationInfo, error)`

- `NewMemoryRepo(db *sql.DB) *MemoryRepo`
  - `Create(ctx, Memory) error`
  - `SearchLexical(ctx, query, category, limit, workspaceID, codebaseID) ([]Memory, error)`
  - `MarkRetrievedMany(ctx, ids, now) error`

- `NewSourceFileRepo(db *sql.DB) *SourceFileRepo`
  - `Upsert(ctx, codebaseID, SourceFileData) error`
  - `Stats(ctx, codebaseID) (map[string]any, error)`

#### Metrics
- `RecordToolCall(db, tool, durationMs, isError)` (Async)
- `RecordToolCallSync(ctx, db, tool, durationMs, isError) error` (Sync)
- `RecordIndexRunSync(ctx, db, codebaseID, files, chunks, embeddingFailures, durationMs) error`
- `RecordAnalyzeRunSync(ctx, db, codebaseID, totalFiles, complete, textFallbacks, partial, panics, zeroSymbols, totalSymbols, totalEdges, durationMs) error`

### Key invariants
- **Codebase Scoping**: All data entities (Chunks, Edges, Symbols, Files) must be associated with a `codebase_id`. Operations that delete a codebase must cascade to remove all associated records.
- **Indexing Status**: Files are considered "complete" by default in `IndexedFileRepo` unless explicitly marked otherwise via `UpsertWithStatus`.
- **Metrics Persistence**: Synchronous metric functions (`*Sync`) are required for short-lived CLI processes to ensure data is flushed to disk before process exit, while asynchronous functions are intended for long-lived daemon processes.

### Non-obvious decisions
- **Manual SQL Placeholders**: The `CrossRepo` methods manually construct `IN` clauses and placeholder strings (`buildPlaceholders`) rather than using an ORM or query builder. This is done to support fan-out queries across an arbitrary number of `codebase_id`s while maintaining raw performance and control over the SQLite execution plan.
- **Degraded File Lookup**: `GetDegradedFiles` accepts a slice of `filePaths` to perform a single batch query against the database. This avoids the N+1 query problem that would occur if the caller checked the status of each file individually during a synchronization loop.
- **Async Metrics**: Metrics are recorded via `go` routines in the non-sync functions. This is a deliberate trade-off to ensure that tracking tool usage or indexing performance does not introduce latency into the critical path of the agent's operations.
