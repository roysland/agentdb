---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/go_parser.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/merge_conflicts.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/parsers.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/parsers_treesitter.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/plugin_dirs.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/plugin_manifest.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/plugin_manifest_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/plugin_process.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/plugin_registry.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/registry.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/resilient.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/resilient_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/treesitter_base.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/treesitter_python.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/treesitter_rust.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/treesitter_typescript.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse/types.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/parse
type: Module
---

# Internal Developer Documentation: `internal/parse` Module

## What it does

The `parse` module is responsible for extracting structured semantic data (symbols, imports, call edges, file metadata) from source code files. It supports Go natively via the standard library's `go/ast`, and optionally supports Python, TypeScript, TSX, JavaScript, and Rust via tree-sitter grammars (enabled with `-tags treesitter`). An external plugin system allows binary parsers to be loaded at runtime for additional languages.

## Public Interface

### Core Types

```go
type Parser interface {
    Language() string
    CanParse(filePath string) bool
    Parse(filePath string, content []byte) (FileResult, error)
}

type FileResult struct {
    FilePath    string
    Language    string
    PackageName string
    LOC         int
    FileHash    string
    Imports     []Import
    Symbols     []Symbol
    Edges       []Edge
}

type Symbol struct {
    Key           string
    FilePath      string
    Language      string
    Kind          string // "func", "method", "struct", "interface", "type", "var", "const"
    Name          string
    QualifiedName string
    Receiver      string
    Signature     string
    DocComment    string
    Visibility    string // "exported" | "unexported"
    BodySnippet   string
    StartLine     int64
    EndLine       int64
    FileHash      string
}

type Edge struct {
    FromKind string // "file" | "symbol"
    FromRef  string
    ToKind   string // "file" | "symbol"
    ToRef    string
    EdgeKind string // "imports" | "calls"
    Line     int64
}

type Import struct {
    Path  string
    Alias string
}
```

### Key Functions

```go
// DefaultParsers returns the built-in parser set (build-tag dependent).
// Without -tags treesitter: only Go. With treesitter: Go + Python + TypeScript + JS + Rust.
func DefaultParsers() []Parser

// ParseDirectory walks rootPath, parses each file with the first matching parser.
// Files with no matching parser are skipped. Parse errors are logged but don't abort.
func ParseDirectory(rootPath string, parsers []Parser) ([]FileResult, error)

// HasMergeConflicts checks for Git merge conflict markers (<<<<<<=, =======, >>>>>>>).
func HasMergeConflicts(content []byte) bool

// PluginDirectories returns directories to scan for parser plugins
// (~/.agentdb/plugins and $AGENTDB_PLUGIN_DIR).
func PluginDirectories() []string

// NewPluginRegistry discovers, loads, and starts external parser plugins.
// Plugins take priority over built-in parsers for the same language.
// Env: AGENTDB_PLUGIN_SAFE_MODE=1 disables all plugins.
// Env: AGENTDB_PLUGIN_ALLOWLIST restricts to specific plugin names.
func NewPluginRegistry(pluginDirs []string, builtins []Parser) (*PluginRegistry, error)

// LoadManifest reads and validates a plugin manifest.json from a directory.
func LoadManifest(dir string) (PluginManifest, error)

// StartPlugin launches a plugin subprocess and performs the capabilities handshake.
func StartPlugin(manifest PluginManifest, dir string) (*PluginProcess, error)
```

### Resilient Parsing (tree-sitter build only)

```go
type ParseResult struct {
    FileResult
    ErrorCount   int
    TotalNodes   int
    ErrorRatio   float64
    ErrorRanges  []LineRange
    IndexStatus  string // "complete" | "text_fallback" | "partial"
    StatusReason string
}

func NewResilientParser(inner *TreeSitterParser, logger *observe.Logger) *ResilientParser
func NewResilientParserWithThreshold(inner *TreeSitterParser, threshold float64, logger *observe.Logger) *ResilientParser
```

## Key Invariants

- **Partial results are valuable**: `ParseDirectory` never aborts on a single file parse error. It logs a warning and continues walking. A file that partially parses still contributes whatever symbols were extracted.
- **Plugin priority over built-ins**: When a plugin and a built-in both declare the same language, `PluginRegistry.AllParsers()` includes only the plugin. This is intentional — plugins are assumed to be more capable.
- **Merge conflict detection short-circuits**: Files containing `<<<<<<<`, `=======`, and `>>>>>>>` markers are flagged with `IndexStatus: "partial"` and no AST extraction is attempted. This prevents garbage symbols from conflicted regions.
- **Error threshold for tree-sitter**: The default resilient threshold is 15% (`0.15`). If more than 15% of AST nodes are `ERROR`/`MISSING`, the parser falls back to `text_fallback` status with empty symbols rather than indexing unreliable data.
- **Plugin subprocess timeout**: `PluginProcess.Parse` enforces a 30-second timeout. If exceeded, the subprocess is killed. This prevents a hung plugin from blocking the entire index.
- **Build-tag gating**: The tree-sitter parsers and resilient parser are only available with `-tags treesitter` (which requires CGo). The default build is pure Go with only `go/ast`-based parsing.
- **Symbol keys are file-scoped**: `Symbol.Key` is `filePath:qualifiedName`, ensuring uniqueness within an index. The qualified name includes the package (e.g., `mypackage.MyStruct.MyMethod`).
- **Content is base64-encoded for plugins**: Plugin subprocess communication sends file content as base64 in JSON-RPC to avoid encoding issues over stdin/stdout pipes.

## Non-Obvious Decisions

- **`go/ast` for Go, not tree-sitter**: Go is parsed via the standard library regardless of the `treesitter` build tag. This is deliberate — `go/ast` is more reliable for Go than tree-sitter-go, handles all Go syntax including generics, and avoids the CGo dependency for the most common use case.
- **Plugin communication is JSON-RPC 2.0 over stdin/stdout, not gRPC or Unix sockets**: This keeps plugins trivially runnable — any binary that reads newline-delimited JSON from stdin and writes responses to stdout works. No socket management, no port conflicts. The tradeoff is that only one parse call can be in flight per plugin at a time (enforced by the mutex in `PluginProcess.call`).
- **`HasMergeConflicts` is in a no-build-tag file**: It's used by both the tree-sitter resilient parser and potentially other code, so it lives in `merge_conflicts.go` without build constraints rather than alongside the tree-sitter-specific code.
- **`snippetFromContent` truncates at 4000 bytes**: Body snippets are capped to prevent bloating the index with very large function bodies. This is a pragmatic limit — large enough for inspection, small enough to keep the index manageable.
- **`resolveCallTarget` returns just the method name for calls on local variables**: When the receiver is a local variable (not an import alias), the edge target is the bare method name (e.g., `String()`) rather than a fully qualified name. This is a known limitation — type inference for local variables would require type-checking, which is out of scope for a parser.

## Unclear Intent

- **`tsModuleName(filePath)`** (called in `resilient.go` but not defined in this module): This function extracts a module/package name from a file path for tree-sitter languages. Its implementation likely lives in `treesitter_base.go` or another file in the package. The exact heuristic (directory name? `package.json` name? `go.mod`?) is not visible from these files alone.
- **The `capabilities` handshake method name**: The plugin handshake calls `"capabilities"` but this isn't formally documented as a JSON-RPC method specification here. The expected request/response shape is only visible in the code (`capabilitiesResult` struct). A formal plugin author would need to reverse-engineer the protocol from `plugin_process.go`.
