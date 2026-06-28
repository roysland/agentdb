# AgentDB Project Overview

## What this project is  
AgentDB is a codebase indexing and analysis platform that extracts semantic structure (symbols, imports, call edges) from source code, chunks files for retrieval-augmented generation (RAG), and enables natural-language issue localization across multi-repo workspaces. It is used by developers and internal tooling to power intelligent code navigation, impact analysis, and context-aware LLM interactions. Its core value lies in transforming unstructured repositories into a structured, queryable knowledge graph while supporting incremental updates, cross-repo symbol resolution, and artifact-based portability.

## Architecture  
The system follows a layered architecture:  
- **CLI layer** (`cmd`) exposes commands and an MCP server, orchestrating high-level workflows (bootstrap, analyze, index, import/export).  
- **Storage layer** (`internal/db`, `data/gen`) manages SQLite with WAL mode, schema migrations, and a typed query interface.  
- **Analysis layer** (`internal/parse`, `internal/chunk`, `internal/index`) parses source code, chunks it for indexing, and maintains incremental deltas via file hashing.  
- **Search & retrieval layer** (`internal/search`, `internal/orient`) performs lexical and semantic search, classifies documentation, and computes blast radius for issue localization.  
- **Configuration & observability** (`internal/config`, `internal/observe`) handle runtime settings and structured logging/metrics.  
- **Artifact layer** (`internal/artifact`) enables export/import of analysis data as self-contained SQLite files.  
Control flows from CLI commands → config resolution → database access → parsing/chunking → indexing/search. Data flows from source files → AST/parse trees → symbols/edges → chunks → FTS5 index, with bidirectional sync between disk and database.

## Module map  
- `.claude/worktrees/.../cmd` — CLI command layer (cobra + MCP server)  
- `.claude/worktrees/.../data/gen` — sqlc-generated database access layer  
- `.claude/worktrees/.../internal/artifact` — export/import of analysis data as SQLite files  
- `.claude/worktrees/.../internal/chunk` — file chunking strategies (AST-aware, fixed-line, token-boundary)  
- `.claude/worktrees/.../internal/config` — configuration resolution (TOML + env vars)  
- `.claude/worktrees/.../internal/db` — SQLite lifecycle, connection pooling, write serialization  
- `.claude/worktrees/.../internal/filefilter` — path filtering (built-in + `.gitignore` rules)  
- `.claude/worktrees/.../internal/index` — incremental indexing via file hash comparison  
- `.claude/worktrees/.../internal/observe` — structured logging and metrics collection  
- `.claude/worktrees/.../internal/orient` — documentation classification and retrieval  
- `.claude/worktrees/.../internal/parse` — source code parsing (Go native, tree-sitter optional, plugin extensible)  
- `.claude/worktrees/.../internal/search` — FTS5 search, issue localization, blast radius analysis  
- `.claude/worktrees/.../internal/store` — repository abstractions over database tables  

## Getting started  
1. Build the CLI: `go build ./cmd/agentdb` (requires Go 1.22+; tree-sitter support needs `-tags treesitter` and CGo).  
2. Initialize the database: `agentdb bootstrap --new` (creates a local SQLite database at `~/.agentdb/agentdb.sqlite`).  
3. Register and analyze a codebase:  
   ```bash
   agentdb codebase register --root /path/to/repo
   agentdb analyze --codebase-id <id>
   agentdb index --codebase-id <id>
   ```  
4. (Optional) Start the MCP server: `agentdb mcp` for LLM tool integration.

## Key design decisions  
- **Write serialization via channel semaphore** instead of `sync.Mutex` to enforce timeouts and prevent goroutine leaks (Bug 1.1 fix).  
- **AST chunker round-trip guarantee**: concatenated snippets reconstruct the original file byte-for-byte, even when gaps (whitespace/comments) are appended to adjacent chunks.  
- **FTS5 is optional and gracefully degraded**: if SQLite lacks FTS5, lexical search falls back to `LIKE` queries; search layer handles this transparently.  
- **Plugin-based extensibility**: external parsers are loaded as JSON-RPC subprocesses, prioritized over built-ins, and isolated with timeouts (30s) to prevent hangs.  
- **Artifact portability via `ATTACH DATABASE`**: export/import copies data cross-database without serialization, but strips source text when `--strip-source` is used (irreversible).  
- **Precedence chain: input > env (`AGENTDB_*`) > config file (`~/.config/agentdb/config.toml`) > defaults**, with tilde expansion applied only to config-file values.  
- **Cross-repo symbol resolution is workspace-scoped and self-excluding**: edges between codebases in the same workspace are resolved, but never back to the originating codebase.