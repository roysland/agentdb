---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .'
files:
- main.go
- schema.go
tags:
- module
timestamp: '2026-06-26'
title: .
type: Module
---

## What it does

agentdb is a Go CLI tool that provides an agent with a persistent, searchable memory store. It embeds a SQL schema at startup and delegates all command execution to the `cmd` package.

## Public interface

- `main()` — entry point; calls `cmd.Execute(context.Background())` and exits with code 1 on error.
- `init()` (in schema.go) — reads the embedded `data/schema.sql` file and registers it with `db.SetEmbeddedSchema`. Falls back to disk reads if the embedded file is unavailable.

## Key invariants

- The schema must be available either via `go:embed` at compile time or from disk at runtime; a warning is printed if the embedded version cannot be loaded.
- `db.SetEmbeddedSchema` is called from an `init()` function, meaning it runs before `main()` and before any command handler accesses the database.

## Non-obvious decisions

- **Schema registration via `init()` rather than explicit setup call**: The embedded schema is injected into the `db` package's global state before `main` runs, so downstream commands never need to pass the schema explicitly. This couples schema loading to package initialization order, which is idiomatic for Go but means the schema cannot be reloaded or swapped without restarting the process.
