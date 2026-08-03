package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// FindByNameMulti searches symbols across multiple codebases.
// It behaves like FindByName but uses codebase_id IN (...) to fan out.
func (r *SymbolRepo) FindByNameMulti(ctx context.Context, codebaseIDs []int64, name string) ([]Symbol, error) {
	if len(codebaseIDs) == 0 {
		return []Symbol{}, nil
	}

	placeholders := buildPlaceholders(len(codebaseIDs))
	args := idsToArgs(codebaseIDs)
	args = append(args, "%"+name+"%", "%"+name+"%", name, name)

	query := fmt.Sprintf(`
		SELECT id, codebase_id, file_path, language, kind, name, qualified_name,
		       receiver, signature, doc_comment, visibility, body_snippet,
		       start_line, end_line, file_hash, indexed_at
		FROM symbols
		WHERE codebase_id IN (%s)
		  AND (name LIKE ? OR qualified_name LIKE ?)
		ORDER BY
			CASE WHEN name = ? THEN 0
			     WHEN qualified_name = ? THEN 1
			     ELSE 2 END,
			name
		LIMIT 50`, placeholders)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find symbols multi: %w", err)
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// FindByNameFuzzyMulti searches symbols with typo-tolerant fuzzy matching across multiple codebases.
func (r *SymbolRepo) FindByNameFuzzyMulti(ctx context.Context, codebaseIDs []int64, name string) ([]Symbol, error) {
	if len(codebaseIDs) == 0 {
		return []Symbol{}, nil
	}

	threshold := fuzzyThreshold(name)
	minLen := len(name) - threshold - 1
	if minLen < 1 {
		minLen = 1
	}
	maxLen := len(name) + threshold + 1

	placeholders := buildPlaceholders(len(codebaseIDs))
	args := idsToArgs(codebaseIDs)
	args = append(args, minLen, maxLen, minLen, maxLen, fuzzyCandidateLimit)

	query := fmt.Sprintf(`
		SELECT id, codebase_id, file_path, language, kind, name, qualified_name,
		       receiver, signature, doc_comment, visibility, body_snippet,
		       start_line, end_line, file_hash, indexed_at
		FROM symbols
		WHERE codebase_id IN (%s)
		  AND (LENGTH(name) BETWEEN ? AND ? OR LENGTH(qualified_name) BETWEEN ? AND ?)
		LIMIT ?`, placeholders)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find symbols fuzzy multi: %w", err)
	}
	defer rows.Close()
	candidates, err := scanSymbols(rows)
	if err != nil {
		return nil, err
	}

	lowerName := strings.ToLower(name)

	type scoredSymbol struct {
		symbol   Symbol
		distance int
	}
	var scored []scoredSymbol
	for _, s := range candidates {
		dName := levenshtein(lowerName, strings.ToLower(s.Name))
		dQual := levenshtein(lowerName, strings.ToLower(s.QualifiedName))
		d := dName
		if dQual < d {
			d = dQual
		}
		if d <= threshold {
			scored = append(scored, scoredSymbol{symbol: s, distance: d})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].distance < scored[j].distance
	})
	if len(scored) > fuzzyResultLimit {
		scored = scored[:fuzzyResultLimit]
	}

	out := make([]Symbol, len(scored))
	for i, sc := range scored {
		out[i] = sc.symbol
	}
	return out, nil
}

// GetCallersMulti returns callers from any of the given codebases.
// It behaves like GetCallers but uses codebase_id IN (...) to fan out.
func (r *EdgeRepo) GetCallersMulti(ctx context.Context, codebaseIDs []int64, targetRef string) ([]Edge, error) {
	if len(codebaseIDs) == 0 {
		return []Edge{}, nil
	}

	placeholders := buildPlaceholders(len(codebaseIDs))
	args := idsToArgs(codebaseIDs)
	args = append(args, targetRef, "%."+targetRef)

	query := fmt.Sprintf(`
		SELECT id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved
		FROM edges
		WHERE codebase_id IN (%s) AND edge_kind = 'calls'
		  AND (to_ref = ? OR to_ref LIKE ?)
		ORDER BY from_ref, line`, placeholders)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get callers multi: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// FindUsagesMulti returns usage edges from any of the given codebases.
// It behaves like FindUsages but uses codebase_id IN (...) to fan out.
func (r *EdgeRepo) FindUsagesMulti(ctx context.Context, codebaseIDs []int64, targetRef string) ([]Edge, error) {
	if len(codebaseIDs) == 0 {
		return []Edge{}, nil
	}

	placeholders := buildPlaceholders(len(codebaseIDs))
	args := idsToArgs(codebaseIDs)
	args = append(args, targetRef, "%."+targetRef)

	query := fmt.Sprintf(`
		SELECT id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved
		FROM edges
		WHERE codebase_id IN (%s) AND (to_ref = ? OR to_ref LIKE ?)
		ORDER BY edge_kind, from_ref, line`, placeholders)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find usages multi: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// buildPlaceholders generates a comma-separated string of "?" placeholders.
// For n=3, it returns "?,?,?".
func buildPlaceholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

// idsToArgs converts a slice of int64 to a slice of interface{} for use as query args.
func idsToArgs(ids []int64) []interface{} {
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
