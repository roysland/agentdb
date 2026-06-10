//go:build treesitter

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/roysland/agentdb/internal/chunk"
	"github.com/roysland/agentdb/internal/filefilter"
	"github.com/roysland/agentdb/internal/parse"
)

// mcpChunkFileResult holds the result of chunking a single file, including
// parse status metadata for the indexed_files table.
type mcpChunkFileResult struct {
	Chunks       []chunk.Chunk
	FileHash     string
	IndexStatus  string // "complete" | "text_fallback" | "partial"
	StatusReason string
}

// mcpChunkFile chunks a single file using the AST chunker for supported languages,
// falling back to the TextFallbackChunker for unsupported languages.
// When the treesitter build tag is active, this uses the full AST chunker.
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

	// Detect language
	language := chunk.DetectLanguage(absPath)

	// Try AST chunker for supported languages
	astChunker := chunk.NewASTChunker(defaultParsers(), chunk.ASTChunkerConfig{
		MaxChunkLines: linesPerChunk,
	})

	// Check if AST chunker supports this language by checking if a parser can handle it
	var hasParser bool
	for _, p := range defaultParsers() {
		if p.CanParse(absPath) {
			hasParser = true
			break
		}
	}

	var chunks []chunk.Chunk
	indexStatus := "complete"
	statusReason := ""

	if hasParser {
		// Use AST chunker for supported languages
		chunks, err = astChunker.ChunkFile(absPath, content, language)
		if err != nil {
			// AST chunking failed, fall back to text chunker
			chunks, indexStatus, statusReason, err = chunkWithTextFallback(content, absPath, relPath, language, fileHash)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// No parser available — use text fallback chunker
		chunks, indexStatus, statusReason, err = chunkWithTextFallback(content, absPath, relPath, language, fileHash)
		if err != nil {
			return nil, err
		}
	}

	// Ensure all chunks have the correct file path (relative) and file hash
	for i := range chunks {
		chunks[i].FilePath = relPath
		if chunks[i].FileHash == "" {
			chunks[i].FileHash = fileHash
		}
	}

	return &mcpChunkFileResult{
		Chunks:       chunks,
		FileHash:     fileHash,
		IndexStatus:  indexStatus,
		StatusReason: statusReason,
	}, nil
}

// chunkWithTextFallback uses the TextFallbackChunker for files without AST support.
func chunkWithTextFallback(content []byte, absPath, relPath, language, fileHash string) ([]chunk.Chunk, string, string, error) {
	textChunker, err := chunk.NewTextFallbackChunker(chunk.DefaultTextChunkerConfig())
	if err != nil {
		// If text chunker fails to initialize, fall back to basic line chunking
		cfg := chunk.ChunkerConfig{LinesPerChunk: 50}
		chunks, chunkErr := chunk.ChunkFile(absPath, cfg)
		if chunkErr != nil {
			return nil, "complete", "", chunkErr
		}
		for i := range chunks {
			chunks[i].FilePath = relPath
		}
		return chunks, "text_fallback", "text chunker initialization failed: " + err.Error(), nil
	}

	chunks, err := textChunker.ChunkTextFile(absPath, content)
	if err != nil {
		return nil, "complete", "", err
	}

	// Set file hash on all chunks
	for i := range chunks {
		chunks[i].FileHash = fileHash
		chunks[i].FilePath = relPath
		if chunks[i].Language == "" {
			chunks[i].Language = language
		}
	}

	return chunks, "complete", "", nil
}

// defaultParsers returns the set of tree-sitter parsers available for AST chunking.
func defaultParsers() []parse.Parser {
	return parse.DefaultParsers()
}

// mcpChunkDirectory chunks all code files in a directory using AST-aware chunking.
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

		fileResult, err := mcpChunkFile(path, relPath, linesPerChunk)
		if err != nil {
			// Skip files that fail to chunk (same behavior as original)
			return nil
		}

		if fileResult != nil && len(fileResult.Chunks) > 0 {
			result[relPath] = fileResult
		}

		return nil
	})

	return result, err
}
