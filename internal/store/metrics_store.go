package store

import (
	"context"
	"database/sql"
	"time"
)

// RecordToolCall persists a single tool-call observation asynchronously.
// Errors are silently dropped so this never adds latency to the hot path.
// Only safe to use from a long-lived process (the MCP server) — a short-lived
// CLI command can exit before the goroutine's write lands. CLI callers should
// use RecordToolCallSync instead.
func RecordToolCall(db *sql.DB, tool string, durationMs int64, isError bool) {
	if db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = RecordToolCallSync(ctx, db, tool, durationMs, isError)
	}()
}

// RecordToolCallSync persists a single tool-call observation synchronously,
// for callers (CLI commands) that need the write to land before the process exits.
func RecordToolCallSync(ctx context.Context, db *sql.DB, tool string, durationMs int64, isError bool) error {
	if db == nil {
		return nil
	}
	isErrInt := 0
	if isError {
		isErrInt = 1
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO metric_tool_calls (recorded_at, tool, duration_ms, is_error) VALUES (?, ?, ?, ?)`,
		time.Now().UnixMilli(), tool, durationMs, isErrInt,
	)
	return err
}

// RecordIndexRun persists the outcome of a single index pipeline run asynchronously.
// Only safe to use from a long-lived process (the MCP server); CLI callers should
// use RecordIndexRunSync instead.
func RecordIndexRun(db *sql.DB, codebaseID, files, chunks, durationMs int64) {
	if db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = RecordIndexRunSync(ctx, db, codebaseID, files, chunks, durationMs)
	}()
}

// RecordIndexRunSync persists the outcome of a single index pipeline run synchronously,
// for callers (CLI commands) that need the write to land before the process exits.
func RecordIndexRunSync(ctx context.Context, db *sql.DB, codebaseID, files, chunks, durationMs int64) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO metric_index_runs (recorded_at, codebase_id, files_indexed, chunks_indexed, duration_ms) VALUES (?, ?, ?, ?, ?)`,
		time.Now().UnixMilli(), codebaseID, files, chunks, durationMs,
	)
	return err
}

// RecordAnalyzeRun persists parse and graph stats for a single analyze run asynchronously.
// Only safe to use from a long-lived process (the MCP server); CLI callers should
// use RecordAnalyzeRunSync instead.
func RecordAnalyzeRun(db *sql.DB, codebaseID, totalFiles, complete, textFallbacks, partial, panics, zeroSymbols, totalSymbols, totalEdges, durationMs int64) {
	if db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = RecordAnalyzeRunSync(ctx, db, codebaseID, totalFiles, complete, textFallbacks, partial, panics, zeroSymbols, totalSymbols, totalEdges, durationMs)
	}()
}

// RecordAnalyzeRunSync persists parse and graph stats for a single analyze run
// synchronously, for callers (CLI commands) that need the write to land before
// the process exits.
func RecordAnalyzeRunSync(ctx context.Context, db *sql.DB, codebaseID, totalFiles, complete, textFallbacks, partial, panics, zeroSymbols, totalSymbols, totalEdges, durationMs int64) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO metric_analyze_runs (recorded_at, codebase_id, total_files, complete, text_fallbacks, partial, panics, zero_symbols, total_symbols, total_edges, duration_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UnixMilli(), codebaseID, totalFiles, complete, textFallbacks, partial, panics, zeroSymbols, totalSymbols, totalEdges, durationMs,
	)
	return err
}
