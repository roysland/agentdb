package chunk

import (
	"fmt"
	"path/filepath"
	"strings"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// TextChunkerConfig configures the text fallback chunking strategy.
type TextChunkerConfig struct {
	TokenWindow  int // Target tokens per chunk (default: 500)
	TokenOverlap int // Overlap tokens between chunks (default: 100)
}

// DefaultTextChunkerConfig returns the default text chunker configuration.
func DefaultTextChunkerConfig() TextChunkerConfig {
	return TextChunkerConfig{
		TokenWindow:  500,
		TokenOverlap: 100,
	}
}

// TextFallbackChunker handles non-code content with content-type-aware splitting.
type TextFallbackChunker struct {
	config    TextChunkerConfig
	tokenizer *tiktoken.Tiktoken // BPE tokenizer for exact token counting
}

// NewTextFallbackChunker creates a text chunker with BPE tokenizer.
func NewTextFallbackChunker(config TextChunkerConfig) (*TextFallbackChunker, error) {
	if config.TokenWindow <= 0 {
		config.TokenWindow = 500
	}
	if config.TokenOverlap <= 0 {
		config.TokenOverlap = 100
	}
	if config.TokenOverlap >= config.TokenWindow {
		config.TokenOverlap = config.TokenWindow / 5
	}

	enc, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return nil, fmt.Errorf("initialize BPE tokenizer: %w", err)
	}

	return &TextFallbackChunker{
		config:    config,
		tokenizer: enc,
	}, nil
}

// ChunkText splits text content using the appropriate strategy based on content type.
func (tc *TextFallbackChunker) ChunkText(content []byte, contentType string) ([]Chunk, error) {
	if len(content) == 0 {
		return nil, nil
	}

	switch contentType {
	case "markdown":
		return tc.chunkMarkdown(content), nil
	default:
		return tc.chunkProse(content), nil
	}
}

// chunkMarkdown splits at header boundaries (lines starting with `#`).
func (tc *TextFallbackChunker) chunkMarkdown(content []byte) []Chunk {
	text := string(content)
	lines := strings.Split(text, "\n")

	// Find header boundary indices (line indices where headers start)
	type section struct {
		startLine int
		lines     []string
	}

	var sections []section
	var currentLines []string
	currentStart := 0

	for i, line := range lines {
		if isMarkdownHeader(line) && len(currentLines) > 0 {
			// Start a new section at this header
			sections = append(sections, section{startLine: currentStart, lines: currentLines})
			currentLines = nil
			currentStart = i
		}
		currentLines = append(currentLines, line)
	}
	// Append the last section
	if len(currentLines) > 0 {
		sections = append(sections, section{startLine: currentStart, lines: currentLines})
	}

	// Now merge sections that are too small or split sections that are too large
	var chunks []Chunk
	for _, sec := range sections {
		sectionText := strings.Join(sec.lines, "\n")
		tokens := tc.tokenizer.Encode(sectionText, nil, nil)

		if len(tokens) <= tc.config.TokenWindow {
			// Section fits in one chunk
			startLine := int64(sec.startLine + 1)
			endLine := startLine + int64(len(sec.lines)-1)
			chunks = append(chunks, Chunk{
				Kind:      "markdown_section",
				Name:      extractHeaderName(sec.lines),
				Snippet:   sectionText,
				StartLine: startLine,
				EndLine:   endLine,
			})
		} else {
			// Section is too large — use prose windowing within it
			subChunks := tc.chunkProseWithOffset([]byte(sectionText), sec.startLine)
			for i := range subChunks {
				subChunks[i].Kind = "markdown_section"
				if subChunks[i].Name == "" {
					subChunks[i].Name = extractHeaderName(sec.lines)
				}
			}
			chunks = append(chunks, subChunks...)
		}
	}

	// Set language for all chunks (keys are set by ChunkTextFile with file path context).
	for i := range chunks {
		chunks[i].Language = "markdown"
	}

	return chunks
}

// chunkProse splits using BPE token windowing with paragraph-boundary preference.
func (tc *TextFallbackChunker) chunkProse(content []byte) []Chunk {
	return tc.chunkProseWithOffset(content, 0)
}

// chunkProseWithOffset splits prose content with a line offset for correct line numbering.
func (tc *TextFallbackChunker) chunkProseWithOffset(content []byte, lineOffset int) []Chunk {
	text := string(content)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	// Split into paragraphs (double newline boundaries)
	paragraphs := splitParagraphs(text)
	if len(paragraphs) == 0 {
		return nil
	}

	var chunks []Chunk
	window := tc.config.TokenWindow
	overlap := tc.config.TokenOverlap

	// Track current chunk state
	var currentParas []paragraphInfo
	var currentTokenCount int

	flushChunk := func() {
		if len(currentParas) == 0 {
			return
		}

		// Build snippet from paragraphs
		var snippetParts []string
		for _, p := range currentParas {
			snippetParts = append(snippetParts, p.text)
		}
		snippet := strings.Join(snippetParts, "\n\n")

		startLine := int64(currentParas[0].startLine + lineOffset + 1)
		endLine := int64(currentParas[len(currentParas)-1].endLine + lineOffset + 1)

		chunks = append(chunks, Chunk{
			Kind:      "prose",
			Name:      fmt.Sprintf("prose:%d-%d", startLine, endLine),
			Snippet:   snippet,
			StartLine: startLine,
			EndLine:   endLine,
			Language:  "text",
		})
	}

	for _, para := range paragraphs {
		paraTokens := tc.tokenizer.Encode(para.text, nil, nil)
		paraTokenCount := len(paraTokens)

		if currentTokenCount+paraTokenCount <= window {
			// Paragraph fits in current chunk
			currentParas = append(currentParas, para)
			currentTokenCount += paraTokenCount
		} else if currentTokenCount == 0 {
			// Single paragraph exceeds window — must split it by tokens
			subChunks := tc.splitLargeParagraph(para, lineOffset)
			chunks = append(chunks, subChunks...)
		} else {
			// Flush current chunk and start overlap
			flushChunk()

			// Compute overlap: take paragraphs from the end of the current chunk
			// that fit within the overlap token budget
			overlapParas, overlapTokens := tc.computeOverlap(currentParas, overlap)

			// Start new chunk with overlap paragraphs
			currentParas = make([]paragraphInfo, len(overlapParas))
			copy(currentParas, overlapParas)
			currentTokenCount = overlapTokens

			// Add the new paragraph
			if currentTokenCount+paraTokenCount <= window {
				currentParas = append(currentParas, para)
				currentTokenCount += paraTokenCount
			} else if paraTokenCount > window {
				// Large paragraph after overlap — flush overlap and split paragraph
				flushChunk()
				currentParas = nil
				currentTokenCount = 0
				subChunks := tc.splitLargeParagraph(para, lineOffset)
				chunks = append(chunks, subChunks...)
			} else {
				// Paragraph doesn't fit with overlap — flush overlap, start fresh
				flushChunk()
				currentParas = []paragraphInfo{para}
				currentTokenCount = paraTokenCount
			}
		}
	}

	// Flush remaining
	flushChunk()

	return chunks
}

// splitLargeParagraph splits a single paragraph that exceeds the token window
// into multiple chunks using token windowing.
func (tc *TextFallbackChunker) splitLargeParagraph(para paragraphInfo, lineOffset int) []Chunk {
	tokens := tc.tokenizer.Encode(para.text, nil, nil)
	window := tc.config.TokenWindow
	overlap := tc.config.TokenOverlap

	var chunks []Chunk
	lines := strings.Split(para.text, "\n")

	start := 0
	for start < len(tokens) {
		end := start + window
		if end > len(tokens) {
			end = len(tokens)
		}

		// Decode the token slice back to text
		snippet := tc.tokenizer.Decode(tokens[start:end])

		// Calculate line numbers within the paragraph
		// Count newlines in the text before this snippet to determine start line
		textBefore := tc.tokenizer.Decode(tokens[:start])
		startLineInPara := strings.Count(textBefore, "\n")
		endLineInPara := startLineInPara + strings.Count(snippet, "\n")

		// Clamp to actual line count
		if endLineInPara >= len(lines) {
			endLineInPara = len(lines) - 1
		}

		startLine := int64(para.startLine + startLineInPara + lineOffset + 1)
		endLine := int64(para.startLine + endLineInPara + lineOffset + 1)

		chunks = append(chunks, Chunk{
			Kind:      "prose",
			Name:      fmt.Sprintf("prose:%d-%d", startLine, endLine),
			Snippet:   snippet,
			StartLine: startLine,
			EndLine:   endLine,
			Language:  "text",
		})

		if end >= len(tokens) {
			break
		}

		// Advance with overlap
		start = end - overlap
		if start <= 0 {
			start = end
		}
	}

	return chunks
}

// computeOverlap selects paragraphs from the end of the current chunk
// that fit within the overlap token budget.
func (tc *TextFallbackChunker) computeOverlap(paras []paragraphInfo, overlapBudget int) ([]paragraphInfo, int) {
	if len(paras) == 0 || overlapBudget <= 0 {
		return nil, 0
	}

	var result []paragraphInfo
	totalTokens := 0

	// Walk backwards through paragraphs
	for i := len(paras) - 1; i >= 0; i-- {
		paraTokens := tc.tokenizer.Encode(paras[i].text, nil, nil)
		if totalTokens+len(paraTokens) > overlapBudget {
			break
		}
		result = append([]paragraphInfo{paras[i]}, result...)
		totalTokens += len(paraTokens)
	}

	return result, totalTokens
}

// paragraphInfo holds a paragraph's text and its line position in the original content.
type paragraphInfo struct {
	text      string
	startLine int // 0-indexed line number in the original content
	endLine   int // 0-indexed line number (inclusive)
}

// splitParagraphs splits text at double-newline boundaries and tracks line positions.
// A paragraph boundary is defined as one or more consecutive empty lines.
func splitParagraphs(text string) []paragraphInfo {
	lines := strings.Split(text, "\n")
	var paragraphs []paragraphInfo
	var currentLines []string
	currentStart := 0
	inBlank := false

	for i, line := range lines {
		isEmpty := strings.TrimSpace(line) == ""

		if isEmpty {
			if !inBlank && len(currentLines) > 0 {
				// First empty line after content — flush current paragraph
				paraText := strings.Join(currentLines, "\n")
				if strings.TrimSpace(paraText) != "" {
					paragraphs = append(paragraphs, paragraphInfo{
						text:      paraText,
						startLine: currentStart,
						endLine:   i - 1,
					})
				}
				currentLines = nil
			}
			inBlank = true
		} else {
			if inBlank || currentLines == nil {
				currentStart = i
			}
			inBlank = false
			currentLines = append(currentLines, line)
		}
	}

	// Flush remaining
	if len(currentLines) > 0 {
		paraText := strings.Join(currentLines, "\n")
		if strings.TrimSpace(paraText) != "" {
			endLine := currentStart + len(currentLines) - 1
			paragraphs = append(paragraphs, paragraphInfo{
				text:      paraText,
				startLine: currentStart,
				endLine:   endLine,
			})
		}
	}

	return paragraphs
}

// isMarkdownHeader checks if a line is a markdown header (starts with #).
func isMarkdownHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#")
}

// extractHeaderName extracts the header text from the first line of a section.
func extractHeaderName(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	line := strings.TrimSpace(lines[0])
	// Strip leading # characters and space
	line = strings.TrimLeft(line, "#")
	line = strings.TrimSpace(line)
	if line == "" && len(lines) > 1 {
		// Use file base name or first non-empty line
		for _, l := range lines[1:] {
			if trimmed := strings.TrimSpace(l); trimmed != "" {
				if len(trimmed) > 50 {
					return trimmed[:50]
				}
				return trimmed
			}
		}
	}
	if len(line) > 80 {
		return line[:80]
	}
	return line
}

// ChunkTextFile is a convenience method that chunks a file given its path and content.
// It auto-detects content type based on file extension.
func (tc *TextFallbackChunker) ChunkTextFile(filePath string, content []byte) ([]Chunk, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	contentType := "prose"
	if ext == ".md" || ext == ".markdown" {
		contentType = "markdown"
	}

	chunks, err := tc.ChunkText(content, contentType)
	if err != nil {
		return nil, err
	}

	// Set file path on all chunks
	for i := range chunks {
		chunks[i].FilePath = filePath
		// Include file path + per-file chunk ordinal to guarantee unique keys in DB.
		chunks[i].Key = fmt.Sprintf("%s:%d:%d-%d", filePath, i, chunks[i].StartLine, chunks[i].EndLine)
	}

	return chunks, nil
}
