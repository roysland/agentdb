---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/parse'
files:
- internal/parse/go_parser.go
- internal/parse/merge_conflicts.go
- internal/parse/parsers.go
- internal/parse/parsers_treesitter.go
- internal/parse/plugin_dirs.go
- internal/parse/plugin_manifest.go
- internal/parse/plugin_manifest_test.go
- internal/parse/plugin_process.go
- internal/parse/plugin_registry.go
- internal/parse/registry.go
- internal/parse/resilient.go
- internal/parse/resilient_test.go
- internal/parse/treesitter_base.go
- internal/parse/treesitter_python.go
- internal/parse/treesitter_rust.go
- internal/parse/treesitter_typescript.go
- internal/parse/types.go
tags:
- module
timestamp: '2026-06-26'
title: internal/parse
type: Module
---

## What it does

The `parse` module provides multi-language source code parsing for the agentdb system. It extracts symbols (functions, types, interfaces, constants), import relationships, and call-graph edges from source files using a combination of stdlib Go AST parsing and tree-sitter grammars, with a plugin system for extending to additional languages via external subprocesses.

## Public interface

```go
// Parser is the core interface implemented by all parsers.
type Parser interface {
    Language() string
    CanParse(filePath string) bool
    Parse(filePath string, content []byte) (FileResult, error)
}

// FileResult holds the complete extraction for a single source file.
type FileResult struct {
    FilePath, Language, PackageName, FileHash string
    LOC      int64
    Imports  []Import
    Symbols  []Symbol
    Edges    []Edge
}

// Symbol represents a single extracted definition.
type Symbol struct {
    Key, FilePath, Language, Kind, Name, QualName string
    Receiver, Signature, DocComment, BodySnippet   string
    Visibility    string
    StartLine, EndLine int64
    FileHash      string
}

// DefaultParsers returns the built-in parser set (build-tag controlled).
func DefaultParsers() []Parser

// ParseDirectory walks a directory tree and parses all recognized files.
func ParseDirectory(rootPath string, parsers []Parser) ([]FileResult, error)

// PluginRegistry discovers and manages external parser plugins.
func NewPluginRegistry(pluginDirs []string, builtins []Parser) (*PluginRegistry, error)
func (r *PluginRegistry) AllParsers() []Parser
func (r *PluginRegistry) Shutdown()

// StartPlugin launches a single plugin subprocess and performs handshake.
func StartPlugin(manifest PluginManifest, dir string) (*PluginProcess, error)

// ResilientParser wraps tree-sitter parsers with error thresholds.
func NewResilientParser(inner *TreeSitterParser, logger *observe.Logger) *ResilientParser
func NewResilientParserWithThreshold(inner *TreeSitterParser, threshold float64, logger *observe.Logger) *ResilientParser

// HasMergeConflicts checks for git merge conflict markers.
func HasMergeConflicts(content []byte) bool
```

## Key invariants

- **Plugin priority over builtins**: `PluginRegistry.AllParsers()` deduplicates by language — if a plugin handles "python", the built-in Python parser is excluded.
- **Graceful degradation**: `ResilientParser` falls back to `text_fallback` status when error ratio exceeds threshold (default 15%), returns `partial` for merge conflicts, and recovers from tree-sitter panics without crashing.
- **Build-tag gating**: Tree-sitter parsers (Python, TypeScript, Rust) are only available with `-tags treesitter`; the default build is pure Go with only the stdlib Go parser.
- **Content truncation**: `snippetFromContent` caps body snippets at 4000 characters to prevent unbounded memory usage from large files.
- **Deduplicated call edges**: `extractCallEdges` deduplicates within a single function body using a `seen` map keyed by `fromRef→target`.
- **Plugin safe mode**: Setting `AGENTDB_PLUGIN_SAFE_MODE=1` skips all plugin subprocess execution entirely.
- **30-second plugin timeout**: `PluginProcess.Parse` kills the subprocess if it doesn't respond within 30 seconds.

## Non-obvious decisions

- **Partial parse errors are accepted in Go parsing**: The Go parser proceeds with `parser.ParseComments` and works with whatever AST was returned even if `err != nil`, because partial results are considered more valuable than no results.
- **Error threshold is 15% by default**: Files with up to 15% ERROR/MISSING nodes still get full symbol extraction; above that, symbols are empty and status becomes `text_fallback`. This balances noise tolerance against indexing garbage from severely broken files.
- **Merge conflict detection happens before tree-sitter**: Files with conflict markers return `partial` status without attempting AST extraction, avoiding the high error ratios that conflict markers would trigger.
- **Plugin content is base64-encoded**: `PluginProcess.Parse` base64-encodes file content before JSON-RPC transmission to avoid encoding issues with binary or non-UTF8 content in JSON strings.
- **Plugin allowlist uses `AGENTDB_PLUGIN_ALLOWLIST`**: A comma-separated env var restricts which plugin names can load, providing a security control beyond safe mode.

## Unclear intent

- **`tsModuleName`**: Referenced in `resilient.go` but defined in the tree-sitter parser files not shown here. Its exact derivation logic for module/package naming from file paths should be documented in `treesitter_base.go` or the language-specific tree-sitter files.
- **`extractCallEdges` method receiver for non-method calls**: When a call target is a bare identifier (not a selector expression), it's qualified as `pkgName.name`. The resolution of local vs. package-level calls relies on the import alias map — calls to unresolvable local functions get no edge, which may under-report internal call graphs.
