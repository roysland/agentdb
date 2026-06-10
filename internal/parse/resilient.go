//go:build treesitter

package parse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/roysland/agentdb/internal/observe"
)

// ParseResult extends FileResult with error metadata for resilient parsing.
type ParseResult struct {
	FileResult
	ErrorCount   int         // Number of ERROR/MISSING nodes
	TotalNodes   int         // Total AST nodes
	ErrorRatio   float64     // ErrorCount / TotalNodes
	ErrorRanges  []LineRange // Line ranges containing errors
	IndexStatus  string      // "complete" | "text_fallback" | "partial"
	StatusReason string      // Human-readable reason for non-complete status
}

// LineRange represents a contiguous range of lines in a source file.
type LineRange struct {
	Start int64
	End   int64
}

// ResilientParser wraps TreeSitterParser with error threshold and graceful degradation.
type ResilientParser struct {
	inner          *TreeSitterParser
	langName       string
	errorThreshold float64 // Default: 0.15 (15%)
	logger         *observe.Logger
}

// NewResilientParser creates a ResilientParser wrapping the given TreeSitterParser.
// The default error threshold is 0.15 (15%).
func NewResilientParser(inner *TreeSitterParser, logger *observe.Logger) *ResilientParser {
	langName := "unknown"
	if inner != nil {
		langName = inner.langName
	}
	return &ResilientParser{
		inner:          inner,
		langName:       langName,
		errorThreshold: 0.15,
		logger:         logger,
	}
}

// NewResilientParserWithThreshold creates a ResilientParser with a custom error threshold.
func NewResilientParserWithThreshold(inner *TreeSitterParser, threshold float64, logger *observe.Logger) *ResilientParser {
	langName := "unknown"
	if inner != nil {
		langName = inner.langName
	}
	return &ResilientParser{
		inner:          inner,
		langName:       langName,
		errorThreshold: threshold,
		logger:         logger,
	}
}

// Language delegates to the inner parser.
func (rp *ResilientParser) Language() string {
	if rp.inner != nil {
		return rp.inner.Language()
	}
	return rp.langName
}

// CanParse delegates to the inner parser.
func (rp *ResilientParser) CanParse(filePath string) bool {
	if rp.inner == nil {
		return false
	}
	return rp.inner.CanParse(filePath)
}

// Parse parses a file with error threshold enforcement.
// If error ratio exceeds threshold, returns empty symbols with text_fallback status.
// If merge conflict markers are detected, returns partial status without AST extraction.
// Recovers from tree-sitter panics gracefully.
func (rp *ResilientParser) Parse(filePath string, content []byte) (result ParseResult, err error) {
	if rp.inner == nil {
		return ParseResult{
			FileResult: FileResult{
				FilePath: filePath,
				Language: rp.langName,
				LOC:      bytes.Count(content, []byte("\n")) + 1,
			},
			IndexStatus:  "partial",
			StatusReason: "parser not initialized",
		}, nil
	}

	if rp.inner.lang == nil || rp.inner.parseFn == nil {
		return ParseResult{
			FileResult: FileResult{
				FilePath: filePath,
				Language: rp.langName,
				LOC:      bytes.Count(content, []byte("\n")) + 1,
			},
			IndexStatus:  "partial",
			StatusReason: "tree-sitter parser is not fully initialized",
		}, nil
	}

	// Recover from tree-sitter panics
	defer func() {
		if r := recover(); r != nil {
			rp.log("error", "tree-sitter panic recovered", filePath, fmt.Sprintf("%v", r))
			result = ParseResult{
				FileResult: FileResult{
					FilePath: filePath,
					Language: rp.langName,
					LOC:      bytes.Count(content, []byte("\n")) + 1,
				},
				IndexStatus:  "partial",
				StatusReason: fmt.Sprintf("tree-sitter panic: %v", r),
			}
			err = nil
		}
	}()

	// Check for merge conflict markers before parsing
	if HasMergeConflicts(content) {
		rp.log("warn", "merge conflict markers detected", filePath, "")
		return ParseResult{
			FileResult: FileResult{
				FilePath: filePath,
				Language: rp.langName,
				LOC:      bytes.Count(content, []byte("\n")) + 1,
			},
			IndexStatus:  "partial",
			StatusReason: "file contains merge conflict markers",
		}, nil
	}

	// Parse with tree-sitter
	tsParser := sitter.NewParser()
	tsParser.SetLanguage(rp.inner.lang)
	tree, parseErr := tsParser.ParseCtx(context.Background(), nil, content)
	if parseErr != nil {
		return ParseResult{
			FileResult: FileResult{
				FilePath: filePath,
				Language: rp.langName,
				LOC:      bytes.Count(content, []byte("\n")) + 1,
			},
			IndexStatus:  "partial",
			StatusReason: fmt.Sprintf("tree-sitter parse error: %v", parseErr),
		}, nil
	}
	if tree == nil {
		rp.log("error", "tree-sitter returned nil tree", filePath, "")
		return ParseResult{
			FileResult: FileResult{
				FilePath: filePath,
				Language: rp.langName,
				LOC:      bytes.Count(content, []byte("\n")) + 1,
			},
			IndexStatus:  "partial",
			StatusReason: "tree-sitter returned nil tree",
		}, nil
	}
	defer tree.Close()

	root := tree.RootNode()

	// Count ERROR/MISSING nodes and total nodes
	errorCount, totalNodes, errorRanges := countErrors(root)

	// Compute error ratio
	var errorRatio float64
	if totalNodes > 0 {
		errorRatio = float64(errorCount) / float64(totalNodes)
	}

	// Log errors if any
	if errorCount > 0 {
		rp.log("warn", fmt.Sprintf("parse errors detected: %d/%d nodes (%.1f%%)",
			errorCount, totalNodes, errorRatio*100), filePath, "")
	}

	// Check error threshold
	if errorRatio > rp.errorThreshold {
		rp.log("warn", fmt.Sprintf("error threshold breached (%.1f%% > %.1f%%), falling back to text",
			errorRatio*100, rp.errorThreshold*100), filePath, "")
		return ParseResult{
			FileResult: FileResult{
				FilePath: filePath,
				Language: rp.inner.langName,
				LOC:      bytes.Count(content, []byte("\n")) + 1,
			},
			ErrorCount:   errorCount,
			TotalNodes:   totalNodes,
			ErrorRatio:   errorRatio,
			ErrorRanges:  errorRanges,
			IndexStatus:  "text_fallback",
			StatusReason: fmt.Sprintf("error ratio %.1f%% exceeds threshold %.1f%%", errorRatio*100, rp.errorThreshold*100),
		}, nil
	}

	// Below threshold — extract symbols normally
	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])
	moduleName := tsModuleName(filePath)

	symbols, imports, edges := rp.inner.parseFn(content, root, filePath, moduleName, hash)

	// Warn if zero symbols from non-empty file
	if len(symbols) == 0 && len(content) > 0 {
		rp.log("warn", "zero symbols extracted from non-empty file", filePath, "")
	}

	indexStatus := "complete"
	statusReason := ""
	if errorCount > 0 {
		indexStatus = "complete"
		statusReason = fmt.Sprintf("%d error nodes in %d total nodes (%.1f%%)", errorCount, totalNodes, errorRatio*100)
	}

	return ParseResult{
		FileResult: FileResult{
			FilePath:    filePath,
			Language:    rp.langName,
			PackageName: moduleName,
			LOC:         bytes.Count(content, []byte("\n")) + 1,
			FileHash:    hash,
			Imports:     imports,
			Symbols:     symbols,
			Edges:       edges,
		},
		ErrorCount:   errorCount,
		TotalNodes:   totalNodes,
		ErrorRatio:   errorRatio,
		ErrorRanges:  errorRanges,
		IndexStatus:  indexStatus,
		StatusReason: statusReason,
	}, nil
}

// HasMergeConflicts is defined in merge_conflicts.go (no build tag) so it's
// available in all builds.

// HealthCheck reports which grammars loaded successfully.
// Returns a map of language name to load status (true = loaded, false = failed).
func (rp *ResilientParser) HealthCheck() map[string]bool {
	result := make(map[string]bool)
	result[rp.inner.langName] = rp.inner.lang != nil
	return result
}

// countErrors walks the AST and counts ERROR/MISSING nodes, collecting their line ranges.
func countErrors(node *sitter.Node) (errorCount, totalNodes int, errorRanges []LineRange) {
	if node == nil {
		return 0, 0, nil
	}

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		totalNodes++

		nodeType := n.Type()
		if nodeType == "ERROR" || nodeType == "MISSING" || strings.HasPrefix(nodeType, "MISSING") {
			errorCount++
			errorRanges = append(errorRanges, LineRange{
				Start: int64(n.StartPoint().Row + 1),
				End:   int64(n.EndPoint().Row + 1),
			})
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}

	walk(node)
	return errorCount, totalNodes, errorRanges
}

// log emits a structured log entry if a logger is configured.
func (rp *ResilientParser) log(level, message, filePath, detail string) {
	if rp.logger == nil {
		return
	}
	entry := observe.LogEntry{
		Level:     level,
		Operation: "resilient_parse",
		Status:    message,
	}
	if filePath != "" {
		entry.Operation = fmt.Sprintf("resilient_parse:%s", filePath)
	}
	if detail != "" {
		entry.Error = detail
	}
	rp.logger.Log(entry)
}
