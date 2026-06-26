---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/search'
files:
- internal/search/blast_radius.go
- internal/search/fts.go
- internal/search/locate_issue.go
- internal/search/locate_issue_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/search
type: Module
---

## What it does

The `search` module provides two core capabilities: full-text lexical search over code chunks using SQLite FTS5 (with a LIKE fallback), and issue-to-symbol localization that ranks candidate symbols by relevance and enriches them with blast radius analysis (callers, callees, file-level dependents) and cross-repository links.

## Public interface

```go
// FTS5 search
func NewFTS5Search(db *sql.DB, logger *observe.Logger) (*FTS5Search, error)
func (s *FTS5Search) EnsureIndex(ctx context.Context) error
func (s *FTS5Search) SearchLexical(ctx context.Context, query string, codebaseID int64, limit int) ([]FTS5Result, error)
func (s *FTS5Search) IsAvailable(ctx context.Context) bool

// Blast radius
func ComputeBlastRadius(ctx context.Context, edgeRepo *store.EdgeRepo, codebaseID int64, sym store.Symbol) (BlastRadius, error)

// Issue localization
func LocateIssue(ctx context.Context, db *sql.DB, cfg LocateIssueConfig, logger *observe.Logger) ([]LocateIssueResult, string, error)
func ComputeConfidenceScoreLexicalOnly(normalizedBM25 float64) float64
func NormalizeBM25(bm25Score, maxAbsBM25Score float64) float64
func MaxAbsBM25Score(results []FTS5Result) float64
```

**Types:**

| Type | Key fields |
|------|-----------|
| `FTS5Result` | `ChunkID`, `FilePath`, `Name`, `Kind`, `Snippet`, `StartLine`, `EndLine`, `BM25Score` |
| `BlastRadius` | `Callers`, `Callees`, `Dependents` |
| `LocateIssueResult` | `Symbol`, `ConfidenceScore`, `BlastRadius`, `Chunks`, `CrossRepoLinks` |
| `LocateIssueConfig` | `IssueText`, `CodebaseIDs`, `Limit` |

## Key invariants

- BM25 scores from FTS5 are **negative** (lower = better); `NormalizeBM25` negates to produce a positive 0–1 range.
- `SearchLexical` tries three strategies in order: raw FTS5 MATCH → escaped FTS5 MATCH → SQL LIKE fallback. The LIKE path returns `BM25Score = 0`.
- `LocateIssue` fetches `max(limit*5, 50)` candidates per codebase for scoring, then truncates to `limit` after ranking.
- Candidates below a confidence threshold of **0.1** are filtered out entirely.
- `EnsureIndex` is idempotent: all statements use `IF NOT EXISTS`.
- `LocateIssue` runs FTS5 searches across codebases in parallel with `sync.WaitGroup` + mutex; a single codebase failure is logged but does not abort the entire operation.
- Cross-repo links are only computed when `len(CodebaseIDs) > 1`.

## Non-obvious decisions

- **`QualifiedName` is set to `Name` in `LocateIssue`** when constructing the `store.Symbol` for blast radius lookup. This means blast radius queries use the bare symbol name rather than a fully-qualified path (e.g., `pkg.Type.Method`). This is likely intentional for simpler edge matching, but could miss edges that are keyed on fully-qualified names — worth verifying against how `EdgeRepo` stores `from_ref`/`to_ref`.

- **Confidence bonus for `func`/`method` kinds (+0.15) and penalty for `const`/`var`/`field` (-0.15)**: The heuristic deliberately ranks runtime entry points above data declarations. The SQL-file penalty (-0.2 for `.sql.go` paths) pushes auto-generated code further down. These magic numbers are not configurable.

- **`escapeFTS5Query` strips most special characters rather than quoting them**: Characters like `"`, `*`, `(`, `)`, `:`, `^`, `~`, `|`, `{`, `}` are removed entirely; `-` and `+` are replaced with spaces. This is a pragmatic approach but means queries containing these characters silently degrade to a different query rather than surfacing an error to the caller.

## Unclear intent

- **`getCrossRepoLinks` queries `edges` using `from_ref = symbolName`**: The edge table's `from_ref` typically stores a qualified name or file-scoped identifier, but the lookup uses the bare `name` field from the chunks table. Whether this produces correct cross-repo links depends on whether edge `from_ref` values are bare names — this is not verifiable from this module alone and should be confirmed against the `store` and `index` modules.
