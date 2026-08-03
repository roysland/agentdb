---
commit: cc77d6b1c1d919b608eb80f7d9faa82f9dba6bc0
description: 'Codebase knowledge for module: cmd'
files:
- cmd/analyze.go
- cmd/bootstrap.go
- cmd/codebase.go
- cmd/export.go
- cmd/import.go
- cmd/index.go
- cmd/locate_issue.go
- cmd/mcp.go
- cmd/mcp_analyzer_default.go
- cmd/mcp_chunker_default.go
- cmd/mcp_test.go
- cmd/memory.go
- cmd/project_path.go
- cmd/query.go
- cmd/root.go
- cmd/target_resolution.go
- cmd/version.go
- cmd/watch.go
- cmd/workspace.go
tags:
- module
timestamp: '2026-08-03'
title: cmd
type: Module
---

### What it does
The `cmd` module provides the CLI entry point and MCP (Model Context Protocol) server implementation for `agentdb`. It orchestrates codebase registration, indexing, symbol analysis, and natural-language issue localization by interfacing with the `internal/db`, `internal/store`, and `internal/search` modules.

### Public interface
The module exposes command-line interfaces via `cobra.Command` and a persistent MCP server via `runMCPServer`.

*   **`newBootstrapCmd(ctx context.Context) *cobra.Command`**: Initializes the database schema.
*   **`newAnalyzeCmd(ctx context.Context) *cobra.Command`**: Parses codebases into symbols and relationships.
*   **`newIndexCmd(ctx context.Context) *cobra.Command`**: Chunks codebases for retrieval-augmented generation.
*   **`newLocateIssueCmd(ctx context.Context) *cobra.Command`**: Performs impact analysis for issue reports.
*   **`runMCPServer(ctx context.Context, cfg config.Runtime, in io.Reader, out io.Writer) error`**: Starts the MCP stdio server loop.

### Key invariants
*   **Database Schema**: All operations require a bootstrapped database; `mcp.go` enforces this via `EnsureSchema` before accepting requests.
*   **Codebase Registration**: A codebase must be registered in the `catalog` (via `codebase register` or `register_codebase` tool) before it can be indexed or analyzed.
*   **JSON-RPC Compliance**: The MCP server must return valid JSON-RPC 2.0 responses, and notifications (requests without an ID) must not elicit a response.
*   **Resource Cleanup**: Database connections and plugin registries must be closed/shutdown via `defer` to prevent file descriptor leaks or zombie processes.

### Non-obvious decisions
*   **`modernc.org/sqlite` over `mattn/go-sqlite3`**: The project migrated to `modernc.org/sqlite` to likely avoid CGO dependencies, enabling cross-compilation and simpler deployment for an agent-based tool that may run in varied environments.
*   **`mcpConnHandle` as a global/singleton**: The MCP server uses a persistent `db.ConnectionHandle` rather than opening/closing connections per request. This is necessary because MCP servers are long-lived processes, and frequent re-opening of the database would introduce significant latency and potential locking contention.
*   **`wrapChunkErr` remediation hint**: The indexer explicitly checks for "UNIQUE constraint failed" errors to suggest a non-incremental re-run. This is a UX-driven approach to handle "stale" index states that occur when an indexing process is interrupted, which would otherwise be opaque to the user.
*   **Cross-repo link resolution**: The `analyze` command performs a secondary pass to resolve imports against other workspace members. This is done after the primary parse to allow for decoupled analysis of independent codebases that are later joined into a logical workspace.

### Unclear intent
*   **`mcp_chunker_default.go` and `mcp_analyzer_default.go`**: These files are present in the module list but were not provided in the source code snippet. Their specific role in the MCP toolset versus the standard `internal/parse` or `internal/chunk` logic is ambiguous.
*   **`mcp_test.go`**: The purpose of this file is unclear; it is unclear if it contains unit tests for the MCP server or if it serves as a mock/harness for testing MCP client interactions.
