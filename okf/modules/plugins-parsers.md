---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: plugins/parsers'
files:
- plugins/parsers/main.go
- plugins/parsers/stub.go
tags:
- module
timestamp: '2026-06-26'
title: plugins/parsers
type: Module
---

# `plugins/parsers`

## What it does

Provides a standalone JSON-RPC 2.0 plugin binary that exposes Python, TypeScript, TSX, JavaScript, and Rust parsing capabilities to agentdb via stdin/stdout communication. It is built with the `treesitter` build tag; without it, a stub binary is produced that exits with an error instructing the user to rebuild correctly.

## Public interface

This package's `main` function is not intended for direct Go import. It is a CLI binary consumed by agentdb's plugin protocol. The protocol surface is:

- **`capabilities`** — returns `{ languages: string[], extensions: string[] }`
- **`parse`** — accepts `{ file_path: string, content: base64 string }`, returns the parser's result
- **`shutdown`** — exits the process

Internal helper functions (unexported, only relevant if modifying this package):

```go
func buildCapabilities(parsers []parse.Parser) capabilitiesResult
func findParser(filePath string, parsers []parse.Parser) parse.Parser
func writeResult(id *int, result any)
func writeError(id *int, code int, msg string)
func emit(resp rpcResponse)
```

## Key invariants

- The binary must be built with `-tags treesitter`; the stub build (`!treesitter`) deliberately fails at runtime with a diagnostic message.
- Each RPC response is exactly one JSON object followed by `\n`; the scanner reads line-by-line, so responses must never contain embedded newlines.
- `req.ID` is a `*int` — the default case guards against `nil` IDs (notifications) to avoid sending spurious error responses.
- `content` is base64-encoded to keep the JSON-RPC payload valid (raw source bytes may contain invalid UTF-8 or control characters).
- The 4 MiB scanner buffer accommodates large base64-encoded source files.
- `findParser` returns the first parser whose `CanParse` matches; parser order in the slice determines priority when extensions overlap.

## Non-obvious decisions

- **Base64 encoding of `content`**: Source files are passed as base64 strings rather than raw JSON strings. This avoids issues with control characters, invalid UTF-8, or embedded quotes breaking the JSON-RPC envelope. A reader might wonder why not just use a raw JSON string — the answer is that Go's `json.Marshal` would escape many common byte sequences, but base64 is unambiguous and avoids edge cases entirely.

- **`*int` for the RPC ID**: Using a pointer-to-int rather than `int` allows distinguishing between requests with ID `0` and notification-style requests with no ID at all (`nil`). This matters because the default case only writes a response when `req.ID != nil`, preventing the plugin from replying to notifications.

- **Process-per-request vs. long-lived**: The `shutdown` method calls `os.Exit(0)` rather than returning gracefully. This is consistent with the plugin protocol where the parent process manages lifecycle — the plugin runs until told to stop, then terminates immediately.

## Unclear intent

- **`knownExts` in `buildCapabilities`**: The extension list is hardcoded to `{".py", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".rs"}` rather than derived from each parser's own reported extensions. If a parser later supports additional extensions (e.g., `.pyi` for Python stubs), they would not appear in capabilities unless this list is manually updated. The relationship between this hardcoded list and whatever `parse.Parser` exposes via `CanParse` is not documented.
