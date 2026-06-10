package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Symbol mirrors the symbols table.
type Symbol struct {
	ID             int64     `json:"id"`
	CodebaseID     int64     `json:"codebase_id"`
	FilePath       string    `json:"file_path"`
	Language       string    `json:"language"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name"`
	QualifiedName  string    `json:"qualified_name"`
	Receiver       string    `json:"receiver,omitempty"`
	Signature      string    `json:"signature,omitempty"`
	DocComment     string    `json:"doc_comment,omitempty"`
	Visibility     string    `json:"visibility,omitempty"`
	BodySnippet    string    `json:"body_snippet,omitempty"`
	StartLine      int64     `json:"start_line"`
	EndLine        int64     `json:"end_line"`
	FileHash       string    `json:"file_hash"`
	IndexedAt      int64     `json:"indexed_at"`
	Embedding      []float32 `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
}

type SymbolData struct {
	FilePath       string
	Language       string
	Kind           string
	Name           string
	QualifiedName  string
	Receiver       string
	Signature      string
	DocComment     string
	Visibility     string
	BodySnippet    string
	StartLine      int64
	EndLine        int64
	FileHash       string
	IndexedAt      int64
	Embedding      []float32
	EmbeddingModel string
}

type SymbolRepo struct{ db *sql.DB }

func NewSymbolRepo(db *sql.DB) *SymbolRepo { return &SymbolRepo{db: db} }

func (r *SymbolRepo) Create(ctx context.Context, codebaseID int64, d SymbolData) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO symbols
			(codebase_id, file_path, language, kind, name, qualified_name, receiver,
			 signature, doc_comment, visibility, body_snippet,
			 start_line, end_line, file_hash, indexed_at, embedding, embedding_model)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		codebaseID, d.FilePath, d.Language, d.Kind, d.Name, d.QualifiedName, d.Receiver,
		d.Signature, d.DocComment, d.Visibility, d.BodySnippet,
		d.StartLine, d.EndLine, d.FileHash, d.IndexedAt,
		embeddingToBlob(d.Embedding), d.EmbeddingModel,
	)
	return err
}

func (r *SymbolRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM symbols WHERE codebase_id = ?`, codebaseID)
	return err
}

// DeleteByFile removes all symbols for a specific file within a codebase.
func (r *SymbolRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM symbols WHERE codebase_id = ? AND file_path = ?`,
		codebaseID, filePath,
	)
	return err
}

// FindByName returns symbols whose name or qualified_name contains the query (case-insensitive).
func (r *SymbolRepo) FindByName(ctx context.Context, codebaseID int64, name string) ([]Symbol, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, file_path, language, kind, name, qualified_name,
		       receiver, signature, doc_comment, visibility, body_snippet,
		       start_line, end_line, file_hash, indexed_at, embedding, embedding_model
		FROM symbols
		WHERE codebase_id = ?
		  AND (name LIKE ? OR qualified_name LIKE ?)
		ORDER BY
			CASE WHEN name = ? THEN 0
			     WHEN qualified_name = ? THEN 1
			     ELSE 2 END,
			name
		LIMIT 50`,
		codebaseID,
		"%"+name+"%", "%"+name+"%",
		name, name,
	)
	if err != nil {
		return nil, fmt.Errorf("find symbols: %w", err)
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// FindByKind returns all symbols of a given kind in the codebase.
func (r *SymbolRepo) FindByKind(ctx context.Context, codebaseID int64, kind string) ([]Symbol, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, file_path, language, kind, name, qualified_name,
		       receiver, signature, doc_comment, visibility, body_snippet,
		       start_line, end_line, file_hash, indexed_at, embedding, embedding_model
		FROM symbols
		WHERE codebase_id = ? AND kind = ?
		ORDER BY file_path, start_line`,
		codebaseID, kind,
	)
	if err != nil {
		return nil, fmt.Errorf("find symbols by kind: %w", err)
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// GetByFile returns all symbols in a specific file.
func (r *SymbolRepo) GetByFile(ctx context.Context, codebaseID int64, filePath string) ([]Symbol, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, file_path, language, kind, name, qualified_name,
		       receiver, signature, doc_comment, visibility, body_snippet,
		       start_line, end_line, file_hash, indexed_at, embedding, embedding_model
		FROM symbols
		WHERE codebase_id = ? AND file_path = ?
		ORDER BY start_line`,
		codebaseID, filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("get symbols by file: %w", err)
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// Stats returns aggregate counts grouped by kind.
func (r *SymbolRepo) Stats(ctx context.Context, codebaseID int64) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT kind, COUNT(*) FROM symbols WHERE codebase_id = ? GROUP BY kind`,
		codebaseID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		out[kind] = count
	}
	return out, rows.Err()
}

// TopFilesBySymbolCount returns files sorted by symbol count descending.
func (r *SymbolRepo) TopFilesBySymbolCount(ctx context.Context, codebaseID int64, limit int) ([]map[string]any, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT file_path, COUNT(*) as cnt
		FROM symbols WHERE codebase_id = ?
		GROUP BY file_path ORDER BY cnt DESC LIMIT ?`,
		codebaseID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var fp string
		var cnt int
		if err := rows.Scan(&fp, &cnt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"file_path": fp, "symbol_count": cnt})
	}
	return out, rows.Err()
}

// ExportedFuncs returns all exported functions (entry points / public API).
func (r *SymbolRepo) ExportedFuncs(ctx context.Context, codebaseID int64, limit int) ([]Symbol, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, file_path, language, kind, name, qualified_name,
		       receiver, signature, doc_comment, visibility, body_snippet,
		       start_line, end_line, file_hash, indexed_at, embedding, embedding_model
		FROM symbols
		WHERE codebase_id = ? AND kind IN ('func','method') AND visibility = 'exported'
		ORDER BY file_path, start_line
		LIMIT ?`,
		codebaseID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// ListWithEmbeddings returns all symbols that have embeddings, for vector search.
func (r *SymbolRepo) ListWithEmbeddings(ctx context.Context, codebaseID int64, limit int) ([]Symbol, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, file_path, language, kind, name, qualified_name,
		       receiver, signature, doc_comment, visibility, body_snippet,
		       start_line, end_line, file_hash, indexed_at, embedding, embedding_model
		FROM symbols
		WHERE codebase_id = ? AND embedding IS NOT NULL
		LIMIT ?`,
		codebaseID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// CapabilityEntry is a minimal symbol record for capability comparison.
// It intentionally omits signature, doc_comment, and body_snippet.
type CapabilityEntry struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	FilePath string `json:"file_path"`
}

// ListCapabilities returns the name, kind, and file_path of every symbol in the
// codebase. Signature and source fields are never included — the result is a
// capability-presence indicator only.
func (r *SymbolRepo) ListCapabilities(ctx context.Context, codebaseID int64) ([]CapabilityEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name, kind, file_path
		FROM symbols
		WHERE codebase_id = ?
		ORDER BY file_path, kind, name`,
		codebaseID,
	)
	if err != nil {
		return nil, fmt.Errorf("list capabilities: %w", err)
	}
	defer rows.Close()

	var out []CapabilityEntry
	for rows.Next() {
		var e CapabilityEntry
		if err := rows.Scan(&e.Name, &e.Kind, &e.FilePath); err != nil {
			return nil, fmt.Errorf("scan capability: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanSymbols(rows *sql.Rows) ([]Symbol, error) {
	out := make([]Symbol, 0)
	for rows.Next() {
		var s Symbol
		var embBlob interface{}
		err := rows.Scan(
			&s.ID, &s.CodebaseID, &s.FilePath, &s.Language,
			&s.Kind, &s.Name, &s.QualifiedName,
			&s.Receiver, &s.Signature, &s.DocComment, &s.Visibility, &s.BodySnippet,
			&s.StartLine, &s.EndLine, &s.FileHash, &s.IndexedAt,
			&embBlob, &s.EmbeddingModel,
		)
		if err != nil {
			return nil, err
		}
		s.Embedding = blobToEmbedding(embBlob)
		out = append(out, s)
	}
	return out, rows.Err()
}
