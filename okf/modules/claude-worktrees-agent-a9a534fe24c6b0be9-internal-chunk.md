---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk/ast_chunker.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk/ast_chunker_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk/chunker.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk/chunker_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk/text_chunker.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk/text_chunker_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk/treesitter_langs.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/chunk
type: Module
---

# Internal Developer Documentation: `internal/chunk`

## What it does

The `chunk` package provides three strategies for splitting source code and text into discrete, indexed units suitable for embedding and retrieval. It supports fixed-line chunking of code files, AST-aware chunking using tree-sitter (behind a `treesitter` build tag), and token-boundary chunking for prose and markdown using a BPE tokenizer. Every chunk carries a SHA-256 file hash, line range, language detection, and structural metadata (kind, name, signature).

## Public interface

```go
// Core chunking
func ChunkFile(filePath string, config ChunkerConfig) ([]Chunk, error)
func ChunkDirectory(rootPath string, config ChunkerConfig) (map[string][]Chunk, error)
func DetectLanguage(filePath string) string

// AST chunking (requires: go build -tags treesitter)
func NewASTChunker(parsers []parse.Parser, config ASTChunkerConfig) *ASTChunker
func (ac *ASTChunker) ChunkFile(filePath string, content []byte, language string) ([]Chunk, error)

// Text/prose chunking
func NewTextFallbackChunker(config TextChunkerConfig) (*TextFallbackChunker, error)
func (tc *TextFallbackChunker) ChunkText(content []byte, contentType string) ([]Chunk, error)

// Types
type Chunk struct { Key, FilePath, Language, Kind, Name, Signature, Snippet, FileHash string; StartLine, EndLine int64 }
type ChunkerConfig struct { LinesPerChunk int }
type ASTChunkerConfig struct { MaxChunkLines int }
type TextChunkerConfig struct { TokenWindow, TokenOverlap int }
```

## Key invariants

- **Round-trip property (AST chunker):** Concatating all `Snippet` fields from `ASTChunker.ChunkFile` output reproduces the original file byte-for-byte. Gaps between AST nodes (whitespace, inter-node comments) are appended to the previous chunk's snippet; leading and trailing gaps create their own chunks or attach to the nearest chunk.
- **File hash is always SHA-256:** Every `Chunk.FileHash` is a 64-character hex-encoded SHA-256 digest. Files larger than 10 MB are hashed via streaming to avoid doubling memory.
- **Fallback chain:** The AST chunker always falls back to fixed-line chunking (same strategy as `ChunkFile`) when no parser matches, parsing fails, or the tree is empty. Fallback chunks use `Kind: "code"`.
- **Token overlap < window:** `TextFallbackChunker` enforces `TokenOverlap < TokenWindow` by silently adjusting if misconfigured.
- **Markdown sections preserve headers:** `chunkMarkdown` splits on header boundaries first, then applies token windowing within oversized sections. The header name populates the chunk's `Name` field.
- **Build tag isolation:** All tree-sitter code is gated behind `//go:build treesitter`. The non-treesitter chunkers have zero external parser dependencies.

## Non-obvious decisions

- **Gap attachment to previous chunk:** In `walkTopLevel`, whitespace/comments between AST nodes are appended to the *previous* chunk rather than creating a standalone chunk. This prevents tiny whitespace-only chunks and keeps logical units together, but it means a chunk's `Snippet` may not start at its `StartLine` — the `StartLine` reflects the logical node start, while the snippet text includes preceding gap content.
- **Streaming hash threshold at 10 MB:** The constant `streamingHashThreshold = 10 * 1024 * 1024` avoids re-reading large files. This is a memory/performance tradeoff; below 10 MB the in-memory hash is cheaper since the content is already loaded.
- **AST chunker uses `parse.Parser` interface, not direct tree-sitter:** `parse.Parser` doesn't expose its `*sitter.Language`, so `parseTree` calls `parser.Language()` (a string like `"python"`) and maps it via `getTreeSitterLanguage`. This indirection exists because parsers are defined in `internal/parse` and the chunker must work through the abstraction.
- **Signature truncation at 200 chars:** `nodeSignature` caps extracted signatures to 200 characters to prevent extremely long declarations (e.g., generics-heavy Rust) from bloating index entries.
- **`TextFallbackChunker` uses `cl100k_base` encoding:** This is the GPT-3.5/GPT-4 tokenizer, chosen because it provides reasonable token boundaries for mixed-language prose without requiring language-specific models.

## Unclear intent

- **`subdivideNode` gap handling:** When subdividing a large node, gaps between children are included in the *next* child's text (`childText := gap + ...`), but the `currentStartLine` is adjusted to the gap's start line. This means a sub-chunk's `StartLine` may precede all its actual content. The intent appears to be preserving the round-trip invariant, but the interaction between gap-adjusted start lines and the `flushChunk` logic is subtle and could produce unexpected line ranges for deeply nested subdivisions.
- **`chunkNode` parent signature propagation:** When a child node has no signature of its own, it inherits `parentSig`. When both exist, they concatenate with `" > "`. This creates signatures like `"func foo() > class Bar"`, but the directionality (parent > child vs. child > parent) and its usefulness for downstream consumers is not documented.
