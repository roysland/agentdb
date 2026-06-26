---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/chunk'
files:
- internal/chunk/ast_chunker.go
- internal/chunk/ast_chunker_test.go
- internal/chunk/chunker.go
- internal/chunk/chunker_test.go
- internal/chunk/text_chunker.go
- internal/chunk/text_chunker_test.go
- internal/chunk/treesitter_langs.go
tags:
- module
timestamp: '2026-06-26'
title: internal/chunk
type: Module
---

# `internal/chunk` Module Documentation

## What it does

The `chunk` module splits source code and text files into semantically meaningful segments for embedding and retrieval. It provides three chunking strategies: a simple line-based chunker for general code files, an AST-aware chunker that uses tree-sitter to split at language structure boundaries (functions, classes, etc.), and a text fallback chunker that uses BPE tokenization with content-type-aware splitting for markdown and prose.

## Public interface

```go
// Core types
type Chunk struct {
    Key, FilePath, Language, Kind, Name, Signature, Snippet, FileHash string
    StartLine, EndLine int64
    EmbeddingModel string
}

// Line-based chunking
type ChunkerConfig struct { LinesPerChunk int }
func DefaultConfig() ChunkerConfig
func ChunkFile(filePath string, config ChunkerConfig) ([]Chunk, error)
func ChunkDirectory(rootPath string, config ChunkerConfig) (map[string][]Chunk, error)
func DetectLanguage(filePath string) string

// AST chunking (build tag: treesitter)
type ASTChunkerConfig struct { MaxChunkLines int }
func DefaultASTChunkerConfig() ASTChunkerConfig
func NewASTChunker(parsers []parse.Parser, config ASTChunkerConfig) *ASTChunker
func (ac *ASTChunker) ChunkFile(filePath string, content []byte, language string) ([]Chunk, error)

// Text fallback chunking
type TextChunkerConfig struct { TokenWindow, TokenOverlap int }
func DefaultTextChunkerConfig() TextChunkerConfig
func NewTextFallbackChunker(config TextChunkerConfig) (*TextFallbackChunker, error)
func (tc *TextFallbackChunker) ChunkText(content []byte, contentType string) ([]Chunk, error)
```

## Key invariants

- **Round-trip property (AST chunker):** Concatating all `Snippet` fields from `ASTChunker.ChunkFile()` output reproduces the original file content exactly. Gaps between AST nodes (whitespace, inter-node comments) are appended to the previous chunk's snippet.
- **File hash is always SHA-256:** Every `Chunk.FileHash` is a 64-character hex-encoded SHA-256 digest. Files larger than 10 MB are hashed via streaming to avoid doubling memory usage; the result is byte-identical to in-memory hashing.
- **Key uniqueness within a file:** `Chunk.Key` is formatted as `filePath:startLine:endLine`. Since start/end line ranges are non-overlapping within a file, keys are unique.
- **Text chunker overlap is strictly less than window:** `TokenOverlap` is clamped to `TokenWindow / 5` if the user configures it >= `TokenWindow`, preventing infinite loops or degenerate chunks.
- **AST chunker falls back gracefully:** If no parser matches the file, tree-sitter fails to parse, or the file is empty, the AST chunker delegates to fixed-line chunking identical to `ChunkFile` (producing `Kind: "code"` chunks).
- **Markdown sections preserve header hierarchy:** `chunkMarkdown` splits at header boundaries first, then applies token windowing within oversized sections. The section's header text populates the chunk `Name`.

## Non-obvious decisions

- **Gap content is merged into the previous chunk, not the next.** When walking top-level AST nodes, whitespace/comments between nodes are appended to the preceding chunk. This means the last chunk in a file may include trailing content that isn't part of its AST node. The alternative (creating separate gap chunks) would produce many tiny, semantically meaningless chunks.
- **`parseTree` re-parses instead of reusing the parser's tree.** The `Parser` interface doesn't expose the internal `sitter.Language`, so the AST chunker calls `parser.Language()` and looks up the tree-sitter grammar via `getTreeSitterLanguage()`. This means every file is parsed twice — once to validate the parser can handle it, and once to walk the AST. This is a deliberate trade-off to keep the `parse` module's interface clean at the cost of redundant parsing.
- **Subdivision groups consecutive children greedily.** When an AST node exceeds `MaxChunkLines`, children are accumulated into sub-chunks until adding the next child would exceed the limit. This is a simple greedy algorithm — it doesn't optimize for balanced chunk sizes or try to align with logical groupings beyond line count.
- **`chunkProseWithOffset` exists to fix line numbers in markdown sub-chunks.** When a markdown section is too large and gets split via prose windowing, the line numbers need an offset from the section's start in the original file. Without this, sub-chunks would report line numbers relative to the section text rather than the file.

## Unclear intent

- The `EmbeddingModel` field on `Chunk` is declared and exported but never set by any code in this module. It appears to be a placeholder for downstream consumers (likely the `internal/index` or `internal/search` modules) to populate after chunking. The field's semantics (which model, who sets it, whether it's persisted) are not defined here.
- The `parentSig` parameter in `chunkNode` builds a breadcrumb signature path (e.g., `"MyClass > method_a"`) for subdivided nodes. However, the signature of the parent node itself is already captured in the first sub-chunk via `nodeSignature`. The breadcrumb is only meaningful for deeply nested subdivisions and its value in retrieval is unclear.
