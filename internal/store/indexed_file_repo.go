package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// IndexedFile mirrors the indexed_files table.
type IndexedFile struct {
	ID         int64  `json:"id"`
	CodebaseID int64  `json:"codebase_id"`
	FilePath   string `json:"file_path"`
	FileHash   string `json:"file_hash"`
	ChunkCount int64  `json:"chunk_count"`
	IndexedAt  int64  `json:"indexed_at"`
}

type IndexedFileRepo struct{ db *sql.DB }

func NewIndexedFileRepo(db *sql.DB) *IndexedFileRepo { return &IndexedFileRepo{db: db} }

// GetHashesByCodebase returns a map of file_path -> file_hash for all indexed files in a codebase.
func (r *IndexedFileRepo) GetHashesByCodebase(ctx context.Context, codebaseID int64) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT file_path, file_hash FROM indexed_files WHERE codebase_id = ?`,
		codebaseID,
	)
	if err != nil {
		return nil, fmt.Errorf("get indexed file hashes: %w", err)
	}
	defer rows.Close()

	hashes := make(map[string]string)
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, fmt.Errorf("scan indexed file hash: %w", err)
		}
		hashes[path] = hash
	}
	return hashes, rows.Err()
}

// Upsert inserts or updates an indexed file record.
func (r *IndexedFileRepo) Upsert(ctx context.Context, codebaseID int64, filePath, fileHash string, chunkCount int64, indexedAt int64) error {
	return r.UpsertWithStatus(ctx, codebaseID, filePath, fileHash, chunkCount, indexedAt, "complete", "")
}

// UpsertWithStatus inserts or updates an indexed file record with index status metadata.
func (r *IndexedFileRepo) UpsertWithStatus(ctx context.Context, codebaseID int64, filePath, fileHash string, chunkCount int64, indexedAt int64, indexStatus, statusReason string) error {
	if indexStatus == "" {
		indexStatus = "complete"
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO indexed_files (codebase_id, file_path, file_hash, chunk_count, indexed_at, index_status, status_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(codebase_id, file_path) DO UPDATE SET
			file_hash     = excluded.file_hash,
			chunk_count   = excluded.chunk_count,
			indexed_at    = excluded.indexed_at,
			index_status  = excluded.index_status,
			status_reason = excluded.status_reason`,
		codebaseID, filePath, fileHash, chunkCount, indexedAt, indexStatus, statusReason,
	)
	return err
}

// DeleteByFile removes the indexed_files record for a specific file in a codebase.
func (r *IndexedFileRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM indexed_files WHERE codebase_id = ? AND file_path = ?`,
		codebaseID, filePath,
	)
	return err
}

// DeleteByCodebase removes all indexed_files records for a codebase.
func (r *IndexedFileRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM indexed_files WHERE codebase_id = ?`,
		codebaseID,
	)
	return err
}

// DegradationInfo holds the degradation status for a file.
type DegradationInfo struct {
	IndexStatus  string
	StatusReason string
}

// GetDegradedFiles returns a map of file_path -> DegradationInfo for files in the given
// codebase whose index_status is NOT 'complete'. Only files present in the provided
// filePaths slice are checked. This enables batch lookup without a per-result query.
func (r *IndexedFileRepo) GetDegradedFiles(ctx context.Context, codebaseID int64, filePaths []string) (map[string]DegradationInfo, error) {
	if len(filePaths) == 0 {
		return nil, nil
	}

	// Build query with placeholders for the file paths.
	placeholders := make([]string, len(filePaths))
	args := make([]any, 0, len(filePaths)+1)
	args = append(args, codebaseID)
	for i, fp := range filePaths {
		placeholders[i] = "?"
		args = append(args, fp)
	}

	query := fmt.Sprintf(
		`SELECT file_path, index_status, status_reason FROM indexed_files WHERE codebase_id = ? AND index_status != 'complete' AND file_path IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get degraded files: %w", err)
	}
	defer rows.Close()

	result := make(map[string]DegradationInfo)
	for rows.Next() {
		var path, status, reason string
		if err := rows.Scan(&path, &status, &reason); err != nil {
			return nil, fmt.Errorf("scan degraded file: %w", err)
		}
		result[path] = DegradationInfo{IndexStatus: status, StatusReason: reason}
	}
	return result, rows.Err()
}
