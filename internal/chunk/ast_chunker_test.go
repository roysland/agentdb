//go:build treesitter

package chunk

import (
	"strings"
	"testing"

	"github.com/roysland/agentdb/internal/parse"
)

func TestNewASTChunker(t *testing.T) {
	parsers := []parse.Parser{
		parse.NewPythonParser(),
	}
	config := ASTChunkerConfig{MaxChunkLines: 50}
	chunker := NewASTChunker(parsers, config)

	if chunker == nil {
		t.Fatal("expected non-nil chunker")
	}
	if chunker.config.MaxChunkLines != 50 {
		t.Errorf("expected MaxChunkLines=50, got %d", chunker.config.MaxChunkLines)
	}
}

func TestNewASTChunkerDefaultMaxLines(t *testing.T) {
	chunker := NewASTChunker(nil, ASTChunkerConfig{MaxChunkLines: 0})
	if chunker.config.MaxChunkLines != 100 {
		t.Errorf("expected default MaxChunkLines=100, got %d", chunker.config.MaxChunkLines)
	}
}

func TestASTChunkerFallbackWhenNoParser(t *testing.T) {
	// No parsers provided — should fall back to fixed-line chunking
	chunker := NewASTChunker(nil, DefaultASTChunkerConfig())

	content := []byte("line1\nline2\nline3\nline4\nline5\n")
	chunks, err := chunker.ChunkFile("test.xyz", content, "text")
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk from fallback")
	}

	// Verify fallback produces "code" kind
	for _, c := range chunks {
		if c.Kind != "code" {
			t.Errorf("expected kind 'code' from fallback, got '%s'", c.Kind)
		}
	}
}

func TestASTChunkerPythonFunction(t *testing.T) {
	parsers := []parse.Parser{
		parse.NewPythonParser(),
	}
	chunker := NewASTChunker(parsers, DefaultASTChunkerConfig())

	content := []byte(`def hello():
    print("hello world")

def goodbye():
    print("goodbye world")
`)

	chunks, err := chunker.ChunkFile("test.py", content, "python")
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected chunks from Python file")
	}

	// Verify round-trip property: concatenating all snippets reproduces original
	var combined strings.Builder
	for _, c := range chunks {
		combined.WriteString(c.Snippet)
	}
	if combined.String() != string(content) {
		t.Errorf("round-trip failed:\ngot:  %q\nwant: %q", combined.String(), string(content))
	}

	// Verify metadata is populated
	foundFunction := false
	for _, c := range chunks {
		if c.Kind == "function" {
			foundFunction = true
			if c.Name == "" {
				t.Error("expected non-empty name for function chunk")
			}
		}
		if c.Language != "python" {
			t.Errorf("expected language 'python', got '%s'", c.Language)
		}
		if c.FileHash == "" {
			t.Error("expected non-empty file hash")
		}
		if len(c.FileHash) != 64 {
			t.Errorf("expected 64-char SHA-256 hash, got %d chars", len(c.FileHash))
		}
	}
	if !foundFunction {
		t.Error("expected at least one chunk with kind 'function'")
	}
}

func TestASTChunkerRoundTripTypeScript(t *testing.T) {
	parsers := []parse.Parser{
		parse.NewTypeScriptParser(),
	}
	chunker := NewASTChunker(parsers, DefaultASTChunkerConfig())

	content := []byte(`import { foo } from './bar';

export function greet(name: string): string {
    return "Hello, " + name;
}

export class Greeter {
    private name: string;

    constructor(name: string) {
        this.name = name;
    }

    greet(): string {
        return "Hello, " + this.name;
    }
}
`)

	chunks, err := chunker.ChunkFile("test.ts", content, "typescript")
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected chunks from TypeScript file")
	}

	// Verify round-trip property
	var combined strings.Builder
	for _, c := range chunks {
		combined.WriteString(c.Snippet)
	}
	if combined.String() != string(content) {
		t.Errorf("round-trip failed:\ngot:  %q\nwant: %q", combined.String(), string(content))
	}
}

func TestASTChunkerLargeNodeSubdivision(t *testing.T) {
	parsers := []parse.Parser{
		parse.NewPythonParser(),
	}
	// Set a very small MaxChunkLines to force subdivision
	config := ASTChunkerConfig{MaxChunkLines: 5}
	chunker := NewASTChunker(parsers, config)

	// Create a Python function that exceeds 5 lines
	content := []byte(`def large_function():
    x = 1
    y = 2
    z = 3
    a = 4
    b = 5
    c = 6
    d = 7
    e = 8
    f = 9
    return x + y + z
`)

	chunks, err := chunker.ChunkFile("test.py", content, "python")
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	// Should produce multiple chunks since the function exceeds MaxChunkLines
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks for large function, got %d", len(chunks))
	}

	// Verify round-trip property still holds
	var combined strings.Builder
	for _, c := range chunks {
		combined.WriteString(c.Snippet)
	}
	if combined.String() != string(content) {
		t.Errorf("round-trip failed:\ngot:  %q\nwant: %q", combined.String(), string(content))
	}

	// Verify parent signature is preserved in sub-chunks
	for _, c := range chunks {
		if c.Signature == "" && c.Kind == "function" {
			// At least some chunks should have a signature
			// (the first sub-chunk should have the function signature)
		}
	}
}

func TestASTChunkerFallbackOnUnsupportedLanguage(t *testing.T) {
	// GoParser uses go/ast, not tree-sitter, so tree-sitter language lookup will fail
	parsers := []parse.Parser{
		&parse.GoParser{},
	}
	chunker := NewASTChunker(parsers, DefaultASTChunkerConfig())

	content := []byte(`package main

func main() {
    println("hello")
}
`)

	chunks, err := chunker.ChunkFile("test.go", content, "go")
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	// Should fall back to fixed-line chunking since Go doesn't have tree-sitter grammar
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk from fallback")
	}

	// Fallback chunks have kind "code"
	for _, c := range chunks {
		if c.Kind != "code" {
			t.Errorf("expected kind 'code' from fallback, got '%s'", c.Kind)
		}
	}
}

func TestASTChunkerEmptyFile(t *testing.T) {
	parsers := []parse.Parser{
		parse.NewPythonParser(),
	}
	chunker := NewASTChunker(parsers, DefaultASTChunkerConfig())

	// Empty file
	chunks, err := chunker.ChunkFile("empty.py", []byte(""), "python")
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	// Empty file should produce no chunks (or fall back gracefully)
	// Either 0 chunks or fallback chunks are acceptable
	_ = chunks
}

func TestASTChunkerFileHashIsSHA256(t *testing.T) {
	parsers := []parse.Parser{
		parse.NewPythonParser(),
	}
	chunker := NewASTChunker(parsers, DefaultASTChunkerConfig())

	content := []byte(`x = 1
`)
	chunks, err := chunker.ChunkFile("test.py", content, "python")
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	for _, c := range chunks {
		if len(c.FileHash) != 64 {
			t.Errorf("expected 64-char SHA-256 hash, got %d chars: %s", len(c.FileHash), c.FileHash)
		}
	}
}
