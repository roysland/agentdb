---
type: Reference
title: agentdb — behavioral and data-flow reference
description: End-to-end narrative of how agentdb operates, from setup through the two independent indexing pipelines to query
tags: [architecture, data-flow]
---

## Setup (run once per database)

```
agentdb bootstrap
agentdb codebase register --path /repo --name myrepo   # → codebase_id N
```

`bootstrap` opens (or creates) the SQLite database and applies `data/schema.sql` idempotently. The schema embeds in the binary at compile time via `//go:embed`; if the binary's embedded copy is unavailable it falls back to reading from disk.

`codebase register` inserts a row into the `codebases` table and returns the numeric ID that every subsequent command requires.

---

## The two pipelines

agentdb has two independent indexing pipelines. They share the same database but write to completely separate table groups. Run them in any order; they do not depend on each other.

```
┌─ PIPELINE A: Symbol Graph ─────────────────────────────────────────┐
│                                                                     │
│  agentdb analyze --codebase-id N --codebase-path /repo             │
│                                                                     │
│  parse.ParseDirectory(path, parsers)                                │
│    └─ for each source file                                          │
│         Parser.Parse(filePath, content) → FileResult               │
│           ├─ .Symbols  ──────────────────→  symbols        table   │
│           ├─ .Edges    ──────────────────→  edges          table   │
│           └─ .LOC / .PackageName ────────→  source_files   table   │
│                                                                     │
│  resolveCrossRepoLinks()                                            │
│    └─ if codebase is in a workspace: match unresolved import edges  │
│       against symbols in other member codebases                     │
│                                                                     │
│  Incremental manifest: source_files.file_hash                       │
└─────────────────────────────────────────────────────────────────────┘

┌─ PIPELINE B: Chunk / FTS5 Index ───────────────────────────────────┐
│                                                                     │
│  agentdb index --codebase-id N --codebase-path /repo               │
│                                                                     │
│  chunk.ChunkDirectory(path)                                         │
│    └─ for each source file                                          │
│         ChunkFile(path) → []Chunk  (default: 50-line windows)      │
│           ├─ chunkRepo.Create() ────────→  chunks          table   │
│           │    └─ AFTER INSERT trigger ─→  chunks_fts  (FTS5/BM25) │
│           └─ indexedFileRepo.Upsert() ──→  indexed_files   table   │
│                                                                     │
│  Incremental manifest: indexed_files.file_hash                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Pipeline A — Symbol graph (analyze)

Parsers are either built-in (Go, generic text) or loaded from the plugin registry at startup. Each `Parser.Parse` call returns a `FileResult` containing:

- **Symbols** — AST-extracted declarations: functions, types, methods, constants. Each carries `qualified_name`, `signature`, `body_snippet`, `start_line`, `end_line`, `file_hash`.
- **Edges** — directed relationships: `imports`, `calls`, `uses_type`. Each edge has `from_ref`, `to_ref`, `edge_kind`, and a `resolved` flag.
- **File metadata** — language, package name, LOC, file hash — stored in `source_files`.

`source_files` doubles as the incremental manifest for this pipeline: on `--incremental`, the stored `file_hash` values are compared against fresh hashes of the current files to compute a delta (changed / added / removed). Only changed and added files are re-parsed; removed files have their symbols and edges deleted.

Cross-repo link resolution runs after all files are stored. If the codebase belongs to a workspace, each unresolved import edge (`resolved = 0`) is matched by `to_ref` against `qualified_name` and `name` in the symbols of other workspace members. On a match, `edges.resolved` is set to 1 and `edges.target_codebase_id` is filled.

---

## Pipeline B — Chunk index (index)

Chunking is language-agnostic: files are split into fixed-size line windows (default 50 lines, configurable via `--lines-per-chunk` or the `AGENTDB_INDEX_LINES_PER_CHUNK` env var). Each chunk stores the raw snippet, file hash, start/end lines, and optional symbol metadata when the chunk boundary aligns with a known symbol.

**FTS5 is populated by triggers, not by the indexer directly.** Three triggers on the `chunks` table (`chunks_ai`, `chunks_ad`, `chunks_au`) keep `chunks_fts` in sync automatically on every insert, delete, and update. The indexer never touches `chunks_fts`; it only writes to `chunks`. This means the FTS5 index is always consistent — there is no separate "build index" step and no risk of the FTS5 table going stale.

`indexed_files` is the incremental manifest for this pipeline: it stores one row per file with the file hash and chunk count. On `--incremental`, the same delta computation as Pipeline A applies, but operates independently of `source_files`. The two manifests can diverge — for example if you run `analyze` incrementally but `index` fully.

---

## Why the pipelines are separate commands

| Concern | analyze | index |
|---|---|---|
| Work done | AST parsing, call-graph extraction | Line splitting, hash computation |
| Bound by | CPU (tree-sitter parsing) | I/O (file reads) |
| Incremental manifest | `source_files` | `indexed_files` |
| Query tools powered | `find_symbol`, `get_callers`, `get_callees`, `find_usages`, `get_file_symbols` | `search`, `locate_issue_impact_area` |

Keeping them separate means you can re-index (after content edits) without re-analyzing (if the symbol structure hasn't changed), or vice versa. They also fail independently — a parser crash in `analyze` doesn't touch the chunk index.

---

## Query layer

All query tools read from the tables populated by the pipelines above.

```
find_symbol / get_callers / get_callees / find_usages / get_file_symbols
  └─ reads: symbols, edges

search / locate_issue_impact_area
  └─ reads: chunks_fts  (FTS5/BM25 ranking, fallback to LIKE)

index_status
  └─ reads: indexed_files  (chunk count and readiness)
```

The FTS5 `SearchLexical` method tries the raw query first, escapes special characters and retries on syntax error, then falls back to `LIKE` as a last resort.

---

## MCP server

The MCP server wraps the same store and pipeline code as the CLI. `analyze_codebase` and `index_codebase` tools run the same `runFull` / `runIncremental` logic. Write operations go through `mcpConnHandle.WriteContext` to serialize concurrent writes against the single SQLite connection; read operations use `mcpConnHandle.ReadContext` and can proceed concurrently.

---

## Table ownership summary

| Table | Written by | Read by |
|---|---|---|
| `codebases` | `codebase register`, `index --path` (auto-register) | all commands |
| `source_files` | `analyze` | `analyze --incremental` (manifest) |
| `symbols` | `analyze` | graph navigation tools |
| `edges` | `analyze` | `get_callers`, `get_callees`, `find_usages` |
| `chunks` | `index` | trigger → `chunks_fts`; `search` |
| `chunks_fts` | INSERT/DELETE/UPDATE triggers on `chunks` | `search`, `locate_issue_impact_area` |
| `indexed_files` | `index` | `index --incremental` (manifest), `index_status` |
| `workspaces` / `workspace_members` | `workspace` commands | `analyze` (cross-repo links) |
| `memories` | — (schema present, no MCP tool exposed yet) | — |
| `metric_*` | pipeline runs | observability / `metrics` tool |
