//go:build !treesitter

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/roysland/agentdb/internal/chunk"
	"github.com/roysland/agentdb/internal/filefilter"
)

// mcpChunkFileResult holds the result of chunking a single file, including
// parse status metadata for the indexed_files table.
type mcpChunkFileResult struct {
	Chunks       []chunk.Chunk
	FileHash     string
	IndexStatus  string // "complete" | "text_fallback" | "partial"
	StatusReason string
}

// mcpChunkFile chunks a single file. Without the treesitter build tag,
// this uses the TextFallbackChunker for text/markdown content and falls back
// to the standard line-based ChunkFile for code files.
func mcpChunkFile(absPath string, relPath string, linesPerChunk int) (*mcpChunkFileResult, error) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	if len(content) == 0 {
		return &mcpChunkFileResult{
			IndexStatus: "complete",
		}, nil
	}

	// Compute file hash (SHA-256)
	h := sha256.Sum256(content)
	fileHash := hex.EncodeToString(h[:])

	language := chunk.DetectLanguage(absPath)

	// For text/markdown content, use the TextFallbackChunker
	if language == "markdown" || language == "text" {
		textChunker, tcErr := chunk.NewTextFallbackChunker(chunk.DefaultTextChunkerConfig())
		if tcErr == nil {
			chunks, chunkErr := textChunker.ChunkTextFile(absPath, content)
			if chunkErr == nil {
				for i := range chunks {
					chunks[i].FileHash = fileHash
					chunks[i].FilePath = relPath
				}
				return &mcpChunkFileResult{
					Chunks:       chunks,
					FileHash:     fileHash,
					IndexStatus:  "complete",
					StatusReason: "",
				}, nil
			}
		}
	}

	// For code files (or if text chunker failed), use standard line-based chunking
	cfg := chunk.ChunkerConfig{LinesPerChunk: linesPerChunk}
	chunks, err := chunk.ChunkFile(absPath, cfg)
	if err != nil {
		return nil, err
	}

	// Update file paths to relative
	for i := range chunks {
		chunks[i].FilePath = relPath
	}

	return &mcpChunkFileResult{
		Chunks:       chunks,
		FileHash:     fileHash,
		IndexStatus:  "complete",
		StatusReason: "",
	}, nil
}

// mcpChunkDirectory chunks all code files in a directory.
// Without the treesitter build tag, this uses line-based chunking with
// TextFallbackChunker for markdown/text files.
func mcpChunkDirectory(rootPath string, linesPerChunk int) (map[string]*mcpChunkFileResult, error) {
	result := make(map[string]*mcpChunkFileResult)

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !chunk.IsCodeFile(path) {
			return nil
		}

		if !filefilter.IsConfinedRegularFile(rootPath, path, info) {
			return nil
		}

		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}

		fileResult, chunkErr := mcpChunkFile(path, relPath, linesPerChunk)
		if chunkErr != nil {
			// Skip files that fail to chunk
			return nil
		}

		if fileResult != nil && len(fileResult.Chunks) > 0 {
			result[relPath] = fileResult
		}

		return nil
	})

	return result, err
}
