package chunk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/roysland/agentdb/internal/filefilter"
)

// streamingHashThreshold is the file size above which we use streaming SHA-256
// instead of hashing the in-memory content. 10MB.
const streamingHashThreshold = 10 * 1024 * 1024

// Chunk represents a logical unit of code
type Chunk struct {
	Key            string // Unique identifier (codebase_id + file_path + start_line)
	FilePath       string
	Language       string
	Kind           string
	Name           string
	Signature      string
	Snippet        string
	StartLine      int64
	EndLine        int64
	FileHash       string
	EmbeddingModel string
}

// ChunkerConfig contains configuration for the chunking process
type ChunkerConfig struct {
	LinesPerChunk int
}

// DefaultConfig returns the default chunking configuration
func DefaultConfig() ChunkerConfig {
	return ChunkerConfig{
		LinesPerChunk: 50,
	}
}

// ChunkFile splits a file into chunks based on line boundaries
func ChunkFile(filePath string, config ChunkerConfig) ([]Chunk, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Calculate file hash using SHA-256 (64-character hex string).
	// For files > 10MB, use streaming hash to avoid doubling memory usage.
	var fileHash string
	if len(content) > streamingHashThreshold {
		fileHash, err = streamingHash(filePath)
		if err != nil {
			return nil, fmt.Errorf("hash file: %w", err)
		}
	} else {
		h := sha256.Sum256(content)
		fileHash = hex.EncodeToString(h[:])
	}

	lines := strings.Split(string(content), "\n")
	language := DetectLanguage(filePath)
	var chunks []Chunk

	linesPerChunk := config.LinesPerChunk
	if linesPerChunk <= 0 {
		linesPerChunk = 50
	}

	for i := 0; i < len(lines); i += linesPerChunk {
		startLine := int64(i + 1)
		endLine := int64(i + linesPerChunk)
		if endLine > int64(len(lines)) {
			endLine = int64(len(lines))
		}

		snippet := strings.Join(lines[i:min(i+linesPerChunk, len(lines))], "\n")
		if strings.TrimSpace(snippet) == "" {
			continue
		}

		chunk := Chunk{
			FilePath:  filePath,
			Language:  language,
			Kind:      "code",
			Name:      fmt.Sprintf("%s:%d-%d", filepath.Base(filePath), startLine, endLine),
			Snippet:   snippet,
			StartLine: startLine,
			EndLine:   endLine,
			FileHash:  fileHash,
		}
		chunk.Key = fmt.Sprintf("%s:%d-%d", filePath, startLine, endLine)

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// streamingHash computes SHA-256 by streaming the file content through the hash
// function, avoiding loading the entire file into memory a second time.
func streamingHash(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ChunkDirectory recursively chunks all code files in a directory
func ChunkDirectory(rootPath string, config ChunkerConfig) (map[string][]Chunk, error) {
	result := make(map[string][]Chunk)
	matcher := filefilter.NewMatcher(rootPath)

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if matcher.ShouldSkipDir(path, info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if !filefilter.IsConfinedRegularFile(rootPath, path, info) {
			return nil
		}

		// Skip directories and non-code files
		if !matcher.IsCodeFile(path) {
			return nil
		}

		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}

		chunks, err := ChunkFile(path, config)
		if err != nil {
			return fmt.Errorf("chunk file %s: %w", path, err)
		}

		result[relPath] = chunks
		return nil
	})

	return result, err
}

// DetectLanguage returns the programming language based on file extension
func DetectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	languageMap := map[string]string{
		".go":   "go",
		".py":   "python",
		".js":   "javascript",
		".ts":   "typescript",
		".java": "java",
		".cpp":  "cpp",
		".c":    "c",
		".h":    "c",
		".rs":   "rust",
		".rb":   "ruby",
		".php":  "php",
		".sql":  "sql",
		".sh":   "bash",
		".md":   "markdown",
		".json": "json",
		".yaml": "yaml",
		".yml":  "yaml",
		".xml":  "xml",
		".html": "html",
		".css":  "css",
	}

	if lang, ok := languageMap[ext]; ok {
		return lang
	}
	return "text"
}

// IsCodeFile checks if the file should be chunked
func IsCodeFile(path string) bool {
	return filefilter.IsCodeFile(path)
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
