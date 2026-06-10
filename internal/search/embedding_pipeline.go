package search

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/roysland/agentdb/internal/embed"
	"github.com/roysland/agentdb/internal/observe"
)

// EmbeddingPipeline manages asynchronous embedding computation with status tracking.
// It provides an application-layer pipeline for computing vector embeddings,
// explicitly managing status transitions rather than relying on database triggers.
type EmbeddingPipeline struct {
	db       *sql.DB
	provider embed.Provider
	logger   *observe.Logger
}

// NewEmbeddingPipeline creates a new EmbeddingPipeline instance.
func NewEmbeddingPipeline(db *sql.DB, provider embed.Provider, logger *observe.Logger) (*EmbeddingPipeline, error) {
	if db == nil {
		return nil, fmt.Errorf("embedding_pipeline: database connection is nil")
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding_pipeline: embedding provider is nil")
	}
	return &EmbeddingPipeline{
		db:       db,
		provider: provider,
		logger:   logger,
	}, nil
}

// MarkPendingEmbedding sets embedding_status='pending_embedding' for given chunk IDs.
// Called within the same transaction as chunk insertion to ensure a transactional
// boundary between lexical and vector index states.
func (ep *EmbeddingPipeline) MarkPendingEmbedding(ctx context.Context, tx *sql.Tx, chunkIDs []int64) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	// Build a parameterized query for the batch update
	placeholders := make([]string, len(chunkIDs))
	args := make([]interface{}, len(chunkIDs))
	for i, id := range chunkIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		"UPDATE chunks SET embedding_status = 'pending_embedding' WHERE id IN (%s)",
		strings.Join(placeholders, ","),
	)

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		ep.log("error", "mark_pending_embedding", fmt.Sprintf("failed to mark %d chunks as pending: %v", len(chunkIDs), err))
		return fmt.Errorf("embedding_pipeline: mark pending: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	ep.log("info", "mark_pending_embedding", fmt.Sprintf("marked %d chunks as pending_embedding", rowsAffected))
	return nil
}

// pendingChunk represents a chunk awaiting embedding computation.
type pendingChunk struct {
	ID      int64
	Snippet string
}

// ProcessPending fetches chunks with pending_embedding status and computes embeddings.
// On success, updates embedding_status to 'complete' and stores the embedding.
// On failure (network errors, rate limiting), retains pending_embedding status and logs the error.
// Returns the count of successfully processed chunks, failed chunks, and any fatal error.
func (ep *EmbeddingPipeline) ProcessPending(ctx context.Context, batchSize int) (processed int, failed int, err error) {
	if batchSize <= 0 {
		batchSize = 50
	}

	// Fetch chunks with pending_embedding status
	rows, err := ep.db.QueryContext(ctx,
		"SELECT id, snippet FROM chunks WHERE embedding_status = 'pending_embedding' LIMIT ?",
		batchSize,
	)
	if err != nil {
		ep.log("error", "process_pending", fmt.Sprintf("failed to fetch pending chunks: %v", err))
		return 0, 0, fmt.Errorf("embedding_pipeline: fetch pending: %w", err)
	}
	defer rows.Close()

	var chunks []pendingChunk
	for rows.Next() {
		var c pendingChunk
		if err := rows.Scan(&c.ID, &c.Snippet); err != nil {
			ep.log("error", "process_pending", fmt.Sprintf("failed to scan pending chunk: %v", err))
			return processed, failed, fmt.Errorf("embedding_pipeline: scan pending: %w", err)
		}
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return processed, failed, fmt.Errorf("embedding_pipeline: iterate pending: %w", err)
	}

	if len(chunks) == 0 {
		return 0, 0, nil
	}

	ep.log("info", "process_pending", fmt.Sprintf("processing %d pending chunks", len(chunks)))

	// Process each chunk individually
	for _, chunk := range chunks {
		if ctx.Err() != nil {
			// Context cancelled, stop processing
			return processed, failed, ctx.Err()
		}

		embedding, embedErr := ep.provider.Embed(ctx, chunk.Snippet)
		if embedErr != nil {
			// On failure: retain pending_embedding status, log the error, continue
			ep.log("warn", "process_pending", fmt.Sprintf("embedding failed for chunk %d: %v", chunk.ID, embedErr))
			failed++
			continue
		}

		// On success: update embedding and set status to 'complete'
		embeddingBlob := float32ToBlob(embedding)
		_, updateErr := ep.db.ExecContext(ctx,
			"UPDATE chunks SET embedding = ?, embedding_model = ?, embedding_status = 'complete' WHERE id = ?",
			embeddingBlob, ep.provider.ModelName(), chunk.ID,
		)
		if updateErr != nil {
			ep.log("error", "process_pending", fmt.Sprintf("failed to store embedding for chunk %d: %v", chunk.ID, updateErr))
			failed++
			continue
		}

		processed++
	}

	ep.log("info", "process_pending", fmt.Sprintf("completed: %d processed, %d failed", processed, failed))
	return processed, failed, nil
}

// float32ToBlob converts a float32 slice to a little-endian byte blob for storage.
func float32ToBlob(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}
	return buf
}

// log emits a structured log entry if a logger is configured.
func (ep *EmbeddingPipeline) log(level, operation, message string) {
	if ep.logger == nil {
		return
	}
	ep.logger.Log(observe.LogEntry{
		Level:     level,
		Operation: operation,
		Status:    message,
	})
}
