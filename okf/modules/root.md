---
commit: 7a5101573c90ae500f0bf680e308d0e8b7593463
description: 'Codebase knowledge for module: .'
files:
- main.go
- schema.go
tags:
- module
timestamp: '2026-06-24'
title: .
type: Module
---

# Module: . (Root)

## What it does

This module initializes the `agentdb` application by executing the CLI entry point defined in the `cmd` package. It also loads an embedded SQL schema from `data/schema.sql` at startup and makes it available to the database layer.

## Public interface

- `func main()` — Application entry point; calls `cmd.Execute()` and exits with code 1 on error.
- Package-level `init()` — Reads embedded schema file and registers it via `db.SetEmbeddedSchema()`.

## Key invariants

- `main()` must be the only entry point; it never returns without either `cmd.Execute()` succeeding or calling `os.Exit(1)`.
- The embedded schema file `data/schema.sql` must exist at build time (the `//go:embed` directive will fail the build otherwise).
- `db.SetEmbeddedSchema()` is called exactly once during initialization, before any database operations occur.

## Non-obvious decisions

- **Fallback to disk reads for schema**: The `init()` function explicitly handles the case where reading the embedded schema fails, printing a warning to stderr and continuing without calling `db.SetEmbeddedSchema()`. This is unusual because `go:embed` directives make the embedded file mandatory at compile time — if the file is missing, the build itself fails. The fallback suggests either a secondary build pipeline that strips embedded data, or a runtime expectation that the schema file exists on disk for development/debugging purposes despite the embed directive.
