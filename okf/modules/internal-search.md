---
commit: 99a4eb8721ee6257678f63653cf97d856a65aa60
description: 'Codebase knowledge for module: internal/search'
files:
- internal/search/blast_radius.go
- internal/search/fts.go
- internal/search/locate_issue.go
- internal/search/locate_issue_test.go
tags:
- module
timestamp: '2026-06-28'
title: internal/search
type: Module
---

# internal/search

## What it does

This module provides lexical full-text search and symbol ranking for codebase-wide issue localization. It uses SQLite FTS5 to search code chunks, ranks candidates using BM25 scores with heuristic adjustments, and enriches top results with blast radius analysis (callers, callees, dependents) and cross-repository links.

## Public interface

```go
type FTS5Search struct {
    // No exported fields
}

func NewFTS5Search(db *sql.DB, logger *observe.Logger) (*FTS5Search, error)
func (s *FTS5Search) EnsureIndex(ctx context.Context) error
func (s *FTS5Search) SearchLexical(ctx context.Context, query string, codebaseID int64, limit int) ([]FTS5Result, error)
func (s *FTS5Search) IsAvailable(ctx context.Context) bool

type LocateIssueResult struct {
    Symbol          store.Symbol
    ConfidenceScore float64
    BlastRadius     BlastRadius
    Chunks          []ChunkSnippet
    CrossRepoLinks  []CrossLink
}

type BlastRadius struct {
    Callers    []string
    Callees    []string
    Dependents []string
}

type ChunkSnippet struct {
    FilePath  string
    Name      string
    Snippet   string
    StartLine int64
    EndLine   int64
}

type CrossLink struct {
    TargetCodebaseID int64
    TargetSymbol     string
    EdgeKind         string
}

type LocateIssueConfig struct {
    IssueText   string
    CodebaseIDs []int64
    Limit       int
}

func LocateIssue(ctx context.Context, db *sql.DB, cfg LocateIssueConfig, logger *observe.Logger) ([]LocateIssueResult, string, error)

func ComputeConfidenceScoreLexicalOnly(normalizedBM25 float64) float64
func NormalizeBM25(bm25Score, maxAbsBM25Score float64) float64
func MaxAbsBM25Score(results []FTS5Result) float64
func ComputeBlastRadius(ctx context.Context, edgeRepo *store.EdgeRepo, codebaseID int64, sym store.Symbol) (BlastRadius, error)
```

## Key invariants

- FTS5 `bm25()` scores are negative in SQLite; normalization negates them so higher values indicate better matches.
- Confidence scores are always clamped to `[0.0, 1.0]`.
- Candidates below `0.1` confidence threshold are filtered out before returning results.
- Test files (`*test.go`, `*_test.go`) and non-implementation files are excluded from final candidates.
- Import-only chunks are excluded from candidates.
- Deduplication key for candidates is `"codebaseID:filePath:name"`.

## Non-obvious decisions

- **FTS5 fallback chain**: When the initial FTS5 query fails (e.g., due to special characters), the code escapes FTS5 special characters and retries; if that also fails, it falls back to a `LIKE` query. This ensures robustness against malformed queries while preserving ranking where possible.
  
- **Candidate limit scaling**: `candidateLimit` is set to `cfg.Limit * 5` (minimum 50) before scoring. This over-fetches candidates to ensure sufficient diversity for confidence-based ranking, since early filtering would discard potentially high-confidence symbols that only rank well after heuristic adjustments.

- **Lexical re-ranking bonus**: A small bonus (up to `+0.08`) is added when query terms appear in the symbol name or file path. This breaks ties between candidates with identical BM25 scores but different semantic relevance to the issue text.

- **Runtime entry point bias**: Functions/methods receive a `+0.15` confidence bonus, while constants/variables receive `-0.15`. Files like `*.sql.go` or `/db/` paths are penalized, while `/llm/`, `/handler/`, `/api/` paths are boosted. This reflects the heuristic that runtime entry points are more likely to be relevant to user-reported issues than generated or infrastructure code.

- **Cross-repo link collection strategy**: For multi-codebase searches, inbound links are collected by iterating over *all* provided `CodebaseIDs` and querying each for edges pointing to the current symbol. This ensures bidirectional cross-repository visibility without assuming a fixed repository hierarchy.
