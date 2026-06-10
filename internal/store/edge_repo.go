package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Edge mirrors the edges table.
type Edge struct {
	ID               int64  `json:"id"`
	CodebaseID       int64  `json:"codebase_id"`
	FromKind         string `json:"from_kind"`
	FromRef          string `json:"from_ref"`
	ToKind           string `json:"to_kind"`
	ToRef            string `json:"to_ref"`
	EdgeKind         string `json:"edge_kind"`
	Line             int64  `json:"line"`
	Resolved         bool   `json:"resolved"`
	TargetCodebaseID *int64 `json:"target_codebase_id,omitempty"`
}

type EdgeData struct {
	FromKind string
	FromRef  string
	ToKind   string
	ToRef    string
	EdgeKind string
	Line     int64
	Resolved bool
}

type EdgeRepo struct{ db *sql.DB }

func NewEdgeRepo(db *sql.DB) *EdgeRepo { return &EdgeRepo{db: db} }

func (r *EdgeRepo) Create(ctx context.Context, codebaseID int64, d EdgeData) error {
	resolved := 0
	if d.Resolved {
		resolved = 1
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO edges (codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved)
		VALUES (?,?,?,?,?,?,?,?)`,
		codebaseID, d.FromKind, d.FromRef, d.ToKind, d.ToRef, d.EdgeKind, d.Line, resolved,
	)
	return err
}

func (r *EdgeRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM edges WHERE codebase_id = ?`, codebaseID)
	return err
}

// DeleteByFile removes all edges originating from a specific file within a codebase.
// It matches edges where from_ref starts with the file path (file-level edges)
// or where from_ref matches a symbol in that file.
func (r *EdgeRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM edges WHERE codebase_id = ? AND (from_ref = ? OR from_ref LIKE ?)`,
		codebaseID, filePath, filePath+":%",
	)
	return err
}

// GetCallers returns all symbols that call the given target.
// targetRef can be a simple name or qualified name; both are matched.
func (r *EdgeRepo) GetCallers(ctx context.Context, codebaseID int64, targetRef string) ([]Edge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved
		FROM edges
		WHERE codebase_id = ? AND edge_kind = 'calls'
		  AND (to_ref = ? OR to_ref LIKE ?)
		ORDER BY from_ref, line`,
		codebaseID, targetRef, "%."+targetRef,
	)
	if err != nil {
		return nil, fmt.Errorf("get callers: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetCallees returns all symbols called by the given source ref.
func (r *EdgeRepo) GetCallees(ctx context.Context, codebaseID int64, fromRef string) ([]Edge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved
		FROM edges
		WHERE codebase_id = ? AND edge_kind = 'calls' AND from_ref = ?
		ORDER BY line`,
		codebaseID, fromRef,
	)
	if err != nil {
		return nil, fmt.Errorf("get callees: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetImports returns all import edges for a file.
func (r *EdgeRepo) GetImports(ctx context.Context, codebaseID int64, filePath string) ([]Edge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved
		FROM edges
		WHERE codebase_id = ? AND edge_kind = 'imports' AND from_ref = ?
		ORDER BY line`,
		codebaseID, filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("get imports: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetDependents returns all files that import the given file/package path.
func (r *EdgeRepo) GetDependents(ctx context.Context, codebaseID int64, targetRef string) ([]Edge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved
		FROM edges
		WHERE codebase_id = ? AND edge_kind = 'imports' AND to_ref = ?
		ORDER BY from_ref`,
		codebaseID, targetRef,
	)
	if err != nil {
		return nil, fmt.Errorf("get dependents: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// FindUsages returns all edges referencing targetRef (partial suffix match supported).
func (r *EdgeRepo) FindUsages(ctx context.Context, codebaseID int64, targetRef string) ([]Edge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved
		FROM edges
		WHERE codebase_id = ? AND (to_ref = ? OR to_ref LIKE ?)
		ORDER BY edge_kind, from_ref, line`,
		codebaseID, targetRef, "%."+targetRef,
	)
	if err != nil {
		return nil, fmt.Errorf("find usages: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetUnresolvedImports returns all import edges that are not yet resolved for a codebase.
func (r *EdgeRepo) GetUnresolvedImports(ctx context.Context, codebaseID int64) ([]Edge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved
		FROM edges
		WHERE codebase_id = ? AND edge_kind = 'imports' AND resolved = 0
		ORDER BY from_ref, line`,
		codebaseID,
	)
	if err != nil {
		return nil, fmt.Errorf("get unresolved imports: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// ResolveCrossRepoEdge updates an edge to mark it as resolved with a target codebase ID.
func (r *EdgeRepo) ResolveCrossRepoEdge(ctx context.Context, edgeID int64, targetCodebaseID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE edges SET resolved = 1, target_codebase_id = ? WHERE id = ?`,
		targetCodebaseID, edgeID,
	)
	if err != nil {
		return fmt.Errorf("resolve cross-repo edge: %w", err)
	}
	return nil
}

func scanEdges(rows *sql.Rows) ([]Edge, error) {
	out := make([]Edge, 0)
	for rows.Next() {
		var e Edge
		var resolved int
		if err := rows.Scan(
			&e.ID, &e.CodebaseID, &e.FromKind, &e.FromRef,
			&e.ToKind, &e.ToRef, &e.EdgeKind, &e.Line, &resolved,
		); err != nil {
			return nil, err
		}
		e.Resolved = resolved != 0
		out = append(out, e)
	}
	return out, rows.Err()
}
