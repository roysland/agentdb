package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	dbgen "github.com/roysland/agentdb/data/gen"
)

type Memory struct {
	ID             string `json:"id"`
	Content        string `json:"content"`
	Category       string `json:"category"`
	WorkspaceID    *int64 `json:"workspace_id,omitempty"`
	CodebaseID     *int64 `json:"codebase_id,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	LastRetrieved  *int64 `json:"last_retrieved,omitempty"`
	RetrievalCount int64  `json:"retrieval_count"`
	SourceTask     string `json:"source_task,omitempty"`
}

type ListMemoryParams struct {
	Category    string
	Limit       int
	WorkspaceID int64
	CodebaseID  int64
}

type UpdateMemoryParams struct {
	Content     *string
	Category    *string
	SourceTask  *string
	WorkspaceID *int64
	CodebaseID  *int64
}

type MemoryRepo struct {
	q *dbgen.Queries
}

func NewMemoryRepo(db *sql.DB) *MemoryRepo {
	return &MemoryRepo{q: dbgen.New(db)}
}

func (r *MemoryRepo) Create(ctx context.Context, m Memory) error {
	err := r.q.CreateMemory(ctx, dbgen.CreateMemoryParams{
		ID:          m.ID,
		Content:     m.Content,
		Category:    m.Category,
		WorkspaceID: nullInt64(m.WorkspaceID),
		CodebaseID:  nullInt64(m.CodebaseID),
		CreatedAt:   m.CreatedAt,
		SourceTask:  nullString(m.SourceTask),
	})
	if err != nil {
		return fmt.Errorf("create memory: %w", err)
	}
	return nil
}

func (r *MemoryRepo) GetByID(ctx context.Context, id string) (Memory, error) {
	row, err := r.q.GetMemoryByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Memory{}, fmt.Errorf("memory not found: %s", id)
		}
		return Memory{}, fmt.Errorf("get memory: %w", err)
	}

	return fromDBMemory(row), nil
}

func (r *MemoryRepo) List(ctx context.Context, params ListMemoryParams) ([]Memory, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}

	rows, err := r.q.ListMemoriesFiltered(ctx, dbgen.ListMemoriesFilteredParams{
		Category:    strings.TrimSpace(params.Category),
		WorkspaceID: nullablePositiveInt64(params.WorkspaceID),
		CodebaseID:  nullablePositiveInt64(params.CodebaseID),
		Limit:       int64(params.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	return mapDBMemories(rows), nil
}

func (r *MemoryRepo) DeleteByID(ctx context.Context, id string) (bool, error) {
	affected, err := r.q.DeleteMemoryByID(ctx, id)
	if err != nil {
		return false, fmt.Errorf("delete memory: %w", err)
	}
	return affected > 0, nil
}

func (r *MemoryRepo) UpdateByID(ctx context.Context, id string, params UpdateMemoryParams) (Memory, error) {
	current, err := r.GetByID(ctx, id)
	if err != nil {
		return Memory{}, err
	}

	if params.Content != nil {
		current.Content = *params.Content
	}
	if params.Category != nil {
		current.Category = *params.Category
	}
	if params.SourceTask != nil {
		current.SourceTask = *params.SourceTask
	}
	if params.WorkspaceID != nil {
		current.WorkspaceID = params.WorkspaceID
	}
	if params.CodebaseID != nil {
		current.CodebaseID = params.CodebaseID
	}

	affected, err := r.q.UpdateMemory(ctx, dbgen.UpdateMemoryParams{
		Content:     current.Content,
		Category:    current.Category,
		WorkspaceID: nullInt64(current.WorkspaceID),
		CodebaseID:  nullInt64(current.CodebaseID),
		SourceTask:  nullString(current.SourceTask),
		ID:          id,
	})
	if err != nil {
		return Memory{}, fmt.Errorf("update memory: %w", err)
	}
	if affected == 0 {
		return Memory{}, fmt.Errorf("memory not found: %s", id)
	}

	return r.GetByID(ctx, id)
}

func (r *MemoryRepo) SearchLexical(ctx context.Context, query, category string, limit int, workspaceID, codebaseID int64) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}

	like := "%" + query + "%"
	rows, err := r.q.SearchMemoriesLexicalFiltered(ctx, dbgen.SearchMemoriesLexicalFilteredParams{
		Content:      like,
		CategoryLike: like,
		Category:     strings.TrimSpace(category),
		WorkspaceID:  nullablePositiveInt64(workspaceID),
		CodebaseID:   nullablePositiveInt64(codebaseID),
		Limit:        int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search memories lexical: %w", err)
	}

	return mapDBMemories(rows), nil
}

func (r *MemoryRepo) MarkRetrieved(ctx context.Context, id string, now int64) (Memory, error) {
	affected, err := r.q.MarkMemoryRetrieved(ctx, dbgen.MarkMemoryRetrievedParams{
		LastRetrieved: sql.NullInt64{Int64: now, Valid: true},
		ID:            id,
	})
	if err != nil {
		return Memory{}, fmt.Errorf("update retrieve stats: %w", err)
	}
	if affected == 0 {
		return Memory{}, fmt.Errorf("memory not found: %s", id)
	}

	item, err := r.q.GetMemoryByID(ctx, id)
	if err != nil {
		return Memory{}, fmt.Errorf("read retrieved memory: %w", err)
	}

	return fromDBMemory(item), nil
}

func (r *MemoryRepo) MarkRetrievedMany(ctx context.Context, ids []string, now int64) error {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		_, err := r.q.MarkMemoryRetrieved(ctx, dbgen.MarkMemoryRetrievedParams{
			LastRetrieved: sql.NullInt64{Int64: now, Valid: true},
			ID:            id,
		})
		if err != nil {
			return fmt.Errorf("mark memory retrieved (%s): %w", id, err)
		}
	}
	return nil
}

func mapDBMemories(rows []dbgen.Memory) []Memory {
	out := make([]Memory, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDBMemory(row))
	}
	return out
}

func fromDBMemory(row dbgen.Memory) Memory {
	item := Memory{
		ID:        row.ID,
		Content:   row.Content,
		Category:  row.Category,
		CreatedAt: row.CreatedAt,
	}
	if row.WorkspaceID.Valid {
		item.WorkspaceID = &row.WorkspaceID.Int64
	}
	if row.CodebaseID.Valid {
		item.CodebaseID = &row.CodebaseID.Int64
	}

	if row.LastRetrieved.Valid {
		item.LastRetrieved = &row.LastRetrieved.Int64
	}
	if row.RetrievalCount.Valid {
		item.RetrievalCount = row.RetrievalCount.Int64
	}
	if row.SourceTask.Valid {
		item.SourceTask = row.SourceTask.String
	}

	return item
}

func nullString(v string) sql.NullString {
	if strings.TrimSpace(v) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

func nullablePositiveInt64(v int64) sql.NullInt64 {
	if v <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil || *v <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}
