package search

import (
	"context"

	"github.com/roysland/agentdb/internal/store"
)

// BlastRadius contains the impact analysis for a symbol.
type BlastRadius struct {
	Callers    []string `json:"callers"`    // qualified names that call this symbol
	Callees    []string `json:"callees"`    // qualified names this symbol calls
	Dependents []string `json:"dependents"` // files that import this symbol's file
}

// ComputeBlastRadius queries the edge repo for callers, callees, and
// file-level dependents of the given symbol.
func ComputeBlastRadius(ctx context.Context, edgeRepo *store.EdgeRepo, codebaseID int64, sym store.Symbol) (BlastRadius, error) {
	br := BlastRadius{
		Callers:    []string{},
		Callees:    []string{},
		Dependents: []string{},
	}

	// Query callers: edges where edge_kind='calls' and to_ref matches the symbol's qualified name
	callerEdges, err := edgeRepo.GetCallers(ctx, codebaseID, sym.QualifiedName)
	if err != nil {
		return br, err
	}
	for _, e := range callerEdges {
		br.Callers = append(br.Callers, e.FromRef)
	}

	// Query callees: edges where edge_kind='calls' and from_ref matches the symbol's qualified name
	calleeEdges, err := edgeRepo.GetCallees(ctx, codebaseID, sym.QualifiedName)
	if err != nil {
		return br, err
	}
	for _, e := range calleeEdges {
		br.Callees = append(br.Callees, e.ToRef)
	}

	// Query dependents: edges where edge_kind='imports' and to_ref matches the symbol's file path
	dependentEdges, err := edgeRepo.GetDependents(ctx, codebaseID, sym.FilePath)
	if err != nil {
		return br, err
	}
	for _, e := range dependentEdges {
		br.Dependents = append(br.Dependents, e.FromRef)
	}

	return br, nil
}
