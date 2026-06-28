package store

import (
	"context"
	"database/sql"
	"time"
)

// RecordToolCall persists a single tool-call observation asynchronously.
// Errors are silently dropped so this never adds latency to the hot path.
func RecordToolCall(db *sql.DB, tool string, durationMs int64, isError bool) {
	if db == nil {
		return
	}
	isErrInt := 0
	if isError {
		isErrInt = 1
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = db.ExecContext(ctx,
			`INSERT INTO metric_tool_calls (recorded_at, tool, duration_ms, is_error) VALUES (?, ?, ?, ?)`,
			time.Now().UnixMilli(), tool, durationMs, isErrInt,
		)
	}()
}

// RecordIndexRun persists the outcome of a single index pipeline run asynchronously.
func RecordIndexRun(db *sql.DB, codebaseID, files, chunks, embeddingFailures, durationMs int64) {
	if db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = db.ExecContext(ctx,
			`INSERT INTO metric_index_runs (recorded_at, codebase_id, files_indexed, chunks_indexed, embedding_failures, duration_ms) VALUES (?, ?, ?, ?, ?, ?)`,
			time.Now().UnixMilli(), codebaseID, files, chunks, embeddingFailures, durationMs,
		)
	}()
}

// RecordAnalyzeRun persists parse and graph stats for a single analyze run asynchronously.
func RecordAnalyzeRun(db *sql.DB, codebaseID, totalFiles, complete, textFallbacks, partial, panics, zeroSymbols, totalSymbols, totalEdges, durationMs int64) {
	if db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = db.ExecContext(ctx,
			`INSERT INTO metric_analyze_runs (recorded_at, codebase_id, total_files, complete, text_fallbacks, partial, panics, zero_symbols, total_symbols, total_edges, duration_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			time.Now().UnixMilli(), codebaseID, totalFiles, complete, textFallbacks, partial, panics, zeroSymbols, totalSymbols, totalEdges, durationMs,
		)
	}()
}
