package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	dbgen "github.com/roysland/agentdb/data/gen"
)

type Codebase struct {
	ID        int64  `json:"id"`
	RootPath  string `json:"root_path"`
	Name      string `json:"name"`
	IndexedAt int64  `json:"indexed_at"`
}

type CatalogRepo struct {
	db *sql.DB
	q  *dbgen.Queries
}

func NewCatalogRepo(db *sql.DB) *CatalogRepo {
	return &CatalogRepo{db: db, q: dbgen.New(db)}
}

func (r *CatalogRepo) RegisterCodebase(ctx context.Context, rootPath, name string) (int64, error) {
	id, err := r.q.RegisterCodebase(ctx, dbgen.RegisterCodebaseParams{
		RootPath:  rootPath,
		Name:      name,
		IndexedAt: time.Now().Unix(),
	})
	if err != nil {
		return 0, fmt.Errorf("register codebase: %w", err)
	}
	return id, nil
}

// GetByID returns a single codebase by its ID.
func (r *CatalogRepo) GetByID(ctx context.Context, id int64) (Codebase, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, root_path, name, indexed_at FROM codebases WHERE id = ?`, id,
	)
	var cb Codebase
	if err := row.Scan(&cb.ID, &cb.RootPath, &cb.Name, &cb.IndexedAt); err != nil {
		return Codebase{}, fmt.Errorf("get codebase by id: %w", err)
	}
	return cb, nil
}

func (r *CatalogRepo) ListCodebases(ctx context.Context) ([]Codebase, error) {
	rows, err := r.q.ListCodebases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list codebases: %w", err)
	}

	out := make([]Codebase, 0, len(rows))
	for _, row := range rows {
		out = append(out, Codebase{
			ID:        row.ID,
			RootPath:  row.RootPath,
			Name:      row.Name,
			IndexedAt: row.IndexedAt,
		})
	}

	return out, nil
}
