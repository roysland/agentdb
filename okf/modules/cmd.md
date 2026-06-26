---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
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
timestamp: '2026-06-26'
title: cmd
type: Module
---

## What it does

The `cmd` package defines all CLI subcommands for the `agentdb` binary using Cobra. It exposes commands for bootstrapping the database schema, registering and managing codebases, parsing source into symbols/edges, chunk-based indexing, import/export of portable artifacts, MCP (Model Context Protocol) server mode, and various search/introspection utilities. Each command wires together configuration resolution, database connection lifecycle, and repository/store abstractions from the `internal` packages.

## Public interface

- `Execute(ctx context.Context, cfg config.Runtime) error` — Entry point that builds the root Cobra command and dispatches to all subcommands.
- `newBootstrapCmd(ctx) *cobra.Command` — Schema migration with optional `--new` DB reset.
- `newCodebaseCmd(ctx) *cobra.Command` — Subcommands `register` and `list`.
- `newAnalyzeCmd(ctx) *cobra.Command` — Full or incremental symbol/edge extraction (`--codebase-id`, `--codebase-path`, `--incremental`).
- `newIndexCmd(ctx) *cobra.Command` — Full or incremental chunk indexing (`--lines-per-chunk`, `--incremental`).
- `newExportCmd(ctx) *cobra.Command` — Export a codebase to a `.agentdb` artifact (`--strip-source`).
- `newImportCmd(ctx) *cobra.Command` — Import from an artifact path.
- `newMCPCmd(ctx) *cobra.Command` — Starts a stdio MCP server exposing tools like `search`, `register_codebase`, `index_codebase`, `analyze_codebase`, `list_codebases`, `index_status`.
- `newLocateIssueCmd(ctx) *cobra.Command` — Natural-language issue-to-codebase impact mapping.
- `newQueryCmd(ctx) *cobra.Command` — Ad-hoc search across chunks/memories.
- `newWatchCmd(ctx) *cobra.Command` — Filesystem watch loop triggering re-index/re-analyze.
- `newMemoryCmd(ctx) *cobra.Command` — CRUD for stored memory entries.
- `newWorkspaceCmd(ctx) *cobra.Command` — Workspace management (add/remove members).
- `newProjectPathCmd(ctx) *cobra.Command` — Resolve project paths.
- `newVersionCmd(ctx) *cobra.Command` — Print version string.
- `newTargetResolutionCmd(ctx) *cobra.Command` — Resolve codebase targets.
- `newMCPTestCmd(ctx) *cobra.Command` — MCP integration testing utilities.
- `newMCPChunkerDefaultCmd(ctx) *cobra.Command` — Default chunker configuration for MCP.
- `newMCPAnalyzerDefaultCmd(ctx) *cobra.Command` — Default analyzer configuration for MCP.
- `printJSON(v any) error` — Shared helper that serializes results to stdout as JSON.

## Key invariants

- Every command that touches a codebase resolves its target through `resolveCodebaseTarget`, which validates the codebase exists (or registers it) before proceeding.
- Database connections are opened via `db.Open(ctx, resolved)` and deferred `Close()` in every command; the MCP server uses a shared persistent `ConnectionHandle` instead.
- Incremental modes (`--incremental`) always fall back to full runs when no stored hashes exist — they never error on empty state.
- All commands output results via `printJSON` to stdout, keeping stderr for diagnostics and warnings.
- The MCP server mirrors the client's requested `ProtocolVersion` when present, defaulting to `"2024-11-05"`.
- Cross-repo link resolution in `analyze` only runs when the codebase belongs to a workspace and only resolves against *other* member codebases, never self.

## Non-obvious decisions

- **`wrapChunkErr` appends a remediation hint only on UNIQUE constraint failures** — This is a deliberate UX choice: stale chunks from an interrupted run produce a specific SQLite error, and the hint tells the user to re-run without `--incremental`. Other errors pass through unmodified to avoid noise.
- **`memory_upsert` is commented out in `mcpTools()`** — The storage layer exists but the MCP tool is intentionally not exposed. The comment notes this is because the workflow isn't concrete yet, not because of a technical limitation.
- **`mcpServerDescription` explicitly prohibits reconstructing source from results** — This is a license/policy constraint embedded in the MCP protocol handshake, not a technical one. It reflects that indexed content is for navigation only.
- **`analyze` clears all symbols/edges/source_files for the codebase before a full run** — This is a delete-then-reinsert strategy rather than upsert, chosen because cross-file relationships may change entirely between runs.

## Unclear intent

- The `newMCPTestCmd`, `newMCPChunkerDefaultCmd`, and `newMCPAnalyzerDefaultCmd` commands are listed in the file manifest but their source code is not included in this snapshot, so their exact purpose and behavior cannot be determined from the provided context.
