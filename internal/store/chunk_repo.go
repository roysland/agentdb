package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"

	dbgen "github.com/roysland/agentdb/data/gen"
)

type Chunk struct {
	ID             int64     `json:"id"`
	CodebaseID     int64     `json:"codebase_id"`
	FilePath       string    `json:"file_path"`
	ChunkKey       string    `json:"chunk_key"`
	Language       string    `json:"language"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name"`
	Signature      string    `json:"signature"`
	Snippet        string    `json:"snippet"`
	StartLine      int64     `json:"start_line"`
	EndLine        int64     `json:"end_line"`
	FileHash       string    `json:"file_hash"`
	IndexedAt      int64     `json:"indexed_at"`
	Embedding      []float32 `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
}

type ChunkRepo struct {
	db *sql.DB
	q  *dbgen.Queries
}

func NewChunkRepo(db *sql.DB) *ChunkRepo {
	return &ChunkRepo{db: db, q: dbgen.New(db)}
}

func (r *ChunkRepo) Create(ctx context.Context, codebaseID int64, chunk ChunkData) error {
	embeddingBlob := embeddingToBlob(chunk.Embedding)

	return r.q.CreateChunk(ctx, dbgen.CreateChunkParams{
		CodebaseID:     codebaseID,
		FilePath:       chunk.FilePath,
		ChunkKey:       chunk.ChunkKey,
		Language:       chunk.Language,
		Kind:           chunk.Kind,
		Name:           chunk.Name,
		Signature:      chunk.Signature,
		Snippet:        chunk.Snippet,
		StartLine:      chunk.StartLine,
		EndLine:        chunk.EndLine,
		FileHash:       chunk.FileHash,
		IndexedAt:      chunk.IndexedAt,
		Embedding:      embeddingBlob,
		EmbeddingModel: sql.NullString{String: chunk.EmbeddingModel, Valid: chunk.EmbeddingModel != ""},
	})
}

// CreateReturningID inserts a chunk and returns the auto-generated row ID.
func (r *ChunkRepo) CreateReturningID(ctx context.Context, codebaseID int64, chunk ChunkData) (int64, error) {
	embeddingBlob := embeddingToBlob(chunk.Embedding)

	embModel := sql.NullString{String: chunk.EmbeddingModel, Valid: chunk.EmbeddingModel != ""}
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO chunks (codebase_id, file_path, chunk_key, language, kind, name, signature, snippet, start_line, end_line, file_hash, indexed_at, embedding, embedding_model)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		codebaseID, chunk.FilePath, chunk.ChunkKey, chunk.Language, chunk.Kind, chunk.Name,
		chunk.Signature, chunk.Snippet, chunk.StartLine, chunk.EndLine, chunk.FileHash,
		chunk.IndexedAt, embeddingBlob, embModel,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (r *ChunkRepo) GetByCodebase(ctx context.Context, codebaseID int64) ([]Chunk, error) {
	rows, err := r.q.GetChunksByCodebase(ctx, codebaseID)
	if err != nil {
		return nil, fmt.Errorf("get chunks by codebase: %w", err)
	}

	return mapDBChunks(rows), nil
}

func (r *ChunkRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error {
	return r.q.DeleteChunksByCodebase(ctx, codebaseID)
}

// DeleteByFile removes all chunks for a specific file within a codebase.
func (r *ChunkRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM chunks WHERE codebase_id = ? AND file_path = ?`,
		codebaseID, filePath,
	)
	return err
}

// ChunkData represents input data for creating a chunk
type ChunkData struct {
	FilePath       string
	ChunkKey       string
	Language       string
	Kind           string
	Name           string
	Signature      string
	Snippet        string
	StartLine      int64
	EndLine        int64
	FileHash       string
	IndexedAt      int64
	Embedding      []float32
	EmbeddingModel string
}

func mapDBChunks(rows []dbgen.Chunk) []Chunk {
	chunks := make([]Chunk, len(rows))
	for i, row := range rows {
		chunks[i] = Chunk{
			ID:             row.ID,
			CodebaseID:     row.CodebaseID,
			FilePath:       row.FilePath,
			ChunkKey:       row.ChunkKey,
			Language:       row.Language,
			Kind:           row.Kind,
			Name:           row.Name,
			Signature:      row.Signature,
			Snippet:        row.Snippet,
			StartLine:      row.StartLine,
			EndLine:        row.EndLine,
			FileHash:       row.FileHash,
			IndexedAt:      row.IndexedAt,
			Embedding:      blobToEmbedding(row.Embedding),
			EmbeddingModel: row.EmbeddingModel.String,
		}
	}
	return chunks
}

func embeddingToBlob(embedding []float32) interface{} {
	if len(embedding) == 0 {
		return nil
	}

	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}
	return buf
}

func blobToEmbedding(data interface{}) []float32 {
	if data == nil {
		return nil
	}

	buf, ok := data.([]byte)
	if !ok {
		return nil
	}

	embedding := make([]float32, len(buf)/4)
	for i := 0; i < len(embedding); i++ {
		bits := binary.LittleEndian.Uint32(buf[i*4:])
		embedding[i] = math.Float32frombits(bits)
	}
	return embedding
}
