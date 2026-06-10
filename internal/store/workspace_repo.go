package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Workspace represents a logical grouping of codebases.
type Workspace struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

// WorkspaceRepo provides CRUD operations for workspaces and their members.
type WorkspaceRepo struct{ db *sql.DB }

// NewWorkspaceRepo creates a new WorkspaceRepo.
func NewWorkspaceRepo(db *sql.DB) *WorkspaceRepo { return &WorkspaceRepo{db: db} }

// Create inserts a new workspace with the given name and returns its ID.
func (r *WorkspaceRepo) Create(ctx context.Context, name string) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO workspaces (name, created_at) VALUES (?, ?)`,
		name, time.Now().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("create workspace: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("create workspace last insert id: %w", err)
	}
	return id, nil
}

// AddMember associates a codebase with a workspace.
func (r *WorkspaceRepo) AddMember(ctx context.Context, workspaceID, codebaseID int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workspace_members (workspace_id, codebase_id) VALUES (?, ?)`,
		workspaceID, codebaseID,
	)
	if err != nil {
		return fmt.Errorf("add workspace member: %w", err)
	}
	return nil
}

// RemoveMember removes a codebase from a workspace.
func (r *WorkspaceRepo) RemoveMember(ctx context.Context, workspaceID, codebaseID int64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM workspace_members WHERE workspace_id = ? AND codebase_id = ?`,
		workspaceID, codebaseID,
	)
	if err != nil {
		return fmt.Errorf("remove workspace member: %w", err)
	}
	return nil
}

// List returns all workspaces ordered by name.
func (r *WorkspaceRepo) List(ctx context.Context) ([]Workspace, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, created_at FROM workspaces ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	out := make([]Workspace, 0)
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetMembers returns all codebases that belong to the given workspace.
func (r *WorkspaceRepo) GetMembers(ctx context.Context, workspaceID int64) ([]Codebase, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.id, c.root_path, c.name, c.indexed_at
		FROM codebases c
		INNER JOIN workspace_members wm ON wm.codebase_id = c.id
		WHERE wm.workspace_id = ?
		ORDER BY c.name`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("get workspace members: %w", err)
	}
	defer rows.Close()

	out := make([]Codebase, 0)
	for rows.Next() {
		var cb Codebase
		if err := rows.Scan(&cb.ID, &cb.RootPath, &cb.Name, &cb.IndexedAt); err != nil {
			return nil, err
		}
		out = append(out, cb)
	}
	return out, rows.Err()
}

// GetByName returns a workspace by its unique name.
func (r *WorkspaceRepo) GetByName(ctx context.Context, name string) (Workspace, error) {
	var w Workspace
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, created_at FROM workspaces WHERE name = ?`, name,
	).Scan(&w.ID, &w.Name, &w.CreatedAt)
	if err != nil {
		return Workspace{}, fmt.Errorf("get workspace by name: %w", err)
	}
	return w, nil
}

// GetMemberIDs returns the codebase IDs that belong to the given workspace.
func (r *WorkspaceRepo) GetMemberIDs(ctx context.Context, workspaceID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT codebase_id FROM workspace_members WHERE workspace_id = ? ORDER BY codebase_id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("get workspace member ids: %w", err)
	}
	defer rows.Close()

	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetWorkspacesForCodebase returns all workspaces that contain the given codebase.
func (r *WorkspaceRepo) GetWorkspacesForCodebase(ctx context.Context, codebaseID int64) ([]Workspace, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT w.id, w.name, w.created_at
		FROM workspaces w
		INNER JOIN workspace_members wm ON wm.workspace_id = w.id
		WHERE wm.codebase_id = ?
		ORDER BY w.name`,
		codebaseID,
	)
	if err != nil {
		return nil, fmt.Errorf("get workspaces for codebase: %w", err)
	}
	defer rows.Close()

	out := make([]Workspace, 0)
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
