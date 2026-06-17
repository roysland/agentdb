# What this tool can do

## Core Indexing & Analysis

- **Codebase registration** — Register project root paths for tracking and indexing
- **AST-aware chunking** — Splits source files at semantic boundaries (functions, classes, methods) using tree-sitter, with kind/name/signature metadata per chunk
- **Symbol extraction** — Extracts functions, types, methods, constants, and their relationships from source code
- **Call graph construction** — Builds directed edges for imports, function calls, and type usage
- **Incremental indexing** — Detects changed files via SHA-256 hashing and re-processes only what changed
- **Incremental analysis** — Re-extracts symbols/edges only for modified files, preserving data for unchanged files
- **Multi-language support** — Go (built-in), Python/TypeScript/JavaScript/Rust (via `agentdb-parsers` plugin), extensible via additional plugins

## Search

- **FTS5 lexical search** — Full-text search over code chunks using SQLite FTS5 with BM25 ranking
- **Hybrid search** — FTS5 candidate retrieval re-ranked by cosine similarity against embedding vectors
- **Semantic search** — Natural language queries mapped to candidate symbols via vector similarity
- **Blast radius analysis** — For any symbol, shows callers, callees, and file-level dependents
- **Memory search** — Lexical and vector search across agent long-term memories
- **Scoped memory search** — Optional `workspace_id` and `codebase_id` filters keep memory retrieval bound to the right project/workspace

## Cross-Repository

- **Workspaces** — Group multiple codebases for unified querying
- **Cross-repo symbol resolution** — `find_symbol`, `get_callers`, `find_usages` query across all workspace members
- **Cross-repo link creation** — Unresolved imports matching symbols in sibling codebases are automatically linked

## Artifact Export/Import

- **Portable artifacts** — Export a codebase to a standalone `.agentdb` SQLite file for distribution
- **CI-friendly** — Analyze once in CI, attach artifact to releases, consumers import and query offline
- **Conditional embedding export** — Optionally preserve embedding vectors for offline semantic search
- **Idempotent import** — Re-importing replaces existing data cleanly (upsert by root_path)
- **Schema version validation** — Artifacts carry version metadata; incompatible versions are rejected

## Pluggable Parser Architecture

- **External plugins** — Language parsers as standalone executables communicating via JSON-RPC over stdin/stdout
- **Any-language plugins** — Plugin authors can use any language (Go, C#, Java, etc.)
- **Auto-discovery** — Plugins discovered from `~/.agentdb/plugins/` or `AGENTDB_PLUGIN_DIR`
- **Priority override** — Plugins take precedence over built-in parsers for the same language
- **Subprocess reuse** — Single subprocess per language per session for efficiency
- **Graceful failure** — Non-executable or crashing plugins are skipped with a warning

## Resilient Parsing

- **Error threshold** — Files with >15% AST ERROR nodes abort symbol extraction and fall back to text chunking
- **Merge conflict detection** — Files with conflict markers are flagged and skipped for AST parsing
- **Panic recovery** — Tree-sitter crashes are caught; the MCP server stays up
- **Degradation metadata** — MCP responses annotate results from files with reduced parse confidence
- **Grammar health reporting** — Reports which language grammars loaded successfully at startup

## Observability

- **Structured JSON logging** — Every MCP tool call logged with timestamp, operation, duration, status
- **Configurable log level** — `AGENTDB_LOG_LEVEL` env var (debug/info/warn/error)
- **Per-tool metrics** — Call counts, average latency, p95 latency, error counts
- **P95 sliding window** — Computed over the last 1000 calls per tool
- **`server_stats` MCP tool** — Query runtime metrics on demand, with optional reset
- **Indexing performance metrics** — Both `index` and `analyze` report throughput and file counts

## Connection & Performance

- **Single persistent connection** — One SQLite connection for the MCP server lifetime (no per-call open/close)
- **Write serialization** — Application-layer mutex prevents SQLITE_BUSY errors
- **Strict timeouts** — All operations have context deadlines (3s writes, 5s reads); no indefinite blocking
- **Health-check reconnection** — Automatic recovery if the connection becomes unhealthy
- **Incremental auto-vacuum** — Non-blocking page reclamation via `PRAGMA auto_vacuum=INCREMENTAL`
- **SHA-256 hashing** — File change detection with streaming for large files
- **Legacy hash migration** — MD5 hashes upgraded to SHA-256 on next incremental run

## Text Fallback Chunking

- **Markdown splitting** — Splits at header boundaries to preserve logical sections
- **BPE token windowing** — Prose content chunked by exact token count (not character approximation)
- **Paragraph-boundary preference** — Splits at paragraph breaks within the token window when possible
- **Configurable window** — Default 500 tokens with 100-token overlap between chunks

## Filesystem Watching

- **Live source watching** — `agentdb watch` monitors a codebase directory for file changes and triggers incremental re-indexing automatically
- **Debounce window** — Configurable debounce (default 500 ms) batches rapid edits into a single re-index pass
- **Optional re-analysis** — `--analyze` flag also re-extracts symbols and edges after each re-index
- **Graceful shutdown** — SIGINT/SIGTERM waits for any in-progress re-index to complete before exiting

## Capability Comparison

- **`compare_capabilities` MCP tool** — Compares symbol coverage between two codebases by file-path domain; returns implemented/partial/missing/extra groups. Only exposes symbol name, kind, and domain — no signatures.

## Issue Triage

- **`locate-issue` CLI command** — `agentdb locate-issue --issue-text "..."` ranks likely impact areas for a natural-language bug or feature description
- **`locate_issue_impact_area` MCP tool** — Same functionality over MCP, with optional `codebase_id` / `workspace_id` scope
- **Hybrid scoring** — Combines FTS5 lexical match with optional vector re-ranking; falls back to lexical-only when no embedding provider is configured
- **Blast radius enrichment** — Returned candidates include caller/callee context to show propagation risk

## Session Bootstrapping

- **`codebase_context` MCP tool** — Retrieves README, design docs, and agent guidance files for a codebase at session start; falls back to `project_overview` when no orientation docs are indexed
- **Configurable doc patterns** — Honors an `.agentdb/orient.toml` config file in the codebase root to customise which files are treated as orientation documents

## MCP Server

- **JSON-RPC stdio protocol** — Standard MCP interface for AI agent integration
- **18 tools** — Search, symbol lookup, call graph traversal, codebase management, capability comparison, observability
- **Workspace-scoped queries** — Optional `workspace_id` parameter fans out across multiple codebases
- **Embedding pipeline** — Async embedding computation with status tracking and retry on failure

---

# What this tool does not do
- **Automatic embedding generation** — Requires a configured embedding provider (OpenAI-compatible API); does not bundle a local model

- **Code generation** — Indexes and queries structure; does not write or modify source code
- **Build/compile** — Does not invoke compilers or build systems
- **Runtime analysis** — Static analysis only; no profiling, tracing, or dynamic call graph construction
- **Access control** — No authentication or authorization; all data in the SQLite file is accessible to anyone with file access
- **Distributed storage** — Single SQLite file per database; no clustering or replication (use Turso driver for remote access)
- **Full WCAG compliance testing** — Not applicable (CLI/MCP tool, no UI)
- **Dependency vulnerability scanning** — Knows import relationships but does not assess security posture
- **Natural language code explanation** — Provides structural data for agents to reason about; does not generate explanations itself
