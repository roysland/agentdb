//go:build treesitter

package cmd

import (
	"github.com/roysland/agentdb/internal/parse"
)

// mcpAnalyzeFileResult holds the result of parsing a single file for the analyze flow,
// including resilient parse status metadata.
type mcpAnalyzeFileResult struct {
	FileResult   parse.FileResult
	IndexStatus  string // "complete" | "text_fallback" | "partial"
	StatusReason string
}

// mcpAnalyzeParseFile parses a single file using the ResilientParser when a TreeSitterParser
// is available. Returns the parse result with index status metadata.
// When the treesitter build tag is active, this wraps TreeSitterParser instances with
// ResilientParser for error threshold detection and merge conflict detection.
func mcpAnalyzeParseFile(filePath string, content []byte, parsers []parse.Parser) *mcpAnalyzeFileResult {
	// Find the parser that can handle this file
	var matchedParser parse.Parser
	for _, p := range parsers {
		if p.CanParse(filePath) {
			matchedParser = p
			break
		}
	}

	if matchedParser == nil {
		return nil
	}

	// Check if the matched parser is a TreeSitterParser that we can wrap with ResilientParser
	if tsParser, ok := matchedParser.(*parse.TreeSitterParser); ok {
		resilient := parse.NewResilientParser(tsParser, mcpLogger)
		result, err := resilient.Parse(filePath, content)
		if err != nil {
			return nil
		}
		return &mcpAnalyzeFileResult{
			FileResult:   result.FileResult,
			IndexStatus:  result.IndexStatus,
			StatusReason: result.StatusReason,
		}
	}

	// For non-TreeSitter parsers (e.g., GoParser), check merge conflicts first
	if parse.HasMergeConflicts(content) {
		return &mcpAnalyzeFileResult{
			FileResult:   parse.FileResult{FilePath: filePath, Language: matchedParser.Language()},
			IndexStatus:  "partial",
			StatusReason: "file contains merge conflict markers",
		}
	}

	// Use standard parsing for non-TreeSitter parsers
	fileResult, err := matchedParser.Parse(filePath, content)
	if err != nil {
		return nil
	}

	return &mcpAnalyzeFileResult{
		FileResult:   fileResult,
		IndexStatus:  "complete",
		StatusReason: "",
	}
}
