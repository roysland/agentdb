---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/data/gen'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/data/gen/db.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/data/gen/models.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/data/gen/queries.sql.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/data/gen
type: Module
---

## What it does

This module contains sqlc-generated Go code that provides a typed database access layer for the agentdb system. It defines structs mapping to database tables and query methods that wrap raw SQL with type-safe Go signatures. The code is auto-generated and should not be hand-edited.

## Public interface

- `New(db DBTX) *Queries` — Constructs a `Queries` instance from any `DBTX`-compatible source (`*sql.DB` or `*sql.Tx`).
- `(*Queries) WithTx(tx *sql.Tx) *Queries` — Returns a `Queries` bound to a transaction.
- `(*Queries) CreateChunk(ctx, CreateChunkParams) error` — Upserts a chunk row keyed on `(codebase_id, chunk_key)`.
- `(*Queries) CreateMemory(ctx, CreateMemoryParams) error` — Inserts a memory row.
- `(*Queries) DeleteChunksByCodebase(ctx, codebaseID) error` — Deletes all chunks for a codebase.
- `(*Queries) DeleteMemoryByID(ctx, id) (int64, error)` — Deletes a memory by ID; returns rows affected.
- `(*Queries) GetChunksByCodebase(ctx, codebaseID) ([]Chunk, error)` — Returns chunks ordered by file path and start line.
- `(*Queries) GetMemoryByID(ctx, id) (Memory, error)` — Returns a single memory row.
- `(*Queries) ListCodebases(ctx) ([]Codebasis, error)` — Returns all codebases ordered by ID descending.
- `(*Queries) ListMemoriesFiltered(ctx, ListMemoriesFilteredParams) ([]Memory, error)` — Lists memories filtered by category, workspace, and codebase with a limit.
- `(*Queries) MarkMemoryRetrieved(ctx, MarkMemoryRetrievedParams) (int64, error)` — Increments retrieval count and sets last_retrieved.
- `(*Queries) RegisterCodebase(ctx, RegisterCodebaseParams) (int64, error)` — Inserts a codebase and returns the new ID.
- `(*Queries) SearchMemoriesLexicalFiltered(ctx, SearchMemoriesLexicalFilteredParams) ([]Memory, error)` — Lexical LIKE search over memory content and category with filters.
- `(*Queries) UpdateMemory(ctx, UpdateMemoryParams) (int64, error)` — Updates a memory row; returns rows affected.

## Key invariants

- `CreateChunk` uses `ON CONFLICT … DO UPDATE` (upsert) keyed on `(codebase_id, chunk_key)`, so there is never more than one chunk per codebase with a given key.
- `ListMemoriesFiltered` and `SearchMemoriesLexicalFiltered` use SQLite-style `?N` positional parameters, with `?1` reused for multiple references to the same value (content and category LIKE patterns).
- `RegisterCodebase` returns `LastInsertId()`, assuming the `id` column is an autoincrement integer primary key.
- All nullable Go fields map to `sql.NullInt64` / `sql.NullString` columns, and the corresponding SQL columns allow NULLs.
- `WithTx` allows the same query set to run inside a transaction, preserving atomicity across multiple operations.

## Non-obvious decisions

- `ListMemoriesFiltered` accepts `interface{}` for filter parameters (`Category`, `WorkspaceID`, `CodebaseID`) rather than concrete types. This is a sqlc convention to allow passing either a value or `NULL` without the caller constructing `sql.NullInt64` directly — the generated code relies on the driver to handle the nil-vs-value distinction.
- `SearchMemoriesLexicalFiltered` passes `?1` (content LIKE pattern) and `?2` (category LIKE pattern) as separate parameters even though they may hold the same value. This is required by sqlc's positional parameter numbering — each `?N` placeholder maps to exactly one Go argument, so the same logical value must be passed twice if used in two clauses.
