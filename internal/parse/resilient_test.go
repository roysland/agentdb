//go:build treesitter

package parse

import (
	"bytes"
	"os"
	"testing"

	"github.com/roysland/agentdb/internal/observe"
)

func TestHasMergeConflicts(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "no conflict markers",
			content: "package main\n\nfunc main() {}\n",
			want:    false,
		},
		{
			name: "all three markers present",
			content: `package main
<<<<<<< HEAD
func foo() {}
=======
func bar() {}
>>>>>>> feature-branch
`,
			want: true,
		},
		{
			name:    "only opening marker",
			content: "<<<<<<< HEAD\nsome code\n",
			want:    false,
		},
		{
			name:    "only separator",
			content: "=======\nsome code\n",
			want:    false,
		},
		{
			name:    "only closing marker",
			content: ">>>>>>> branch\nsome code\n",
			want:    false,
		},
		{
			name:    "opening and separator only",
			content: "<<<<<<< HEAD\ncode\n=======\nother\n",
			want:    false,
		},
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasMergeConflicts([]byte(tt.content))
			if got != tt.want {
				t.Errorf("HasMergeConflicts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResilientParser_Parse_MergeConflicts(t *testing.T) {
	logger := observe.NewLogger(observe.LevelDebug, os.Stderr)
	inner := NewPythonParser()
	rp := NewResilientParser(inner, logger)

	content := []byte(`<<<<<<< HEAD
def foo():
    pass
=======
def bar():
    pass
>>>>>>> feature-branch
`)

	result, err := rp.Parse("test.py", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IndexStatus != "partial" {
		t.Errorf("IndexStatus = %q, want %q", result.IndexStatus, "partial")
	}
	if result.StatusReason == "" {
		t.Error("StatusReason should not be empty for merge conflicts")
	}
	if len(result.Symbols) != 0 {
		t.Errorf("expected zero symbols for merge conflict file, got %d", len(result.Symbols))
	}
}

func TestResilientParser_Parse_ValidFile(t *testing.T) {
	logger := observe.NewLogger(observe.LevelDebug, os.Stderr)
	inner := NewPythonParser()
	rp := NewResilientParser(inner, logger)

	content := []byte(`def hello():
    print("hello world")

def goodbye():
    print("goodbye world")
`)

	result, err := rp.Parse("test.py", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IndexStatus != "complete" {
		t.Errorf("IndexStatus = %q, want %q", result.IndexStatus, "complete")
	}
	if result.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0", result.ErrorCount)
	}
	if result.TotalNodes == 0 {
		t.Error("TotalNodes should be > 0 for valid file")
	}
	if result.ErrorRatio != 0 {
		t.Errorf("ErrorRatio = %f, want 0", result.ErrorRatio)
	}
	if len(result.Symbols) == 0 {
		t.Error("expected symbols from valid Python file")
	}
}

func TestResilientParser_Parse_HighErrorRatio(t *testing.T) {
	logger := observe.NewLogger(observe.LevelDebug, os.Stderr)
	inner := NewPythonParser()
	// Use a very low threshold to trigger fallback easily
	rp := NewResilientParserWithThreshold(inner, 0.01, logger)

	// Content with syntax errors that tree-sitter will flag
	content := []byte(`def @@@invalid:
    !!!broken syntax here
    %%%more garbage
    $$$not python at all
    &&&still broken
`)

	result, err := rp.Parse("broken.py", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With a very low threshold, this should trigger text_fallback
	if result.ErrorCount == 0 {
		t.Skip("tree-sitter did not produce ERROR nodes for this content")
	}

	if result.IndexStatus != "text_fallback" {
		t.Errorf("IndexStatus = %q, want %q (errorRatio=%.3f, threshold=0.01)",
			result.IndexStatus, "text_fallback", result.ErrorRatio)
	}
	if len(result.Symbols) != 0 {
		t.Errorf("expected zero symbols when threshold breached, got %d", len(result.Symbols))
	}
	if result.StatusReason == "" {
		t.Error("StatusReason should not be empty when threshold breached")
	}
}

func TestResilientParser_Parse_ErrorRanges(t *testing.T) {
	logger := observe.NewLogger(observe.LevelDebug, os.Stderr)
	inner := NewPythonParser()
	// Use a high threshold so we still get symbols but also error metadata
	rp := NewResilientParserWithThreshold(inner, 0.99, logger)

	content := []byte(`def valid_func():
    pass

def @@@broken():
    pass

def another_valid():
    pass
`)

	result, err := rp.Parse("mixed.py", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ErrorCount == 0 {
		t.Skip("tree-sitter did not produce ERROR nodes for this content")
	}

	if len(result.ErrorRanges) == 0 {
		t.Error("expected error ranges to be populated when errors exist")
	}

	// Verify error ranges have valid line numbers
	for _, r := range result.ErrorRanges {
		if r.Start < 1 {
			t.Errorf("error range start %d should be >= 1", r.Start)
		}
		if r.End < r.Start {
			t.Errorf("error range end %d should be >= start %d", r.End, r.Start)
		}
	}
}

func TestResilientParser_HealthCheck(t *testing.T) {
	logger := observe.NewLogger(observe.LevelDebug, os.Stderr)
	inner := NewPythonParser()
	rp := NewResilientParser(inner, logger)

	health := rp.HealthCheck()
	if len(health) == 0 {
		t.Fatal("HealthCheck returned empty map")
	}

	status, ok := health["python"]
	if !ok {
		t.Fatal("HealthCheck missing 'python' entry")
	}
	if !status {
		t.Error("python grammar should be loaded")
	}
}

func TestResilientParser_CanParse(t *testing.T) {
	logger := observe.NewLogger(observe.LevelDebug, os.Stderr)
	inner := NewPythonParser()
	rp := NewResilientParser(inner, logger)

	if !rp.CanParse("test.py") {
		t.Error("should be able to parse .py files")
	}
	if rp.CanParse("test.go") {
		t.Error("should not be able to parse .go files with python parser")
	}
}

func TestResilientParser_Language(t *testing.T) {
	logger := observe.NewLogger(observe.LevelDebug, os.Stderr)
	inner := NewPythonParser()
	rp := NewResilientParser(inner, logger)

	if rp.Language() != "python" {
		t.Errorf("Language() = %q, want %q", rp.Language(), "python")
	}
}

func TestResilientParser_Parse_EmptyContent(t *testing.T) {
	logger := observe.NewLogger(observe.LevelDebug, &bytes.Buffer{})
	inner := NewPythonParser()
	rp := NewResilientParser(inner, logger)

	result, err := rp.Parse("empty.py", []byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty file should parse without errors
	if result.IndexStatus != "complete" {
		t.Errorf("IndexStatus = %q, want %q for empty file", result.IndexStatus, "complete")
	}
}

func TestResilientParser_Parse_NilInner(t *testing.T) {
	rp := NewResilientParser(nil, nil)

	result, err := rp.Parse("nil.py", []byte("print('hi')"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IndexStatus != "partial" {
		t.Fatalf("IndexStatus = %q, want %q", result.IndexStatus, "partial")
	}
	if result.StatusReason == "" {
		t.Fatal("StatusReason should not be empty for nil parser")
	}
}

func TestResilientParser_NilLogger(t *testing.T) {
	inner := NewPythonParser()
	rp := NewResilientParser(inner, nil)

	content := []byte(`def hello():
    pass
`)

	// Should not panic with nil logger
	result, err := rp.Parse("test.py", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IndexStatus != "complete" {
		t.Errorf("IndexStatus = %q, want %q", result.IndexStatus, "complete")
	}
}
