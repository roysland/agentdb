package search

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/roysland/agentdb/internal/observe"
)

// FTS5Result represents a single result from an FTS5 lexical search.
type FTS5Result struct {
	ChunkID   int64
	FilePath  string
	Name      string
	Kind      string
	Snippet   string
	StartLine int64
	EndLine   int64
	BM25Score float64
	IsPending bool // true if embedding_status = 'pending_embedding'
}

// HybridResult extends FTS5Result with cosine similarity re-ranking metadata.
type HybridResult struct {
	FTS5Result
	CosineScore   float64
	CombinedScore float64
	VectorApplied bool // false if chunk has pending_embedding status
}

// FTS5Search provides full-text search over the chunks table using SQLite FTS5.
type FTS5Search struct {
	db     *sql.DB
	logger *observe.Logger
}

// NewFTS5Search creates a new FTS5 search instance.
func NewFTS5Search(db *sql.DB, logger *observe.Logger) (*FTS5Search, error) {
	if db == nil {
		return nil, fmt.Errorf("fts5: database connection is nil")
	}
	return &FTS5Search{db: db, logger: logger}, nil
}

// EnsureIndex creates the FTS5 virtual table and synchronization triggers if they don't exist.
func (s *FTS5Search) EnsureIndex(ctx context.Context) error {
	statements := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
			snippet, name, file_path,
			content='chunks',
			content_rowid='id'
		)`,
		`CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
			INSERT INTO chunks_fts(rowid, snippet, name, file_path)
			VALUES (new.id, new.snippet, new.name, new.file_path);
		END`,
		`CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
			VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
		END`,
		`CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
			VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
			INSERT INTO chunks_fts(rowid, snippet, name, file_path)
			VALUES (new.id, new.snippet, new.name, new.file_path);
		END`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			s.log("warn", "fts5_ensure_index", fmt.Sprintf("failed to execute statement: %v", err))
			return fmt.Errorf("fts5: ensure index: %w", err)
		}
	}

	s.log("info", "fts5_ensure_index", "FTS5 index and triggers ensured")
	return nil
}

// SearchLexical executes an FTS5 MATCH query and returns results ranked by bm25().
func (s *FTS5Search) SearchLexical(ctx context.Context, query string, codebaseID int64, limit int) ([]FTS5Result, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	// Try the original query first
	results, err := s.executeFTS5Query(ctx, query, codebaseID, limit)
	if err == nil {
		return results, nil
	}

	// If FTS5 query syntax error, escape special chars and retry
	s.log("warn", "fts5_search_lexical", fmt.Sprintf("FTS5 query failed, escaping special chars: %v", err))
	escapedQuery := escapeFTS5Query(query)
	results, err = s.executeFTS5Query(ctx, escapedQuery, codebaseID, limit)
	if err == nil {
		return results, nil
	}

	// If still fails, fall back to LIKE query
	s.log("warn", "fts5_search_lexical", fmt.Sprintf("FTS5 escaped query failed, falling back to LIKE: %v", err))
	return s.searchWithLIKE(ctx, query, codebaseID, limit)
}

// SearchHybrid executes FTS5 for candidate set, then re-ranks by cosine similarity.
func (s *FTS5Search) SearchHybrid(ctx context.Context, query string, queryEmbedding []float32, codebaseID int64, limit int) ([]HybridResult, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	// Get a larger candidate set from FTS5 for re-ranking
	candidateLimit := limit * 3
	if candidateLimit < 50 {
		candidateLimit = 50
	}

	candidates, err := s.SearchLexical(ctx, query, codebaseID, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("fts5: hybrid search candidates: %w", err)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Fetch embeddings for candidates and compute cosine similarity
	results := make([]HybridResult, 0, len(candidates))
	for _, candidate := range candidates {
		hr := HybridResult{
			FTS5Result:    candidate,
			CosineScore:   0,
			CombinedScore: candidate.BM25Score,
			VectorApplied: false,
		}

		// Skip cosine re-ranking for chunks with pending embedding status
		if candidate.IsPending {
			results = append(results, hr)
			continue
		}

		// Fetch the chunk's embedding
		embedding, err := s.getChunkEmbedding(ctx, candidate.ChunkID)
		if err != nil || len(embedding) == 0 {
			// No embedding available, use BM25 score only
			results = append(results, hr)
			continue
		}

		// Compute cosine similarity
		cosine := CosineSimilarity(queryEmbedding, embedding)
		hr.CosineScore = cosine
		hr.VectorApplied = true
		// Combined score: weighted blend of normalized BM25 and cosine
		// BM25 scores are negative (lower is better), so we negate for combination
		hr.CombinedScore = 0.4*(-candidate.BM25Score) + 0.6*cosine
		results = append(results, hr)
	}

	// Sort by combined score descending
	sortHybridResults(results)

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// IsAvailable checks if the FTS5 index exists and is queryable.
func (s *FTS5Search) IsAvailable(ctx context.Context) bool {
	// Check if the chunks_fts table exists and can be queried
	row := s.db.QueryRowContext(ctx, "SELECT count(*) FROM chunks_fts LIMIT 1")
	var count int64
	if err := row.Scan(&count); err != nil {
		s.log("warn", "fts5_is_available", fmt.Sprintf("FTS5 index not available: %v", err))
		return false
	}
	return true
}

// executeFTS5Query runs the actual FTS5 MATCH query against chunks_fts.
func (s *FTS5Search) executeFTS5Query(ctx context.Context, query string, codebaseID int64, limit int) ([]FTS5Result, error) {
	sqlQuery := `
		SELECT c.id, c.file_path, c.name, c.kind, c.snippet, c.start_line, c.end_line,
		       bm25(chunks_fts) as score, c.embedding_status
		FROM chunks_fts fts
		JOIN chunks c ON c.id = fts.rowid
		WHERE chunks_fts MATCH ?
		  AND c.codebase_id = ?
		ORDER BY bm25(chunks_fts)
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, sqlQuery, query, codebaseID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FTS5Result
	for rows.Next() {
		var r FTS5Result
		var embeddingStatus string
		if err := rows.Scan(&r.ChunkID, &r.FilePath, &r.Name, &r.Kind, &r.Snippet,
			&r.StartLine, &r.EndLine, &r.BM25Score, &embeddingStatus); err != nil {
			return nil, fmt.Errorf("fts5: scan result: %w", err)
		}
		r.IsPending = embeddingStatus == "pending_embedding"
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fts5: iterate results: %w", err)
	}

	return results, nil
}

// searchWithLIKE provides a fallback search using SQL LIKE when FTS5 fails.
func (s *FTS5Search) searchWithLIKE(ctx context.Context, query string, codebaseID int64, limit int) ([]FTS5Result, error) {
	likePattern := "%" + strings.ReplaceAll(strings.ReplaceAll(query, "%", "\\%"), "_", "\\_") + "%"

	sqlQuery := `
		SELECT id, file_path, name, kind, snippet, start_line, end_line, embedding_status
		FROM chunks
		WHERE codebase_id = ?
		  AND (snippet LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\' OR file_path LIKE ? ESCAPE '\')
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, sqlQuery, codebaseID, likePattern, likePattern, likePattern, limit)
	if err != nil {
		return nil, fmt.Errorf("fts5: LIKE fallback: %w", err)
	}
	defer rows.Close()

	var results []FTS5Result
	for rows.Next() {
		var r FTS5Result
		var embeddingStatus string
		if err := rows.Scan(&r.ChunkID, &r.FilePath, &r.Name, &r.Kind, &r.Snippet,
			&r.StartLine, &r.EndLine, &embeddingStatus); err != nil {
			return nil, fmt.Errorf("fts5: LIKE scan: %w", err)
		}
		r.IsPending = embeddingStatus == "pending_embedding"
		r.BM25Score = 0 // LIKE doesn't provide ranking
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fts5: LIKE iterate: %w", err)
	}

	return results, nil
}

// getChunkEmbedding fetches the embedding for a specific chunk.
func (s *FTS5Search) getChunkEmbedding(ctx context.Context, chunkID int64) ([]float32, error) {
	var data interface{}
	err := s.db.QueryRowContext(ctx, "SELECT embedding FROM chunks WHERE id = ?", chunkID).Scan(&data)
	if err != nil {
		return nil, err
	}
	return blobToFloat32Embedding(data), nil
}

// blobToFloat32Embedding converts a raw blob to a float32 slice.
func blobToFloat32Embedding(data interface{}) []float32 {
	if data == nil {
		return nil
	}
	buf, ok := data.([]byte)
	if !ok || len(buf) == 0 {
		return nil
	}
	embedding := make([]float32, len(buf)/4)
	for i := 0; i < len(embedding); i++ {
		bits := binary.LittleEndian.Uint32(buf[i*4:])
		embedding[i] = math.Float32frombits(bits)
	}
	return embedding
}

// escapeFTS5Query escapes special FTS5 characters in a query string.
// FTS5 special characters: " * ( ) : ^ - + ~ |
func escapeFTS5Query(query string) string {
	// Remove or escape FTS5 special characters
	replacer := strings.NewReplacer(
		`"`, ``,
		`*`, ``,
		`(`, ``,
		`)`, ``,
		`:`, ``,
		`^`, ``,
		`-`, ` `,
		`+`, ` `,
		`~`, ``,
		`|`, ``,
		`{`, ``,
		`}`, ``,
	)
	escaped := replacer.Replace(query)

	// Collapse multiple spaces and trim
	parts := strings.Fields(escaped)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// sortHybridResults sorts hybrid results by CombinedScore descending.
func sortHybridResults(results []HybridResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].CombinedScore > results[j].CombinedScore
	})
}

// log emits a structured log entry if a logger is configured.
func (s *FTS5Search) log(level, operation, message string) {
	if s.logger == nil {
		return
	}
	s.logger.Log(observe.LogEntry{
		Level:     level,
		Operation: operation,
		Status:    message,
	})
}
