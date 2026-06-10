# agentdb

A CLI and MCP server that pre-indexes codebases into SQLite — symbols, call graphs, imports, and file metadata — so AI agents can query project structure without parsing source at runtime.

## Why

Two situations where this matters:

1. **Source isn't distributed.** Binary releases, proprietary libraries, vendored dependencies — the consumer has no `.go` or `.ts` files to read. An agentdb artifact gives agents the structural data anyway.

2. **Large codebases are expensive to parse on every session.** Pre-indexing once in CI and distributing the result avoids repeated AST extraction on 100k+ LOC projects.

If agents can grep it in under 10 tool calls, this is overhead. For everything else — large codebases, distributed binaries, or repeated cross-package traversal — pre-indexing pays off.

## Install

```bash
go install github.com/roysland/agentdb@latest
```

Requires Go 1.24+.

Configuration is read from ~/.config/agentdb/config.toml (or $XDG_CONFIG_HOME/agentdb/config.toml) with precedence:
flags > environment variables > config.toml > built-in defaults.

Naming convention:
- Flags use kebab-case: `--embed-base-url`
- Environment variables use uppercase snake case with `AGENTDB_` prefix: `AGENTDB_EMBED_BASE_URL`
- `config.toml` uses the exact same `AGENTDB_*` keys as environment variables.

Canonical names are `AGENTDB_*` in both environment and config.

Example config.toml:

```toml
AGENTDB_DB_URL = "~/.local/share/agentdb/agentdb.db"
AGENTDB_DB_DRIVER = "auto"
AGENTDB_PROJECT_PATH = "~/Projects/my-repo"

AGENTDB_EMBED_PROVIDER = "ollama"
AGENTDB_EMBED_BASE_URL = "http://localhost:11434/v1"
AGENTDB_EMBED_MODEL = "nomic-embed-text"
AGENTDB_EMBED_TIMEOUT_SECONDS = "30"
AGENTDB_LINES_PER_CHUNK = "20"
```

## Security and Privacy

agentdb is local-first by default (local SQLite DB, stdio MCP server, local Ollama embedding endpoint), with explicit controls for stricter environments.

- `AGENTDB_EMBED_LOCAL_ONLY=1` hard-fails startup when embeddings point to a non-localhost endpoint.
- `AGENTDB_PLUGIN_SAFE_MODE=1` disables all external parser plugin subprocess execution.
- `AGENTDB_PLUGIN_ALLOWLIST=name1,name2` restricts plugin loading to approved plugin names.
- File traversal for indexing/analyzing/chunking only processes regular files confined to the target root; symlinks resolving outside the root are skipped.
- MCP request parameters are only logged at `AGENTDB_LOG_LEVEL=debug` to reduce accidental sensitive text exposure in stderr collectors.

Recommended hardened setup:

```bash
export AGENTDB_EMBED_PROVIDER=disabled
export AGENTDB_EMBED_LOCAL_ONLY=1
export AGENTDB_PLUGIN_SAFE_MODE=1
export AGENTDB_LOG_LEVEL=info
```

## Quick Start

```bash
# Register and index a project
agentdb codebase register --path . --name my-project
agentdb index --codebase-id 1
agentdb analyze --codebase-id 1

# Start MCP server for agent access
agentdb mcp
```

## Commands

| Command | Description |
|---------|-------------|
| `bootstrap` | Apply schema and ensure tables exist |
| `codebase register` | Register a codebase root path |
| `codebase list` | List registered codebases |
| `index` | Chunk and index source files (supports `--incremental`) |
| `analyze` | Extract symbols, call graphs, and relationships (supports `--incremental`) |
| `watch` | Watch a codebase for file changes and trigger incremental re-indexing |
| `locate-issue` | Locate likely impact area for a natural-language issue report |
| `export` | Export a codebase to a portable `.agentdb` artifact |
| `import` | Import a `.agentdb` artifact into the local database |
| `workspace create/add/remove/list` | Manage cross-repository workspaces |
| `mcp` | Run MCP stdio server |
| `version` | Print version |

## MCP Server

`agentdb mcp` exposes all capabilities over JSON-RPC stdio (MCP protocol). Tools available:

During MCP initialization, agentdb also publishes a server description that frames proprietary-artifact usage: indexed data is for navigation/development assistance, and reconstructing or reproducing source implementation from search/symbol results is prohibited.

### Search & Retrieval
- `search` — Ranked retrieval across memories and/or code chunks (lexical via FTS5, vector, or hybrid)
- `semantic_search` — Natural language symbol lookup via vector similarity with optional blast radius enrichment

### Codebase Management
- `register_codebase` / `list_codebases` — Codebase management
- `index_codebase` / `index_status` — Indexing (supports incremental mode)
- `analyze_codebase` — Symbol and relationship extraction (supports incremental mode)
- `codebase_context` — Retrieve README/design/agent guidance docs for session bootstrapping; falls back to `project_overview` when no docs are indexed

### Symbol & Graph Queries
- `find_symbol` — Look up functions, types, methods by name (supports workspace-scoped queries)
- `find_usages` — Find all references to a symbol (supports workspace-scoped queries)
- `get_file_symbols` — List symbols defined in a file
- `get_callers` / `get_callees` — Call graph traversal (supports workspace-scoped queries)
- `get_imports` — List imports for a file
- `project_overview` — High-level codebase summary (languages, LOC, packages, top files)
- `locate_issue_impact_area` — Triage a natural-language issue description into ranked impact candidates using hybrid search and blast radius

### Memory & Observability
- `server_stats` — Runtime metrics: uptime, per-tool call counts, avg/p95 latency, error rates


### MCP examples

```json
{
  "name": "search",
  "arguments": {
    "query": "incremental analyze",
    "source": "chunks",
    "mode": "lexical",
    "codebase_id": 1,
    "limit": 10
  }
}
```

### CLI examples

```bash
agentdb search \
  --query "incremental analyze" \
  --mode lexical \
  --codebase-id 1
```

## Filesystem Watching

`agentdb watch` monitors a codebase directory for file changes and triggers incremental re-indexing automatically — no CI step required for local development.

```bash
# Watch and re-index only (chunk index, no symbol extraction)
agentdb watch --codebase-id 1 --codebase-path .

# Watch and re-index + re-analyze (symbols, call graph)
agentdb watch --codebase-id 1 --codebase-path . --analyze

# Custom debounce window (default: 500ms)
agentdb watch --codebase-id 1 --codebase-path . --debounce 1000
```

SIGINT/SIGTERM waits for any in-progress re-index to complete before exiting.

## Incremental Indexing

Both `index` and `analyze` support an `--incremental` flag that detects changed files via SHA-256 content hashing and only re-processes what changed. On a 150k LOC codebase with a few file edits, incremental runs complete in seconds instead of minutes.

File discovery for indexing/analyzing also honors `.gitignore` files (root and nested directories), in addition to built-in skip rules like `.git`, `node_modules`, and `vendor`, so ignored files are skipped by default.

```bash
# Full index (first run or when you want a clean slate)
agentdb index --codebase-id 1
agentdb analyze --codebase-id 1

# Incremental (subsequent runs — only processes changed files)
agentdb index --incremental --codebase-id 1
agentdb analyze --incremental --codebase-id 1
```

Both commands output performance metrics: files processed, files skipped, duration, and throughput.

## Cross-Repository Workspaces

Group multiple codebases into a workspace for cross-repo symbol resolution and call graph traversal.

```bash
# Create a workspace and add codebases
agentdb workspace create --name platform
agentdb workspace add --workspace platform --codebase-id 1
agentdb workspace add --workspace platform --codebase-id 2

# MCP tools accept workspace_id to query across all members
# find_symbol, get_callers, find_usages all support workspace-scoped queries
```

When analyzing codebases in a workspace, unresolved imports that match symbols in sibling codebases are automatically linked as cross-repository edges.

## Semantic Search

The `semantic_search` MCP tool maps natural language queries to candidate symbols via vector similarity. Useful for bug triage from vague descriptions.

```
Tool: semantic_search
Input: {"query": "config parsing fails for special characters", "codebase_id": 1, "include_blast_radius": true}
```

Returns ranked symbols with similarity scores, plus optional blast radius (callers, callees, dependents) showing what might break.

For offline use with artifacts, export with `--include-embeddings` to preserve vectors, then supply a pre-computed `query_embedding` parameter at query time — no live embedding API needed.

## Artifact Export/Import

The core CI workflow: analyze once, distribute the result.

```bash
# In CI: index, analyze, export
agentdb codebase register --path . --name my-lib
agentdb index --codebase-id 1
agentdb analyze --codebase-id 1
agentdb export --codebase-id 1 --output my-lib.agentdb

# With embeddings for offline semantic search
agentdb export --codebase-id 1 --output my-lib.agentdb --include-embeddings

# For proprietary distribution: strip source-bearing text while keeping graph metadata
agentdb export --codebase-id 1 --output my-lib.agentdb --strip-source

# On consumer machine: import and query
agentdb import my-lib.agentdb
agentdb mcp
```

The artifact is a standard SQLite file containing symbols, edges, source file metadata, chunks, and indexed file records for a single codebase. By default it excludes memories, tasks, and embedding vectors (embeddings are environment-specific and can be regenerated locally). Use `--include-embeddings` to preserve vectors for offline semantic search.

Use `--strip-source` to remove source-bearing text from exported data (`chunks.snippet`, `symbols.doc_comment`, `symbols.body_snippet`, and `symbols.signature`) while preserving the symbol graph and structural metadata.

### Import behavior

- Upserts by matching on `root_path` — re-importing replaces existing data
- `--name` flag overrides the codebase name from the artifact
- Schema version is validated before import
- Embedding metadata (`has_embeddings`, `embedding_model`, `embedding_dimensions`) is preserved

### GitHub Actions example

```yaml
name: Generate AgentDB Artifact
on:
  release:
    types: [published]

jobs:
  build-artifact:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'

      - name: Install agentdb
        run: go install github.com/roysland/agentdb@latest

      - name: Index and export
        run: |
          CODEBASE_ID=$(agentdb codebase register --path . --name "${{ github.event.repository.name }}" | jq -r '.id')
          agentdb index --codebase-id "$CODEBASE_ID"
          agentdb analyze --codebase-id "$CODEBASE_ID"
          agentdb export --codebase-id "$CODEBASE_ID" --output "${{ github.event.repository.name }}.agentdb"

      - name: Attach to release
        uses: softprops/action-gh-release@v2
        with:
          files: "${{ github.event.repository.name }}.agentdb"
```

## Pluggable Parser Architecture

Language support is extensible via external parser plugins. Plugins are standalone executables that communicate with agentdb over JSON-RPC (stdin/stdout), so they can be written in any language.

```
~/.agentdb/plugins/
└── csharp-parser/
    ├── manifest.json
    └── bin/csharp-parser
```

**manifest.json:**
```json
{
  "name": "csharp-parser",
  "version": "1.0.0",
  "languages": ["csharp"],
  "binary": "./bin/csharp-parser"
}
```

Plugins are discovered from `~/.agentdb/plugins/` (default) or a directory specified by `AGENTDB_PLUGIN_DIR`. When a plugin declares a language already handled by a built-in parser, the plugin takes priority.

The plugin protocol supports three methods: `capabilities` (handshake), `parse` (extract symbols/edges), and `shutdown` (graceful termination). Subprocesses are reused across files of the same language within a session.

## Language Support

The default binary (pure Go, no CGo) supports **Go only** via `go/ast`.

For additional languages, build with the `treesitter` tag:

```bash
go install -tags treesitter github.com/roysland/agentdb@latest
```

This enables tree-sitter parsers for:
- Python
- TypeScript / TSX / JavaScript
- Rust

The treesitter build requires CGo and a C compiler. The default `go install` produces a Go-only binary.

### Resilient Parsing

The tree-sitter parsing layer includes graceful degradation:

- **Error threshold** — If a file has >15% ERROR nodes in its AST, symbol extraction is aborted and the file falls back to text-based chunking. This prevents corrupted symbol graphs from poisoning search results.
- **Merge conflict detection** — Files with merge conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`) are marked as `partial` and skipped for AST extraction.
- **Panic recovery** — Tree-sitter crashes are caught and don't bring down the MCP server.
- **Health reporting** — The parser reports which grammars loaded successfully at startup.

MCP tool responses annotate results from degraded files so agents know when data confidence is reduced.

## AST-Aware Chunking

Code is chunked at semantic boundaries (functions, classes, methods, modules) rather than fixed line counts. This means retrieved snippets contain complete logical units.

- Chunks carry `kind`, `name`, and `signature` metadata extracted from the AST
- Large nodes (>100 lines) are subdivided at nested block boundaries
- Non-code content (markdown, prose) uses BPE token-count windowing with paragraph-boundary preference
- Concatenating all chunks reproduces the original file (round-trip property)

## FTS5 Search

Chunk search uses SQLite FTS5 for sub-linear lexical matching with BM25 ranking. The FTS5 index is kept synchronized with the chunks table via triggers.

- **Lexical mode** — FTS5 MATCH with BM25 scoring
- **Hybrid mode** — FTS5 candidates re-ranked by cosine similarity against embedding vectors
- **Fallback** — If FTS5 is unavailable, falls back to in-memory scan (with a warning)

New chunks are marked with `pending_embedding` status until the async embedding pipeline processes them. Hybrid search results flag chunks where vector re-ranking wasn't applied.

## Observability

The MCP server emits structured JSON logs to stderr and tracks per-tool metrics in memory.

### Structured Logging

```json
{"timestamp":"2024-01-15T10:30:00.123Z","level":"info","operation":"find_symbol","duration_ms":12,"status":"ok"}
```

Configure log level via `AGENTDB_LOG_LEVEL` (debug, info, warn, error). At debug level, request parameters and response sizes are included.

### Metrics

The `server_stats` MCP tool returns runtime metrics:
- Uptime, total requests, active codebase count
- Per-tool breakdown: call count, average latency, p95 latency, error count
- P95 computed over a sliding window of the last 1000 calls
- Resettable via `reset: true` parameter

## Connection Architecture

The MCP server uses a single persistent SQLite connection (`SetMaxOpenConns(1)`) with application-layer write serialization via `sync.Mutex`. This eliminates WAL contention and `SQLITE_BUSY` errors structurally.

- All operations have strict context deadlines (3s writes, 5s reads)
- Mutex acquisition timeout prevents indefinite blocking
- Health-check with automatic reconnection
- `PRAGMA journal_mode=WAL` and `PRAGMA busy_timeout=3000` as defense-in-depth
- `PRAGMA auto_vacuum=INCREMENTAL` for non-blocking storage reclamation

## SHA-256 File Hashing

File change detection uses SHA-256.

- Streaming hash for files >10MB (no memory spikes)
- Post-migration incremental vacuum reclaims freed pages
- Integrity verification ensures no orphaned chunks after migration

## Embeddings (optional)

Vector search is optional. Configure a provider to enable it:

```bash
export AGENTDB_EMBED_PROVIDER=openai
export AGENTDB_EMBED_BASE_URL=https://api.openai.com/v1
export AGENTDB_EMBED_API_KEY=sk-...
export AGENTDB_EMBED_MODEL=text-embedding-3-small
```

When unavailable, all search falls back to lexical mode. Artifacts exclude embeddings by default — use `--include-embeddings` to preserve them for offline semantic search.

### Local Embeddings with Ollama

You can run embeddings entirely locally using [Ollama](https://ollama.com), removing the need for an external API key or network access.

#### Setup

1. Install Ollama from https://ollama.com and start the service.

2. Pull an embedding model:

```bash
ollama pull nomic-embed-text
```

3. Configure environment variables:

```bash
export AGENTDB_EMBED_BASE_URL=http://localhost:11434/v1
export AGENTDB_EMBED_PROVIDER=ollama
export AGENTDB_EMBED_MODEL=nomic-embed-text
export AGENTDB_EMBED_API_KEY=
```

The API key can be left empty or unset — agentdb skips authentication for local endpoints.

#### Example

```bash
# Start Ollama (if not already running)
ollama serve &

# Pull the embedding model
ollama pull nomic-embed-text

# Configure agentdb for local embeddings
export AGENTDB_EMBED_BASE_URL=http://localhost:11434/v1
export AGENTDB_EMBED_PROVIDER=ollama
export AGENTDB_EMBED_MODEL=nomic-embed-text
export AGENTDB_EMBED_API_KEY=

# Register, index, and use semantic search
agentdb codebase register --path . --name my-project
agentdb index --codebase-id 1
agentdb analyze --codebase-id 1
agentdb mcp
```

Semantic search and hybrid mode now work without any external API dependency.

#### Troubleshooting

**Ollama not running**

If you see connection errors, make sure the Ollama service is running:

```bash
ollama serve
```

Or check its status with `ollama list`. On macOS/Linux, Ollama may run as a background service — verify with `ps aux | grep ollama`.

**Model not pulled**

If embedding requests fail with a model-not-found error, pull the model first:

```bash
ollama pull nomic-embed-text
```

Verify it's available with `ollama list` — you should see `nomic-embed-text` in the output.

**Connection refused**

If agentdb reports "connection refused" when calling the embedding endpoint:

- Confirm Ollama is listening on port 11434: `curl http://localhost:11434/v1/models`
- Check that `AGENTDB_EMBED_BASE_URL` is set to `http://localhost:11434/v1` (not https, and include the `/v1` path)
- If Ollama is bound to a different host/port, adjust the URL accordingly

## Global Flags

```
--db-url            Database file path (env: AGENTDB_DB_URL)
--db-driver         Driver mode: auto|sqlite3 (env: AGENTDB_DB_DRIVER)
--project-path      Default project path (env: AGENTDB_PROJECT_PATH)
--embed-provider    Embedding provider: disabled|openai|ollama (env: AGENTDB_EMBED_PROVIDER)
--embed-base-url    Embedding API base URL (env: AGENTDB_EMBED_BASE_URL)
--embed-api-key     Embedding API key (env: AGENTDB_EMBED_API_KEY)
--embed-model       Embedding model name (env: AGENTDB_EMBED_MODEL)
--embed-timeout-seconds Embedding timeout seconds (env: AGENTDB_EMBED_TIMEOUT_SECONDS)
```

## Schema

SQLite database with these tables:

| Table | Purpose |
|-------|---------|
| `meta` | Schema version and artifact metadata |
| `codebases` | Registered project roots |
| `symbols` | AST-extracted symbols (functions, types, methods, consts) |
| `edges` | Directed relationships (imports, calls, type usage, cross-repo links) |
| `source_files` | File-level metadata (language, LOC, package) |
| `chunks` | Semantic code chunks with optional embeddings and FTS5 index |
| `chunks_fts` | FTS5 virtual table for sub-linear lexical search |
| `indexed_files` | File indexing state with parse status tracking |
| `memories` | Agent long-term memory |
| `workspaces` | Logical groupings of codebases |
| `workspace_members` | Workspace-to-codebase membership |


# Build locally
```bash
CGO_ENABLED=1 go build -tags "treesitter sqlite_fts5" -o ~/.local/bin/agentdb .
```
## License

MIT
