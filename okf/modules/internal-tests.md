---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/tests'
files:
- internal/tests/memory_task_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/tests
type: Module
---

## What it does

An integration test that verifies the end-to-end memory persistence flow: it opens a temporary SQLite database, inserts a row into the `memories` table, reads it back, and asserts the content matches. It also loads the project's schema file via `db.SetEmbeddedSchema` so that `db.Open` auto-bootstraps the schema in the test environment.

## Public interface

- `TestMemoryFlow(t *testing.T)` — the only public function; a standard Go test that exercises insert and query paths on the `memories` table.

## Key invariants

- The schema must be loaded before `db.Open` is called, otherwise the `memories` table won't exist and the test will fail.
- The test database is created in `t.TempDir()` and is automatically cleaned up when the test finishes.
- The `DatabaseDriver` must be `"sqlite"` to match the imported `modernc.org/sqlite` driver.

## Non-obvious decisions

- The `init()` function reads the schema from a relative path (`../../data/schema.sql`) rather than embedding it with `//go:embed`. This means the test depends on the working directory being `internal/tests` at execution time; running `go test` from any other directory would silently skip schema loading (the error is swallowed with `if err == nil`).

## Unclear intent

- `SuppressBootstrapWarning: true` — resolved. `db.Open` auto-applies `data/schema.sql` when it detects the target database is missing the core tables, and emits a one-time warning to stderr when it does. In tests, databases are always freshly created so this warning fires on every test run and is noise. Setting this flag in the test `Runtime` suppresses the warning without changing any functional behavior.
