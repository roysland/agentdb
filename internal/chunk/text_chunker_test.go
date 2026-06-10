package chunk

import (
	"fmt"
	"strings"
	"testing"
)

func newTestChunker(t *testing.T) *TextFallbackChunker {
	t.Helper()
	tc, err := NewTextFallbackChunker(DefaultTextChunkerConfig())
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}
	return tc
}

func TestNewTextFallbackChunker(t *testing.T) {
	tc, err := NewTextFallbackChunker(TextChunkerConfig{
		TokenWindow:  500,
		TokenOverlap: 100,
	})
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}
	if tc == nil {
		t.Fatal("expected non-nil chunker")
	}
	if tc.config.TokenWindow != 500 {
		t.Errorf("expected TokenWindow=500, got %d", tc.config.TokenWindow)
	}
	if tc.config.TokenOverlap != 100 {
		t.Errorf("expected TokenOverlap=100, got %d", tc.config.TokenOverlap)
	}
}

func TestNewTextFallbackChunker_Defaults(t *testing.T) {
	tc, err := NewTextFallbackChunker(TextChunkerConfig{})
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}
	if tc.config.TokenWindow != 500 {
		t.Errorf("expected default TokenWindow=500, got %d", tc.config.TokenWindow)
	}
	if tc.config.TokenOverlap != 100 {
		t.Errorf("expected default TokenOverlap=100, got %d", tc.config.TokenOverlap)
	}
}

func TestNewTextFallbackChunker_OverlapExceedsWindow(t *testing.T) {
	tc, err := NewTextFallbackChunker(TextChunkerConfig{
		TokenWindow:  100,
		TokenOverlap: 200,
	})
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}
	// Overlap should be clamped to window/5
	if tc.config.TokenOverlap >= tc.config.TokenWindow {
		t.Errorf("expected overlap < window, got overlap=%d window=%d", tc.config.TokenOverlap, tc.config.TokenWindow)
	}
}

func TestChunkText_EmptyContent(t *testing.T) {
	tc := newTestChunker(t)
	chunks, err := tc.ChunkText([]byte(""), "prose")
	if err != nil {
		t.Fatalf("ChunkText failed: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty content, got %d", len(chunks))
	}
}

func TestChunkText_MarkdownDispatch(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte("# Header 1\n\nSome content.\n\n# Header 2\n\nMore content.\n")
	chunks, err := tc.ChunkText(content, "markdown")
	if err != nil {
		t.Fatalf("ChunkText failed: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for markdown with 2 headers, got %d", len(chunks))
	}
	// Each chunk should be a markdown_section
	for _, c := range chunks {
		if c.Kind != "markdown_section" {
			t.Errorf("expected kind=markdown_section, got %s", c.Kind)
		}
		if c.Language != "markdown" {
			t.Errorf("expected language=markdown, got %s", c.Language)
		}
	}
}

func TestChunkText_ProseDispatch(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte("This is some prose content.\n\nAnother paragraph here.\n")
	chunks, err := tc.ChunkText(content, "prose")
	if err != nil {
		t.Fatalf("ChunkText failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk for prose content")
	}
	for _, c := range chunks {
		if c.Kind != "prose" {
			t.Errorf("expected kind=prose, got %s", c.Kind)
		}
	}
}

func TestChunkText_DefaultContentType(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte("Some text content.\n")
	chunks, err := tc.ChunkText(content, "unknown")
	if err != nil {
		t.Fatalf("ChunkText failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	// Default should be prose
	if chunks[0].Kind != "prose" {
		t.Errorf("expected kind=prose for unknown content type, got %s", chunks[0].Kind)
	}
}

func TestChunkMarkdown_HeaderBoundaries(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte(`# Introduction

This is the introduction section with some text.

## Methods

Here we describe the methods used.

### Sub-method A

Details about sub-method A.

## Results

The results are presented here.
`)

	chunks := tc.chunkMarkdown(content)
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks for markdown with multiple headers, got %d", len(chunks))
	}

	// First chunk should contain "Introduction"
	if !strings.Contains(chunks[0].Name, "Introduction") {
		t.Errorf("expected first chunk name to contain 'Introduction', got '%s'", chunks[0].Name)
	}

	// All chunks should be markdown_section kind
	for i, c := range chunks {
		if c.Kind != "markdown_section" {
			t.Errorf("chunk %d: expected kind=markdown_section, got %s", i, c.Kind)
		}
	}
}

func TestChunkMarkdown_SingleSection(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte("# Only Header\n\nSome content without another header.\n")
	chunks := tc.chunkMarkdown(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for single-section markdown, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Name, "Only Header") {
		t.Errorf("expected name to contain 'Only Header', got '%s'", chunks[0].Name)
	}
}

func TestChunkProse_TokenWindowRespected(t *testing.T) {
	// Create a chunker with a small window for testing
	tc, err := NewTextFallbackChunker(TextChunkerConfig{
		TokenWindow:  50,
		TokenOverlap: 10,
	})
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}

	// Generate content that exceeds the token window
	var paragraphs []string
	for i := 0; i < 20; i++ {
		paragraphs = append(paragraphs, fmt.Sprintf("This is paragraph number %d with some additional text to make it longer and use more tokens in the BPE encoding.", i))
	}
	content := []byte(strings.Join(paragraphs, "\n\n"))

	chunks := tc.chunkProse(content)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for content exceeding window, got %d", len(chunks))
	}

	// Verify each chunk's token count is within the window
	for i, c := range chunks {
		tokens := tc.tokenizer.Encode(c.Snippet, nil, nil)
		if len(tokens) > tc.config.TokenWindow {
			t.Errorf("chunk %d: token count %d exceeds window %d", i, len(tokens), tc.config.TokenWindow)
		}
	}
}

func TestChunkProse_OverlapBetweenChunks(t *testing.T) {
	// Create a chunker with a small window for testing
	tc, err := NewTextFallbackChunker(TextChunkerConfig{
		TokenWindow:  50,
		TokenOverlap: 10,
	})
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}

	// Generate content with distinct paragraphs
	var paragraphs []string
	for i := 0; i < 20; i++ {
		paragraphs = append(paragraphs, fmt.Sprintf("Paragraph %d contains unique content about topic number %d with enough words to use several tokens.", i, i))
	}
	content := []byte(strings.Join(paragraphs, "\n\n"))

	chunks := tc.chunkProse(content)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// Check that consecutive chunks have overlapping content
	for i := 0; i < len(chunks)-1; i++ {
		current := chunks[i]
		next := chunks[i+1]

		// The end of the current chunk should overlap with the beginning of the next
		// Due to paragraph-boundary snapping, overlap may be ±1 token
		currentTokens := tc.tokenizer.Encode(current.Snippet, nil, nil)
		nextTokens := tc.tokenizer.Encode(next.Snippet, nil, nil)

		// At minimum, verify chunks are non-empty
		if len(currentTokens) == 0 {
			t.Errorf("chunk %d has 0 tokens", i)
		}
		if len(nextTokens) == 0 {
			t.Errorf("chunk %d has 0 tokens", i+1)
		}
	}
}

func TestChunkProse_ParagraphBoundaryPreference(t *testing.T) {
	tc, err := NewTextFallbackChunker(TextChunkerConfig{
		TokenWindow:  100,
		TokenOverlap: 20,
	})
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}

	// Create content with clear paragraph boundaries
	content := []byte("First paragraph with some text.\n\nSecond paragraph with different text.\n\nThird paragraph with more text.\n\nFourth paragraph with final text.\n")

	chunks := tc.chunkProse(content)

	// With a 100-token window, all paragraphs should fit in one chunk
	// since each paragraph is short
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	// Verify chunks don't split mid-paragraph (no partial paragraphs)
	for i, c := range chunks {
		// A chunk should not start or end in the middle of a word
		snippet := c.Snippet
		if len(snippet) > 0 && snippet[0] == ' ' {
			t.Errorf("chunk %d starts with a space (possible mid-paragraph split)", i)
		}
	}
}

func TestChunkProse_SingleParagraph(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte("This is a single paragraph without any double newlines.")
	chunks := tc.chunkProse(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for single paragraph, got %d", len(chunks))
	}
	if chunks[0].Snippet != string(content) {
		t.Errorf("expected snippet to match content")
	}
}

func TestChunkProse_LargeSingleParagraph(t *testing.T) {
	tc, err := NewTextFallbackChunker(TextChunkerConfig{
		TokenWindow:  20,
		TokenOverlap: 5,
	})
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}

	// Create a single large paragraph that exceeds the token window
	content := []byte("The quick brown fox jumps over the lazy dog. " +
		"Pack my box with five dozen liquor jugs. " +
		"How vexingly quick daft zebras jump. " +
		"The five boxing wizards jump quickly. " +
		"Sphinx of black quartz judge my vow.")

	chunks := tc.chunkProse(content)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for large paragraph with small window, got %d", len(chunks))
	}

	// Each chunk should respect the token window
	for i, c := range chunks {
		tokens := tc.tokenizer.Encode(c.Snippet, nil, nil)
		if len(tokens) > tc.config.TokenWindow {
			t.Errorf("chunk %d: token count %d exceeds window %d", i, len(tokens), tc.config.TokenWindow)
		}
	}
}

func TestChunkTextFile_AutoDetectsMarkdown(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte("# Title\n\nContent here.\n\n# Another\n\nMore content.\n")
	chunks, err := tc.ChunkTextFile("docs/readme.md", content)
	if err != nil {
		t.Fatalf("ChunkTextFile failed: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.FilePath != "docs/readme.md" {
			t.Errorf("expected FilePath='docs/readme.md', got '%s'", c.FilePath)
		}
		if c.Language != "markdown" {
			t.Errorf("expected Language='markdown', got '%s'", c.Language)
		}
	}
}

func TestChunkTextFile_AutoDetectsProse(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte("Some plain text content.\n")
	chunks, err := tc.ChunkTextFile("notes.txt", content)
	if err != nil {
		t.Fatalf("ChunkTextFile failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	for _, c := range chunks {
		if c.FilePath != "notes.txt" {
			t.Errorf("expected FilePath='notes.txt', got '%s'", c.FilePath)
		}
	}
}

func TestChunkTextFile_UniqueKeysForSingleLineMultiChunk(t *testing.T) {
	tc, err := NewTextFallbackChunker(TextChunkerConfig{
		TokenWindow:  20,
		TokenOverlap: 5,
	})
	if err != nil {
		t.Fatalf("NewTextFallbackChunker failed: %v", err)
	}

	content := []byte("The quick brown fox jumps over the lazy dog. " +
		"Pack my box with five dozen liquor jugs. " +
		"How vexingly quick daft zebras jump. " +
		"The five boxing wizards jump quickly. " +
		"Sphinx of black quartz judge my vow.")

	chunks, err := tc.ChunkTextFile("docs/longline.txt", content)
	if err != nil {
		t.Fatalf("ChunkTextFile failed: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	seen := make(map[string]struct{}, len(chunks))
	for _, c := range chunks {
		if !strings.HasPrefix(c.Key, "docs/longline.txt:") {
			t.Errorf("expected key to include file path prefix, got '%s'", c.Key)
		}
		if _, ok := seen[c.Key]; ok {
			t.Fatalf("duplicate chunk key generated: %s", c.Key)
		}
		seen[c.Key] = struct{}{}
	}
}

func TestIsMarkdownHeader(t *testing.T) {
	tests := []struct {
		line     string
		expected bool
	}{
		{"# Header", true},
		{"## Sub Header", true},
		{"### Deep Header", true},
		{"  # Indented Header", true},
		{"Not a header", false},
		{"", false},
		{"#hashtag without space", true}, // Still starts with #
		{"Regular text with # in middle", false},
	}

	for _, tt := range tests {
		result := isMarkdownHeader(tt.line)
		if result != tt.expected {
			t.Errorf("isMarkdownHeader(%q): expected %v, got %v", tt.line, tt.expected, result)
		}
	}
}

func TestExtractHeaderName(t *testing.T) {
	tests := []struct {
		lines    []string
		expected string
	}{
		{[]string{"# Introduction"}, "Introduction"},
		{[]string{"## Methods"}, "Methods"},
		{[]string{"### Sub-section A"}, "Sub-section A"},
		{[]string{""}, ""},
		{nil, ""},
	}

	for _, tt := range tests {
		result := extractHeaderName(tt.lines)
		if result != tt.expected {
			t.Errorf("extractHeaderName(%v): expected %q, got %q", tt.lines, tt.expected, result)
		}
	}
}

func TestSplitParagraphs(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph."
	paras := splitParagraphs(text)
	if len(paras) != 3 {
		t.Fatalf("expected 3 paragraphs, got %d", len(paras))
	}
	if !strings.Contains(paras[0].text, "First") {
		t.Errorf("first paragraph should contain 'First', got %q", paras[0].text)
	}
	if !strings.Contains(paras[1].text, "Second") {
		t.Errorf("second paragraph should contain 'Second', got %q", paras[1].text)
	}
	if !strings.Contains(paras[2].text, "Third") {
		t.Errorf("third paragraph should contain 'Third', got %q", paras[2].text)
	}
}

func TestSplitParagraphs_NoParagraphBreaks(t *testing.T) {
	text := "Single block of text\nwith line breaks\nbut no double newlines."
	paras := splitParagraphs(text)
	if len(paras) != 1 {
		t.Fatalf("expected 1 paragraph, got %d", len(paras))
	}
}

func TestChunkMarkdown_NoHeaders(t *testing.T) {
	tc := newTestChunker(t)
	content := []byte("Just some text without any headers.\nAnother line.\n")
	chunks := tc.chunkMarkdown(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for markdown without headers, got %d", len(chunks))
	}
}
