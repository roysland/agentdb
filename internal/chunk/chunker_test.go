package chunk

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestChunkFile(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	// Create test content
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}

func helper() {
	// Line 9
	// Line 10
	// ... more lines
}
`

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	config := ChunkerConfig{LinesPerChunk: 5}
	chunks, err := ChunkFile(testFile, config)
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Errorf("expected chunks, got none")
	}

	// Check chunk metadata
	chunk := chunks[0]
	if chunk.Language != "go" {
		t.Errorf("expected language 'go', got '%s'", chunk.Language)
	}

	if chunk.StartLine != 1 {
		t.Errorf("expected start line 1, got %d", chunk.StartLine)
	}

	if chunk.FileHash == "" {
		t.Errorf("expected file hash to be set")
	}

	// SHA-256 produces a 64-character hex string
	if len(chunk.FileHash) != 64 {
		t.Errorf("expected file hash to be 64 hex chars (SHA-256), got %d chars: %s", len(chunk.FileHash), chunk.FileHash)
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		file     string
		expected string
	}{
		{"main.go", "go"},
		{"script.py", "python"},
		{"app.js", "javascript"},
		{"config.yaml", "yaml"},
		{"readme.md", "markdown"},
		{"unknown.xyz", "text"},
	}

	for _, tt := range tests {
		result := DetectLanguage(tt.file)
		if result != tt.expected {
			t.Errorf("DetectLanguage(%s): expected %s, got %s", tt.file, tt.expected, result)
		}
	}
}

func TestIsCodeFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"main.go", true},
		{"script.py", true},
		{"app.js", true},
		{"node_modules/package.js", false},
		{".git/config", false},
		{"build/output.o", false},
		{"test.xyz", false},
	}

	for _, tt := range tests {
		result := IsCodeFile(tt.path)
		if result != tt.expected {
			t.Errorf("IsCodeFile(%s): expected %v, got %v", tt.path, tt.expected, result)
		}
	}
}

func TestStreamingHashConsistency(t *testing.T) {
	// Verify that the streaming hash function produces the same result
	// as the in-memory SHA-256 computation.
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	content := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Compute hash via streaming
	streamHash, err := streamingHash(testFile)
	if err != nil {
		t.Fatalf("streamingHash failed: %v", err)
	}

	// Compute hash in-memory for comparison
	inMemSum := sha256.Sum256(content)
	inMemHash := hex.EncodeToString(inMemSum[:])

	if streamHash != inMemHash {
		t.Errorf("streaming hash %s != in-memory hash %s", streamHash, inMemHash)
	}

	if len(streamHash) != 64 {
		t.Errorf("expected 64-char hex string, got %d chars", len(streamHash))
	}
}

func TestChunkDirectory(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go": `package main
func main() {
	// test
}`,
		"utils.go": `package main
func helper() {
	// helper
}`,
		"README.md": `# Test
	This is a test readme`,
	}

	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file %s: %v", name, err)
		}
	}

	config := ChunkerConfig{LinesPerChunk: 2}
	result, err := ChunkDirectory(tmpDir, config)
	if err != nil {
		t.Fatalf("ChunkDirectory failed: %v", err)
	}

	if len(result) == 0 {
		t.Errorf("expected chunks, got none")
	}

	// Verify that we got chunks for both .go files
	goFileCount := 0
	for path := range result {
		if filepath.Ext(path) == ".go" {
			goFileCount++
		}
	}

	if goFileCount != 2 {
		t.Errorf("expected 2 .go files chunked, got %d", goFileCount)
	}
}
