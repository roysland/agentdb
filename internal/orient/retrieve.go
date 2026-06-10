package orient

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// OrientationDoc represents a retrieved orientation document chunk.
type OrientationDoc struct {
	FilePath string  `json:"file_path"`
	Content  string  `json:"content"`
	DocType  DocType `json:"doc_type"`
	Priority int     `json:"-"` // internal sorting priority
}

// RetrieveConfig holds parameters for orientation document retrieval.
type RetrieveConfig struct {
	CodebaseIDs []int64
	Config      Config // loaded from codebase or defaults; required
}

// Retrieve fetches orientation documents for the given codebases, sorted by priority.
// Uses patterns and priorities from the provided Config.
// It builds a single SQL query from the config patterns, classifies results at the
// application layer, sorts by priority, and enforces per-type MaxItems caps.
func Retrieve(ctx context.Context, db *sql.DB, cfg RetrieveConfig) ([]OrientationDoc, error) {
	if len(cfg.CodebaseIDs) == 0 {
		return nil, fmt.Errorf("orient: at least one codebase ID is required")
	}
	if cfg.Config == nil {
		return nil, fmt.Errorf("orient: config is required")
	}

	whereClause, args := buildQuery(cfg.Config, cfg.CodebaseIDs)
	if whereClause == "" {
		return nil, nil
	}

	query := `SELECT c.file_path, c.snippet, c.start_line, c.end_line
FROM chunks c
WHERE ` + whereClause + `
ORDER BY c.file_path, c.start_line`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("orient: query chunks: %w", err)
	}
	defer rows.Close()

	// Aggregate chunks by file path (concatenate snippets in order)
	type chunkRow struct {
		filePath  string
		snippet   string
		startLine int64
		endLine   int64
	}

	var rawChunks []chunkRow
	for rows.Next() {
		var r chunkRow
		if err := rows.Scan(&r.filePath, &r.snippet, &r.startLine, &r.endLine); err != nil {
			return nil, fmt.Errorf("orient: scan row: %w", err)
		}
		rawChunks = append(rawChunks, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orient: iterate rows: %w", err)
	}

	// Group chunks by file path and concatenate content
	fileContents := make(map[string]string)
	fileOrder := make([]string, 0)
	for _, chunk := range rawChunks {
		if _, exists := fileContents[chunk.filePath]; !exists {
			fileOrder = append(fileOrder, chunk.filePath)
		}
		if existing := fileContents[chunk.filePath]; existing != "" {
			fileContents[chunk.filePath] = existing + "\n" + chunk.snippet
		} else {
			fileContents[chunk.filePath] = chunk.snippet
		}
	}

	// Classify each file and build OrientationDoc results
	var docs []OrientationDoc
	for _, fp := range fileOrder {
		result := Classify(fp, cfg.Config)
		if result.DocType == "" {
			continue
		}
		docs = append(docs, OrientationDoc{
			FilePath: fp,
			Content:  fileContents[fp],
			DocType:  result.DocType,
			Priority: result.Priority,
		})
	}

	// Sort by priority (lower number = higher priority)
	sort.SliceStable(docs, func(i, j int) bool {
		return docs[i].Priority < docs[j].Priority
	})

	// Enforce per-type MaxItems caps
	docs = enforceMaxItems(docs, cfg.Config)

	return docs, nil
}

// buildQuery dynamically constructs a SQL WHERE clause from the loaded Config.
// Each pattern in each PatternSet becomes a LIKE condition, ORed together.
// Returns the complete WHERE clause string and the query arguments.
func buildQuery(config Config, codebaseIDs []int64) (whereClause string, args []interface{}) {
	if len(codebaseIDs) == 0 || len(config) == 0 {
		return "", nil
	}

	// Build codebase_id IN (?) clause
	placeholders := make([]string, len(codebaseIDs))
	for i, id := range codebaseIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	codebaseClause := "c.codebase_id IN (" + strings.Join(placeholders, ", ") + ")"

	// Build LIKE conditions from all patterns across all doc types
	var likeConditions []string
	for _, ps := range config {
		for _, pattern := range ps.Patterns {
			likePattern := globToSQL(pattern)
			likeConditions = append(likeConditions, "c.file_path LIKE ? ESCAPE '\\'")
			args = append(args, likePattern)
		}
	}

	if len(likeConditions) == 0 {
		return "", nil
	}

	patternClause := "(" + strings.Join(likeConditions, " OR ") + ")"
	whereClause = codebaseClause + " AND " + patternClause

	return whereClause, args
}

// globToSQL converts a glob-style pattern to a SQL LIKE pattern.
// Supports * as wildcard (maps to %). The pattern matches the basename of the file_path,
// so we prepend % to match any directory prefix, and use the basename pattern with LIKE.
// Since we match against the full file_path, we use %/pattern OR pattern (for root files).
func globToSQL(pattern string) string {
	// Escape SQL LIKE special characters in the pattern first
	escaped := strings.ReplaceAll(pattern, "%", "\\%")
	escaped = strings.ReplaceAll(escaped, "_", "\\_")

	// Convert glob * to SQL %
	sqlPattern := strings.ReplaceAll(escaped, "*", "%")

	// Always prepend % to match any directory prefix. This is intentionally broad —
	// the Classify function handles precise matching at the application layer.
	// Even if the pattern already starts with % (from a leading *), we still prepend
	// another % so the query matches paths like "subdir/file.md" as well.
	sqlPattern = "%" + sqlPattern

	return sqlPattern
}

// enforceMaxItems applies per-type MaxItems caps from the config.
func enforceMaxItems(docs []OrientationDoc, config Config) []OrientationDoc {
	typeCounts := make(map[DocType]int)
	result := make([]OrientationDoc, 0, len(docs))

	for _, doc := range docs {
		ps, exists := config[doc.DocType]
		if !exists {
			result = append(result, doc)
			continue
		}

		typeCounts[doc.DocType]++
		if ps.MaxItems > 0 && typeCounts[doc.DocType] > ps.MaxItems {
			continue // skip, cap reached
		}
		result = append(result, doc)
	}

	return result
}

// globMatchBasename checks if a filename matches a glob pattern.
// This is a helper used internally for validation.
func globMatchBasename(baseName, pattern string) bool {
	matched, err := filepath.Match(pattern, baseName)
	return err == nil && matched
}
