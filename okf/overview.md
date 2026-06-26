# AgentDB — Project Overview

## What This Project Is

AgentDB is a local-first code intelligence platform that indexes source codebases into a SQLite database, enabling semantic search, symbol resolution, cross-reference navigation, and AI-assisted issue localization. It serves developers and AI agents (via the Model Context Protocol) who need fast, offline access to structural code knowledge — symbols, call graphs, imports, and documentation — across one or many repositories. The core value is turning raw source into a queryable knowledge graph with blast-radius analysis, incremental re-indexing, and portable artifact export/import — all without leaving the local machine.

## Architecture

AgentDB follows a layered architecture with clear separation between CLI orchestration, domain logic, storage, and retrieval:

```
─────────────────────────────────────────────────────┐
│  CLI Layer (cmd/)                                    │
│  Cobra commands: bootstrap, analyze, index, export, │
│  import, mcp server, locate-issue, workspace, watch  │
└──────────────┬──────────────────────────────────────┘
               │
──────────────▼──────────────────────────────────────┐
│  Domain / Service Layer                              │
│  ┌──────────┐ ┌──────────┐ ┌───────── ┌─────────┐ │
│  │  parse   │ │  chunk   │ │ search  │ │ orient  │ │
│  │ symbols, │ │  AST/text│ │  FTS5,  │ │ classify│ │
│  │ edges,   │ │  fallback│ │ blast   │ │ docs,   │ │
│  │ imports  │ │  chain   │ │ radius  │ │ retrieve│ │
│  └──────────┘ └────────── └─────────┘ └─────────┘ │
│  ┌──────────┐ ┌──────────┐ ──────────────────────┐│
│  │  index   │ │ artifact │ │  store (repos)       ││
│  │incremental│ │ export/ │ │  chunks, symbols,    ││
│  │ delta    │ │ import   │ │  edges, cross-repo   ││
│  └──────────┘ └──────────┘ └──────────────────────┘│
└──────────────┬──────────────────────────────────────
               │
┌──────────────▼──────────────────────────────────────┐
│  Data Layer                                          │
│  ┌──────────┐ ┌──────────┐ ┌───────── ┌─────────┐ │
│  │    db    │ │  config  │ │ observe │ │  gen    │ │
│  │  SQLite  │ │  (XDG/   │ │ logger, │ │  sqlc   │ │
│  │  WAL,    │ │  TOML)   │ │ metrics │ │  queries│ │
│  │  conn    │ │          │ │         │ │         │ │
│  │  handle  │ │          │ │         │ │         │ │
│  └──────────┘ └────────── └─────────┘ └─────────┘ │
└─────────────────────────────────────────────────────┘
```

**Control flow**: The `cmd` package is the entry point. Every command resolves configuration via `internal/config` (env → file → default), opens a database connection via `internal/db`, and delegates to domain packages. Analysis flows `parse → store → chunk → index`. Retrieval flows `search → blast-radius → orient`. The MCP server exposes these capabilities as JSON-RPC tools over stdio.

**Key boundaries**:
- **Build tags**: Tree-sitter parsers live behind `//go:build treesitter`; the default build is pure Go with `go/ast` only.
- **Single-connection write serialization**: `ConnectionHandle` uses a channel-based semaphore so all writes are serialized with timeout — the application assumes exclusive SQLite access.
- **FTS5 is optional**: The system gracefully degrades to LIKE-based search if the SQLite build lacks FTS5.
- **Plugin extensibility**: External binary parsers can be loaded at runtime and take priority over built-ins for the same language.

## Module Map

| Module | Purpose |
|--------|---------|
| `cmd/` | Cobra CLI commands — the user-facing entry point for all operations |
| `internal/parse/` | Extracts symbols, edges, imports from source via `go/ast` and tree-sitter; supports external parser plugins |
| `internal/chunk/` | Splits source into indexable units — fixed-line, AST-aware (tree-sitter), and token-boundary (BPE) chunking |
| `internal/index/` | Incremental indexing engine — computes file deltas, handles hash migration, verifies chunk integrity |
| `internal/store/` | Repository layer — typed CRUD for chunks, symbols, edges, codebases, memories, cross-repo queries |
| `internal/search/` | Full-text search (FTS5 + LIKE fallback), blast-radius computation, issue-to-symbol localization |
| `internal/orient/` | Classifies and retrieves documentation files (READMEs, design docs, agent instructions) by type and priority |
| `internal/artifact/` | Exports/imports codebase analysis as portable SQLite files using `ATTACH DATABASE` bulk copy |
| `internal/db/` | SQLite lifecycle — connection pooling, WAL mode, schema bootstrap, migrations, write serialization |
| `internal/config/` | Configuration resolution from XDG-style files and `AGENTDB_*` environment variables |
| `internal/filefilter/` | Path filtering — built-in blocklist plus `.gitignore` support for traversal |
| `internal/observe/` | Structured JSON logging and in-memory metrics collection (latency, throughput, parser health) |
| `data/gen/` | sqlc-generated typed database access layer — auto-generated, not hand-edited |

## Getting Started

1. **Clone the repository** and ensure Go is installed.
2. **Build** (pure-Go mode, Go-only parser):
   ```bash
   go build -o agentdb ./cmd/agentdb
   ```
   For tree-sitter support (Python, TypeScript, Rust, JS):
   ```bash
   go build -tags treesitter -o agentdb ./cmd/agentdb
   ```
3. **Bootstrap** the database and register a codebase:
   ```bash
   agentdb bootstrap
   agentdb codebase register --path /path/to/project
   ```
4. **Analyze** to extract symbols and edges, then **index** for search:
   ```bash
   agentdb analyze
   agentdb index
   ```
5. **Search** via the MCP server or use `locate-issue` for natural-language impact analysis:
   ```bash
   agentdb mcp
   agentdb locate-issue "authentication timeout on login"
   ```

## Key Design Decisions

- **SQLite as the sole storage engine**: Everything — symbols, edges, chunks, FTS5 indices, metadata — lives in a single SQLite database. This makes the entire system a file you can copy, inspect with any SQLite tool, and back up trivially. The tradeoff is write serialization: a single connection with a channel-based semaphore enforces exclusive writes with bounded timeout.

- **ATTACH DATABASE for export/import**: Rather than serializing rows over the wire, artifacts are built by attaching a second SQLite file and running cross-database `INSERT … SELECT`. This is extremely fast but requires both databases to be accessible from the same process — it cannot work across a network boundary.

- **Incremental indexing with content hashing**: Files are keyed by SHA-256 hash. Only changed, added, or removed files are re-processed on subsequent runs, making re-indexing of large codebases practical. Legacy MD5 hashes are detected and force re-indexing to SHA-256.

- **Two-stage classification for documentation**: SQL queries are intentionally broad (always prepending `%` to LIKE patterns), with precise filtering happening in the application layer. This keeps the database query simple but means callers must not assume returned rows are all relevant.

- **Plugin priority over built-ins**: When an external parser plugin and a built-in both declare the same language, the plugin wins unconditionally. This assumes plugins are more capable — a design choice that simplifies registry logic but means a buggy plugin silently replaces a working built-in.

- **MCP protocol version mirroring**: The server echoes the client's requested protocol version rather than enforcing a default, maximizing interop across diverse MCP client implementations.

- **Graceful degradation everywhere**: FTS5 unavailable → fall back to LIKE search. Tree-sitter parser fails → fall back to fixed-line chunking. Config file is malformed → use defaults with a warning. The system is designed to always produce *some* result rather than fail hard.