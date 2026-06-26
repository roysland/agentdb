---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/cmd'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/analyze.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/bootstrap.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/codebase.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/export.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/import.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/index.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/locate_issue.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/mcp.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/mcp_analyzer_default.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/mcp_chunker_default.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/mcp_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/memory.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/project_path.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/root.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/target_resolution.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/version.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/watch.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/cmd/workspace.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/cmd
type: Module
---

## What it does

This module implements the CLI command layer for AgentDB using cobra. It provides subcommands for bootstrapping the database schema, managing registered codebases, parsing/analyzing source into symbols and edges, chunking files for retrieval indexing, importing/exporting artifacts, running an MCP (Model Context Protocol) stdio server, and supporting utilities like workspace management, issue location, and cross-repo symbol resolution.

## Public interface

- `newBootstrapCmd(ctx context.Context) *cobra.Command` — schema migration/creation
- `newCodebaseCmd(ctx context.Context) *cobra.Command` — `codebase register` / `codebase list`
- `newAnalyzeCmd(ctx context.Context) *cobra.Command` — full or incremental symbol/edge extraction
- `newIndexCmd(ctx context.Context) *cobra.Command` — full or incremental chunk indexing
- `newExportCmd(ctx context.Context) *cobra.Command` — export codebase to `.agentdb` artifact
- `newImportCmd(ctx context.Context) *cobra.Command` — import codebase from artifact
- `newMCPCmd(ctx context.Context) *cobra.Command` — MCP stdio server exposing tools (search, register_codebase, list_codebases, index_codebase, index_status, analyze_codebase, locate_issue, etc.)
- `newLocateIssueCmd(ctx context.Context) *cobra.Command` — natural-language issue impact area location
- `newWorkspaceCmd(ctx context.Context) *cobra.Command` — workspace management
- `newVersionCmd(ctx context.Context) *cobra.Command` — version output
- `newWatchCmd(ctx context.Context) *cobra.Command` — filesystem watch
- `newProjectPathCmd(ctx context.Context) *cobra.Command` — project path resolution
- `newMemoryCmd(ctx context.Context) *cobra.Command` — memory management
- `newTargetResolutionCmd(ctx context.Context) *cobra.Command` — target resolution
- `newMCPAnalyzerDefaultCmd(ctx context.Context) *cobra.Command` — default MCP analyzer
- `newMCPChunkerDefaultCmd(ctx context.Context) *cobra.Command` — default MCP chunker

Internal helpers:
- `resolveCodebaseTarget(ctx, repo, cfg, codebaseID, path, codebasePath, autoRegister bool) (int64, string, error)` — resolves or auto-registers a codebase
- `resolveScopedCodebaseIDs(ctx, conn, codebaseID, workspaceID int64) ([]int64, error)` — resolves codebase IDs from workspace scope
- `findParserForFile(filePath string, parsers []parse.Parser) parse.Parser` — selects parser by file extension
- `wrapChunkErr(err error, key string) error` — enriches duplicate-key errors with remediation hint

## Key invariants

- Every command opens a connection via `db.Open(ctx, resolved)` and defers `conn.Close()`.
- `analyze` and `index` both support `--incremental`: when no stored hashes exist, they fall back to full runs automatically.
- Incremental analysis computes a delta (changed/added/removed/unchanged) and only touches affected files, deleting stale rows before re-inserting.
- Cross-repo link resolution in `analyze` only fires when the codebase belongs to a workspace and only resolves against *other* member codebases, never self.
- MCP server initializes shared singletons (`mcpConnHandle`, `mcpFTS5`, `mcpLogger`, `mcpMetrics`) once per server lifecycle and cleans them up on shutdown.
- `bootstrap --new` refuses to operate on non-local database URLs and creates a timestamped backup before replacing.
- `wrapChunkErr` only appends the remediation hint when the error message contains SQLite's UNIQUE constraint text, leaving all other errors unchanged.

## Non-obvious decisions

- **MCP protocol version mirroring**: The server echoes the client's requested `ProtocolVersion` rather than always enforcing `mcpDefaultProtocolVersion`. This maximizes interop across MCP client versions that may not support the hardcoded default.
- **`memory_upsert` tool commented out**: The storage layer exists but the MCP tool is intentionally excluded because the workflow isn't concrete yet. The comment explicitly notes this is deferred for real-world use cases (e.g., annotating vendor artifacts).
- **`storeFileResult` writes source file record before symbols/edges**: This ordering means a failed symbol insert leaves an orphaned `source_files` row, but the incremental cleanup path handles this by deleting per-file on re-analysis.
- **`mcpConnHandle` uses a shared persistent connection**: Rather than opening/closing connections per tool call, the MCP server holds one long-lived handle. This is safe because SQLite WAL mode allows concurrent readers, but it means the handle must survive the entire server lifecycle.

## Unclear intent

- `mcp_chunker_default.go` and `mcp_analyzer_default.go` are listed in the module files but their source is not included in this snapshot. Their purpose cannot be confirmed from the provided code — they likely implement default chunker/analyzer strategies for MCP-served queries, but the specific design choices are opaque without the source.
