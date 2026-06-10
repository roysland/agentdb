//go:build !treesitter

package cmd

import (
	"github.com/roysland/agentdb/internal/parse"
)

// mcpAnalyzeFileResult holds the result of parsing a single file for the analyze flow,
// including parse status metadata.
type mcpAnalyzeFileResult struct {
	FileResult   parse.FileResult
	IndexStatus  string // "complete" | "text_fallback" | "partial"
	StatusReason string
}

// mcpAnalyzeParseFile parses a single file using the standard parser.
// Without the treesitter build tag, this uses the basic parser without resilient wrapping.
// Merge conflict detection is still performed before parsing.
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

	// Check for merge conflicts before parsing
	if parse.HasMergeConflicts(content) {
		return &mcpAnalyzeFileResult{
			FileResult:   parse.FileResult{FilePath: filePath, Language: matchedParser.Language()},
			IndexStatus:  "partial",
			StatusReason: "file contains merge conflict markers",
		}
	}

	// Use standard parsing
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
