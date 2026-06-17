package search

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/roysland/agentdb/internal/observe"
	"github.com/roysland/agentdb/internal/store"
)

// LocateIssueResult represents a ranked candidate symbol with impact analysis.
type LocateIssueResult struct {
	Symbol          store.Symbol   `json:"symbol"`
	ConfidenceScore float64        `json:"confidence_score"`
	BlastRadius     BlastRadius    `json:"blast_radius"`
	Chunks          []ChunkSnippet `json:"chunks"`
	CrossRepoLinks  []CrossLink    `json:"cross_repo_links,omitempty"`
}

// ChunkSnippet is a relevant code chunk matching the issue text.
type ChunkSnippet struct {
	FilePath  string `json:"file_path"`
	Name      string `json:"name"`
	Snippet   string `json:"snippet"`
	StartLine int64  `json:"start_line"`
	EndLine   int64  `json:"end_line"`
}

// CrossLink represents a cross-repository symbol connection.
type CrossLink struct {
	TargetCodebaseID int64  `json:"target_codebase_id"`
	TargetSymbol     string `json:"target_symbol"`
	EdgeKind         string `json:"edge_kind"`
}

// LocateIssueConfig holds parameters for the locate operation.
type LocateIssueConfig struct {
	IssueText   string
	CodebaseIDs []int64
	Limit       int
}

// ComputeConfidenceScoreLexicalOnly calculates the confidence score using only
// the normalized BM25 score.
//
// Formula: confidence_score = clamp(normalized_bm25, 0.0, 1.0)
func ComputeConfidenceScoreLexicalOnly(normalizedBM25 float64) float64 {
	return clampFloat64(normalizedBM25, 0.0, 1.0)
}

// NormalizeBM25 normalizes a raw BM25 score against the maximum absolute BM25
// score in the result set.
//
// BM25 scores from SQLite FTS5 are negative (lower = better match), so we
// negate to produce a positive value where higher = better match.
//
// Formula: normalized_bm25 = -bm25_score / max_abs_bm25_score
//
// Returns 0 if maxAbsBM25Score is zero (avoids division by zero).
func NormalizeBM25(bm25Score, maxAbsBM25Score float64) float64 {
	if maxAbsBM25Score == 0 {
		return 0
	}
	return -bm25Score / maxAbsBM25Score
}

// MaxAbsBM25Score returns the maximum absolute BM25 score from a slice of
// FTS5 results. This is used as the denominator for BM25 normalization.
func MaxAbsBM25Score(results []FTS5Result) float64 {
	var maxAbs float64
	for _, r := range results {
		abs := math.Abs(r.BM25Score)
		if abs > maxAbs {
			maxAbs = abs
		}
	}
	return maxAbs
}

// clampFloat64 constrains a value to the range [min, max].
func clampFloat64(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// locateIssueConfidenceBonus applies a small heuristic adjustment so runtime
// entry points outrank string-heavy constants and generated SQL artifacts.
func locateIssueConfidenceBonus(c *candidate) float64 {
	if c == nil {
		return 0
	}

	bonus := 0.0
	switch strings.ToLower(c.kind) {
	case "func", "method":
		bonus += 0.15
	case "type", "interface":
		bonus += 0.06
	case "const", "var", "field", "package":
		bonus -= 0.15
	}

	lowerPath := strings.ToLower(c.filePath)
	switch {
	case strings.Contains(lowerPath, ".sql.go"), strings.Contains(lowerPath, "_sql.go"):
		bonus -= 0.2
	case strings.Contains(lowerPath, "/db/"):
		bonus -= 0.08
	}

	switch {
	case strings.Contains(lowerPath, "/llm/"), strings.Contains(lowerPath, "/handler"), strings.Contains(lowerPath, "/router"), strings.Contains(lowerPath, "/api/"), strings.Contains(lowerPath, "/server/"):
		bonus += 0.08
	case strings.Contains(lowerPath, "/internal/"), strings.Contains(lowerPath, "/cmd/"):
		bonus += 0.03
	}

	return bonus
}

// adjustLocateIssueConfidence combines the base score with a small heuristic bonus.
func adjustLocateIssueConfidence(c *candidate, baseConfidence float64) float64 {
	return clampFloat64(baseConfidence+locateIssueConfidenceBonus(c), 0.0, 1.0)
}

// candidate is an internal type used during merge and scoring.
type candidate struct {
	chunkID    int64
	filePath   string
	name       string
	kind       string
	snippet    string
	startLine  int64
	endLine    int64
	bm25Score  float64
	codebaseID int64
}

// LocateIssue performs lexical FTS5 search and returns ranked candidates enriched
// with blast radius and matching chunks.
//
// Returns (results, warning, error).
func LocateIssue(ctx context.Context, db *sql.DB, cfg LocateIssueConfig, logger *observe.Logger) ([]LocateIssueResult, string, error) {
	if cfg.IssueText == "" {
		return nil, "", fmt.Errorf("locate_issue: issue_text is required")
	}
	if len(cfg.CodebaseIDs) == 0 {
		return nil, "", fmt.Errorf("locate_issue: at least one codebase_id is required")
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 10
	}

	logMsg := func(level, op, msg string) {
		if logger == nil {
			return
		}
		logger.Log(observe.LogEntry{Level: level, Operation: op, Status: msg})
	}

	// Execute FTS5 search across all codebase IDs in parallel.
	fts, err := NewFTS5Search(db, logger)
	if err != nil {
		return nil, "", fmt.Errorf("locate_issue: create fts5 search: %w", err)
	}

	// Collect FTS5 candidates from all codebases.
	type ftsResult struct {
		codebaseID int64
		results    []FTS5Result
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var ftsResults []ftsResult
	var ftsErr error

	// Candidate limit: fetch more than needed for scoring
	candidateLimit := cfg.Limit * 5
	if candidateLimit < 50 {
		candidateLimit = 50
	}

	for _, cbID := range cfg.CodebaseIDs {
		wg.Add(1)
		go func(codebaseID int64) {
			defer wg.Done()
			results, err := fts.SearchLexical(ctx, cfg.IssueText, codebaseID, candidateLimit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if ftsErr == nil {
					ftsErr = fmt.Errorf("fts5 search for codebase %d: %w", codebaseID, err)
				}
				logMsg("warn", "locate_issue", fmt.Sprintf("FTS5 search failed for codebase %d: %v", codebaseID, err))
				return
			}
			if len(results) > 0 {
				ftsResults = append(ftsResults, ftsResult{codebaseID: codebaseID, results: results})
			}
		}(cbID)
	}

	wg.Wait()

	// If FTS5 failed entirely, return error.
	if ftsErr != nil && len(ftsResults) == 0 {
		return nil, "", ftsErr
	}

	// Merge and deduplicate candidates.
	// Key: "codebaseID:filePath:name" to deduplicate.
	type candidateKey struct {
		codebaseID int64
		filePath   string
		name       string
	}
	candidateMap := make(map[candidateKey]*candidate)

	// Compute max absolute BM25 score across all FTS results for normalization.
	var allFTSResults []FTS5Result
	for _, fr := range ftsResults {
		allFTSResults = append(allFTSResults, fr.results...)
	}
	maxAbsBM25 := MaxAbsBM25Score(allFTSResults)

	// Add FTS5 candidates.
	for _, fr := range ftsResults {
		for _, r := range fr.results {
			key := candidateKey{codebaseID: fr.codebaseID, filePath: r.FilePath, name: r.Name}
			if existing, ok := candidateMap[key]; ok {
				// Keep the better BM25 score (more negative = better)
				if r.BM25Score < existing.bm25Score {
					existing.bm25Score = r.BM25Score
				}
			} else {
				candidateMap[key] = &candidate{
					chunkID:    r.ChunkID,
					filePath:   r.FilePath,
					name:       r.Name,
					kind:       r.Kind,
					snippet:    r.Snippet,
					startLine:  r.StartLine,
					endLine:    r.EndLine,
					bm25Score:  r.BM25Score,
					codebaseID: fr.codebaseID,
				}
			}
		}
	}

	// Compute confidence scores.
	type scoredCandidate struct {
		candidate  *candidate
		confidence float64
	}
	scored := make([]scoredCandidate, 0, len(candidateMap))
	for _, c := range candidateMap {
		normalizedBM25 := NormalizeBM25(c.bm25Score, maxAbsBM25)
		confidence := ComputeConfidenceScoreLexicalOnly(normalizedBM25)
		confidence = adjustLocateIssueConfidence(c, confidence)
		scored = append(scored, scoredCandidate{candidate: c, confidence: confidence})
	}

	// Sort by confidence descending.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].confidence > scored[j].confidence
	})

	// Filter out candidates below 0.1 threshold.
	var filtered []scoredCandidate
	for _, sc := range scored {
		if sc.confidence >= 0.1 {
			filtered = append(filtered, sc)
		}
	}

	// Return empty list with message when no candidates exceed threshold.
	if len(filtered) == 0 {
		return []LocateIssueResult{}, "", nil
	}

	// Apply limit.
	if len(filtered) > cfg.Limit {
		filtered = filtered[:cfg.Limit]
	}

	// Enrich top candidates with blast radius and matching chunks.
	edgeRepo := store.NewEdgeRepo(db)
	multiCodebase := len(cfg.CodebaseIDs) > 1

	results := make([]LocateIssueResult, 0, len(filtered))
	for _, sc := range filtered {
		c := sc.candidate

		// Build a symbol for blast radius computation.
		sym := store.Symbol{
			CodebaseID:    c.codebaseID,
			FilePath:      c.filePath,
			Name:          c.name,
			QualifiedName: c.name, // Use name as qualified name for lookup
			Kind:          c.kind,
			StartLine:     c.startLine,
			EndLine:       c.endLine,
		}

		// Compute blast radius.
		br, err := ComputeBlastRadius(ctx, edgeRepo, c.codebaseID, sym)
		if err != nil {
			logMsg("warn", "locate_issue", fmt.Sprintf("blast radius failed for %s: %v", c.name, err))
			br = BlastRadius{Callers: []string{}, Callees: []string{}, Dependents: []string{}}
		}

		// Get matching chunks for this candidate's file.
		chunks := getMatchingChunks(ctx, db, c.codebaseID, c.filePath, c.name)

		// Get cross-repo links when workspace scope spans multiple codebases.
		var crossLinks []CrossLink
		if multiCodebase {
			crossLinks = getCrossRepoLinks(ctx, db, c.codebaseID, c.name, cfg.CodebaseIDs)
		}

		result := LocateIssueResult{
			Symbol:          sym,
			ConfidenceScore: sc.confidence,
			BlastRadius:     br,
			Chunks:          chunks,
			CrossRepoLinks:  crossLinks,
		}
		results = append(results, result)
	}

	return results, "", nil
}

// getMatchingChunks retrieves code chunks for a given file and symbol name.
func getMatchingChunks(ctx context.Context, db *sql.DB, codebaseID int64, filePath, name string) []ChunkSnippet {
	query := `
		SELECT file_path, name, snippet, start_line, end_line
		FROM chunks
		WHERE codebase_id = ? AND file_path = ? AND name = ?
		ORDER BY start_line
		LIMIT 5
	`
	rows, err := db.QueryContext(ctx, query, codebaseID, filePath, name)
	if err != nil {
		return []ChunkSnippet{}
	}
	defer rows.Close()

	var chunks []ChunkSnippet
	for rows.Next() {
		var cs ChunkSnippet
		if err := rows.Scan(&cs.FilePath, &cs.Name, &cs.Snippet, &cs.StartLine, &cs.EndLine); err != nil {
			continue
		}
		chunks = append(chunks, cs)
	}
	if chunks == nil {
		chunks = []ChunkSnippet{}
	}
	return chunks
}

// getCrossRepoLinks finds edges that connect to symbols in sibling codebases.
func getCrossRepoLinks(ctx context.Context, db *sql.DB, codebaseID int64, symbolName string, allCodebaseIDs []int64) []CrossLink {
	// Find edges from this symbol that point to other codebases.
	var links []CrossLink

	// Query edges with target_codebase_id set (resolved cross-repo edges).
	query := `
		SELECT target_codebase_id, to_ref, edge_kind
		FROM edges
		WHERE codebase_id = ? AND from_ref = ? AND target_codebase_id IS NOT NULL
		LIMIT 10
	`
	rows, err := db.QueryContext(ctx, query, codebaseID, symbolName)
	if err != nil {
		return links
	}
	defer rows.Close()

	for rows.Next() {
		var cl CrossLink
		if err := rows.Scan(&cl.TargetCodebaseID, &cl.TargetSymbol, &cl.EdgeKind); err != nil {
			continue
		}
		links = append(links, cl)
	}

	// Also check for edges from sibling codebases that reference this symbol.
	for _, siblingID := range allCodebaseIDs {
		if siblingID == codebaseID {
			continue
		}
		inboundQuery := `
			SELECT codebase_id, from_ref, edge_kind
			FROM edges
			WHERE codebase_id = ? AND to_ref = ? AND edge_kind = 'calls'
			LIMIT 5
		`
		inRows, err := db.QueryContext(ctx, inboundQuery, siblingID, symbolName)
		if err != nil {
			continue
		}
		for inRows.Next() {
			var targetCB int64
			var fromRef, edgeKind string
			if err := inRows.Scan(&targetCB, &fromRef, &edgeKind); err != nil {
				continue
			}
			links = append(links, CrossLink{
				TargetCodebaseID: targetCB,
				TargetSymbol:     fromRef,
				EdgeKind:         edgeKind,
			})
		}
		inRows.Close()
	}

	if links == nil {
		links = []CrossLink{}
	}
	return links
}

