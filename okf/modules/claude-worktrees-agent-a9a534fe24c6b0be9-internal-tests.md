---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/tests'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/tests/memory_task_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/tests
type: Module
---

## What it does

An integration test that verifies the core memory persistence flow: inserting a row into the `memories` table and reading it back. It bootstraps a temporary SQLite database using the project's schema file and the `db` package's auto-bootstrap mechanism.

## Public interface

This file is a test file (`package tests`) and exposes no public API. It contains:

- `func init()` — loads `../../data/schema.sql` and registers it via `db.SetEmbeddedSchema` so `db.Open` auto-bootstraps the schema.
- `func TestMemoryFlow(t *testing.T)` — the single test case exercising insert and select on the `memories` table.

## Key invariants

- The schema file at `../../data/schema.sql` must exist and define a `memories` table with columns `id`, `content`, `category`, and `created_at` for the test to pass.
- `db.SetEmbeddedSchema` must be called before `db.Open` for auto-bootstrap to work; the `init()` function guarantees this ordering.
- The test uses `t.TempDir()` so the SQLite database is cleaned up automatically after the test completes.

## Non-obvious decisions

- **`init()` instead of `TestMain` or a helper**: The schema is loaded in `init()` rather than in `TestMain` or the test body itself. This means the embedded schema is set once for the entire package's test binary, not per-test. This is safe here because the schema is read-only and never mutated, but it creates a subtle coupling: if any other test in `internal/tests` runs in the same binary and depends on a different schema state, the behavior could be unexpected. The choice of `init()` over `TestMain` is a minor trade-off of explicitness for brevity.

## Unclear intent

- The `category` column is inserted with value `"notes"` but never asserted. It's unclear whether this is intentional (testing that the column accepts arbitrary values) or an oversight (a planned assertion that was never added).
