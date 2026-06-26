---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/search'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/search/blast_radius.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/search/fts.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/search/locate_issue.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/search/locate_issue_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/search
type: Module
---

## What it does

The `search` module provides two core capabilities: full-text lexical search over code chunks using SQLite FTS5 (with a LIKE fallback), and issue-to-symbol localization that ranks candidate symbols by relevance and enriches them with blast radius analysis (callers, callees, file-level dependents). Blast radius computation queries an edge store to determine which symbols and files would be affected by changes to a given symbol.

## Public interface

- `NewFTS5Search(db *sql.DB, logger *observe.Logger) (*FTS5Search, error)` — constructs an FTS5 search instance.
- `(s *FTS5Search) EnsureIndex(ctx context.Context) error` — creates the FTS5 virtual table and sync triggers if missing.
- `(s *FTS5Search) SearchLexical(ctx context.Context, query string, codebaseID int64, limit int) ([]FTS5Result, error)` — executes a ranked full-text search with automatic fallback.
- `(s *FTS5Search) IsAvailable(ctx context.Context) bool` — checks whether the FTS5 index is queryable.
- `ComputeBlastRadius(ctx context.Context, edgeRepo *store.EdgeRepo, codebaseID int64, sym store.Symbol) (BlastRadius, error)` — returns callers, callees, and file dependents for a symbol.
- `LocateIssue(ctx context.Context, db *sql.DB, cfg LocateIssueConfig, logger *observe.Logger) ([]LocateIssueResult, string, error)` — performs multi-codebase FTS5 search, scores candidates, and enriches with blast radius and cross-repo links.
- `ComputeConfidenceScoreLexicalOnly(normalizedBM25 float64) float64` — clamps normalized BM25 to [0, 1].
- `NormalizeBM25(bm25Score, maxAbsBM25Score float64) float64` — negates and normalizes a raw BM25 score.
- `MaxAbsBM25Score(results []FTS5Result) float64` — returns the maximum absolute BM25 score from a result set.

## Key invariants

- BM25 scores from FTS5 are negative (lower = better); normalization negates them so higher = better, producing a [0, 1] confidence score.
- `SearchLexical` always falls back: first the raw query, then an escaped query, then a LIKE-based search — it never returns an error for query syntax issues alone.
- Candidates with confidence below 0.1 are filtered out before results are returned.
- `LocateIssue` fetches `max(limit * 5, 50)` FTS5 candidates per codebase before scoring and truncating to `limit`, ensuring the final top-N are selected from a sufficiently large pool.
- Candidates are deduplicated by `(codebaseID, filePath, name)`; on collision the better (more negative) BM25 score is kept.
- `escapeFTS5Query` strips or replaces all FTS5 special characters and collapses whitespace, guaranteeing the escaped query is syntactically valid.

## Non-obvious decisions

- **Heuristic confidence bonus in `locateIssueConfidenceBonus`**: Runtime kinds (`func`, `method`) receive +0.15 while constants/vars receive −0.15, and file paths containing `/llm/`, `/handler`, `/router/`, etc. receive +0.08. This is a hand-tuned heuristic to prefer runtime entry points over generated SQL or string-heavy constants when localizing an issue. A more principled approach would use structural signals (e.g., call graph depth, test coverage) rather than path/name matching, but the current approach is lightweight and deterministic.

- **Parallel per-codebase FTS5 search**: Each codebase ID is searched in a separate goroutine with a mutex-protected result slice. This avoids sequential latency when multiple codebases are queried, but the `candidateLimit` is applied per-codebase rather than globally, meaning a codebase with many matches can dominate the candidate pool.

- **`getCrossRepoLinks` queries both outbound and inbound edges**: It checks edges where `from_ref` matches the symbol in the current codebase (outbound) and also iterates over all sibling codebase IDs to find inbound `calls` edges. The inbound loop runs N additional queries, which could be replaced with a single `WHERE to_ref = ? AND codebase_id IN (...)` query.

## Unclear intent

- **`QualifiedName` is set to `c.name` in `LocateIssue`**: When constructing the `store.Symbol` for blast radius computation, `QualifiedName` is assigned the plain `name` field rather than a fully-qualified name (e.g., `package.Type.Method`). This means `GetCallers` and `GetCallees` look up by the unqualified name, which may match unintended symbols or miss the correct one. It is unclear whether the edge store keys by unqualified names or whether this is a simplification that should be revisited.
