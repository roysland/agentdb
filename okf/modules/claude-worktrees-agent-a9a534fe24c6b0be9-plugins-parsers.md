---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/plugins/parsers'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/plugins/parsers/main.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/plugins/parsers/stub.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/plugins/parsers
type: Module
---

## What it does

`plugins/parsers` is a standalone Go binary that wraps Tree-sitter-based parsers for Python, TypeScript, TSX, JavaScript, and Rust, exposing them to agentdb via a JSON-RPC 2.0 plugin protocol over stdin/stdout. It is only built when the `treesitter` build tag is active; without it, a stub binary exits with an error message.

## Public interface

This package has no exported Go API — it is a `package main` that compiles to a standalone binary. The protocol surface (JSON-RPC 2.0 methods) is:

- **`capabilities`** → returns `{ languages: string[], extensions: string[] }`
- **`parse`** — params: `{ file_path: string, content: string (base64) }` → returns parser output from `internal/parse`
- **`shutdown`** → exits the process

Internal helper functions (unexported, not part of the public API):

- `buildCapabilities(parsers []parse.Parser) capabilitiesResult`
- `findParser(filePath string, parsers []parse.Parser) parse.Parser`
- `writeResult(id *int, result any)`
- `writeError(id *int, code int, msg string)`
- `emit(resp rpcResponse)`

## Key invariants

- The `treesitter` build tag gates all real functionality; the stub (`!treesitter`) must always fail at runtime rather than silently no-op.
- Each input line is treated as a single JSON-RPC request; the scanner never frames multi-line JSON.
- `rpcRequest.ID` is a `*int` — a nil ID means the message is a notification and must not receive a response.
- `params.Content` is always base64-encoded; the binary decodes before passing to any parser.
- The 4 MiB scanner buffer is required because base64-encoded source files can exceed `bufio.Scanner`'s default 64 KiB limit.

## Non-obvious decisions

- **Process-per-request vs. long-lived process**: The binary stays alive across many `parse` calls and only exits on an explicit `shutdown` method. This avoids the startup cost of loading Tree-sitter grammars for each file, which would be significant.
- **`capabilities` derives extensions from `CanParse` probes rather than a static list**: The `knownExts` slice is used to probe each parser at startup, so the advertised extensions reflect what the parser actually accepts. This means adding a new extension to a parser's implementation automatically surfaces it without changing this plugin code.
- **`emit` ignores `json.Marshal` errors**: The only way `json.Marshal` fails is if the value isn't marshalable, and `any` is used for the result. This is a deliberate trade-off — a marshal failure here would indicate a parser bug, and crashing the plugin is preferable to silently dropping the response.

## Unclear intent

- **No `parse` result type is defined here**: The return value of `p.Parse(...)` is passed through as `any`. The structure of parse results is entirely determined by `internal/parse`, which is listed as a sibling module. A reader of this plugin alone cannot know what fields a `parse` response contains without consulting that module.
