---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/main.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/schema.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9
type: Module
---

## What it does

This is the entry point package for the `agentdb` binary. It bootstraps the CLI command tree via `cmd.Execute` and registers the embedded database schema at startup. The `schema.go` file uses a Go `embed` directive to bundle `data/schema.sql` into the binary, falling back to disk reads if the embedded asset is unavailable.

## Public interface

- `func main()` — Program entry point; delegates to `cmd.Execute(context.Background())`, exits with code 1 on error.
- `func init()` — Registers the embedded schema with `db.SetEmbeddedSchema`; logs a warning to stderr and falls back to disk if the embed fails.

## Key invariants

- `db.SetEmbeddedSchema` is called exactly once at package init time, before `cmd.Execute` runs.
- If the embedded schema cannot be read, the program does **not** fail — it falls back to disk reads and only prints a warning.

## Non-obvious decisions

- **Schema registration via `init()` rather than explicit call in `main()`**: The embedded schema is registered before `main` runs, meaning any code path that calls `db` functions (including `cmd.Execute` and its subcommands) will always have the schema available without requiring an explicit initialization step. This avoids a two-phase startup but couples schema registration to package load order.

- **Fallback to disk reads on embed failure**: Rather than treating a missing embedded schema as fatal, the code silently degrades. This is intentional for development (e.g., running from a checkout where `data/schema.sql` may not be embedded correctly), but could mask deployment issues where the embed should have worked.
