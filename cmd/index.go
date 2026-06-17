package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/chunk"
	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/index"
	"github.com/roysland/agentdb/internal/store"
)

type indexCmd struct {
	codebaseID    int64
	path          string
	codebasePath  string
	linesPerChunk int
	incremental   bool
}

func newIndexCmd(ctx context.Context) *cobra.Command {
	ic := &indexCmd{}

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index a registered codebase",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ic.run(ctx)
		},
	}

	cmd.Flags().Int64Var(&ic.codebaseID, "codebase-id", 0, "Codebase ID to index")
	cmd.Flags().StringVar(&ic.path, "path", "", "Path to codebase root (registers codebase if missing)")
	cmd.Flags().StringVar(&ic.codebasePath, "codebase-path", "", "Path to codebase root")
	cmd.Flags().IntVar(&ic.linesPerChunk, "lines-per-chunk", 0, "Lines per chunk (default from config/env, fallback 50)")
	cmd.Flags().BoolVar(&ic.incremental, "incremental", false, "Only re-index changed files (uses stored file hashes)")

	return cmd
}

func (ic *indexCmd) run(ctx context.Context) error {
	startTime := time.Now()

	resolved := config.Resolve(rootCfg)
	if ic.linesPerChunk <= 0 {
		ic.linesPerChunk = resolved.IndexLinesPerChunk
	}

	dbConn, err := db.Open(ctx, resolved)
	if err != nil {
		return err
	}
	defer dbConn.Close()

	catalogRepo := store.NewCatalogRepo(dbConn)
	codebaseID, codebasePath, err := ic.resolveTarget(ctx, catalogRepo, resolved)
	if err != nil {
		return err
	}

	// Create repositories
	chunkRepo := store.NewChunkRepo(dbConn)
	indexedFileRepo := store.NewIndexedFileRepo(dbConn)

	// Determine if we should do incremental indexing
	if ic.incremental {
		return ic.runIncremental(ctx, codebaseID, codebasePath, chunkRepo, indexedFileRepo, startTime)
	}

	return ic.runFull(ctx, codebaseID, codebasePath, chunkRepo, indexedFileRepo, startTime)
}

func (ic *indexCmd) runFull(ctx context.Context, codebaseID int64, codebasePath string, chunkRepo *store.ChunkRepo, indexedFileRepo *store.IndexedFileRepo, startTime time.Time) error {
	// Delete existing chunks for this codebase
	if err := chunkRepo.DeleteByCodebase(ctx, codebaseID); err != nil {
		return fmt.Errorf("delete existing chunks: %w", err)
	}

	// Delete existing indexed_files records
	if err := indexedFileRepo.DeleteByCodebase(ctx, codebaseID); err != nil {
		return fmt.Errorf("delete existing indexed files: %w", err)
	}

	// Chunk the codebase
	cfg := chunk.ChunkerConfig{
		LinesPerChunk: ic.linesPerChunk,
	}

	chunksMap, err := chunk.ChunkDirectory(codebasePath, cfg)
	if err != nil {
		return fmt.Errorf("chunk directory: %w", err)
	}

	// Store chunks in database and record manifest
	totalChunks := 0
	indexedAt := time.Now().Unix()
	totalFiles := len(chunksMap)

	for filePath, chunks := range chunksMap {
		fileChunkCount := 0
		var fileHash string
		for _, c := range chunks {
			chunkData := store.ChunkData{
				FilePath:  filePath,
				ChunkKey:  c.Key,
				Language:  c.Language,
				Kind:      c.Kind,
				Name:      c.Name,
				Signature: c.Signature,
				Snippet:   c.Snippet,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				FileHash:  c.FileHash,
				IndexedAt: indexedAt,
			}

			if err := chunkRepo.Create(ctx, codebaseID, chunkData); err != nil {
				return wrapChunkErr(err, c.Key)
			}

			fileHash = c.FileHash
			fileChunkCount++
			totalChunks++
		}

		// Record manifest entry for this file
		if err := indexedFileRepo.Upsert(ctx, codebaseID, filePath, fileHash, int64(fileChunkCount), indexedAt); err != nil {
			return fmt.Errorf("record indexed file %s: %w", filePath, err)
		}
	}

	duration := time.Since(startTime)
	durationMs := duration.Milliseconds()
	filesPerSecond := float64(0)
	if durationMs > 0 {
		filesPerSecond = float64(totalFiles) / duration.Seconds()
	}

	result := map[string]interface{}{
		"codebase_id":      codebaseID,
		"codebase_path":    codebasePath,
		"total_files":      totalFiles,
		"files_indexed":    totalFiles,
		"files_skipped":    0,
		"files_changed":    0,
		"files_added":      totalFiles,
		"files_removed":    0,
		"total_chunks":     totalChunks,
		"duration_ms":      durationMs,
		"files_per_second": filesPerSecond,
		"incremental":      false,
	}

	return printJSON(result)
}

func (ic *indexCmd) runIncremental(ctx context.Context, codebaseID int64, codebasePath string, chunkRepo *store.ChunkRepo, indexedFileRepo *store.IndexedFileRepo, startTime time.Time) error {
	// Load stored hashes from indexed_files table
	storedHashes, err := indexedFileRepo.GetHashesByCodebase(ctx, codebaseID)
	if err != nil {
		return fmt.Errorf("load stored hashes: %w", err)
	}

	// If no manifest exists, fall back to full index
	if len(storedHashes) == 0 {
		return ic.runFull(ctx, codebaseID, codebasePath, chunkRepo, indexedFileRepo, startTime)
	}

	// Compute delta
	delta, err := index.ComputeDelta(ctx, codebaseID, codebasePath, storedHashes)
	if err != nil {
		return fmt.Errorf("compute delta: %w", err)
	}

	indexedAt := time.Now().Unix()
	totalChunks := 0

	cfg := chunk.ChunkerConfig{
		LinesPerChunk: ic.linesPerChunk,
	}

	// Process Changed + Added files: delete old chunks and re-chunk
	filesToProcess := make([]string, 0, len(delta.Changed)+len(delta.Added))
	filesToProcess = append(filesToProcess, delta.Changed...)
	filesToProcess = append(filesToProcess, delta.Added...)
	for _, relPath := range filesToProcess {
		// Delete existing chunks for this file
		if err := chunkRepo.DeleteByFile(ctx, codebaseID, relPath); err != nil {
			return fmt.Errorf("delete chunks for file %s: %w", relPath, err)
		}

		// Chunk the single file
		absPath := filepath.Join(codebasePath, filepath.FromSlash(relPath))
		fileChunks, err := chunk.ChunkFile(absPath, cfg)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to chunk file %s: %v\n", relPath, err)
			continue
		}

		var fileHash string
		fileChunkCount := 0
		for _, c := range fileChunks {
			chunkData := store.ChunkData{
				FilePath:  relPath,
				ChunkKey:  c.Key,
				Language:  c.Language,
				Kind:      c.Kind,
				Name:      c.Name,
				Signature: c.Signature,
				Snippet:   c.Snippet,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				FileHash:  c.FileHash,
				IndexedAt: indexedAt,
			}

			if err := chunkRepo.Create(ctx, codebaseID, chunkData); err != nil {
				return wrapChunkErr(err, c.Key)
			}

			fileHash = c.FileHash
			fileChunkCount++
			totalChunks++
		}

		// Update indexed_files with new hash
		if fileHash != "" {
			if err := indexedFileRepo.Upsert(ctx, codebaseID, relPath, fileHash, int64(fileChunkCount), indexedAt); err != nil {
				return fmt.Errorf("update indexed file %s: %w", relPath, err)
			}
		}
	}

	// Delete chunks for Removed files
	for _, relPath := range delta.Removed {
		if err := chunkRepo.DeleteByFile(ctx, codebaseID, relPath); err != nil {
			return fmt.Errorf("delete chunks for removed file %s: %w", relPath, err)
		}
		if err := indexedFileRepo.DeleteByFile(ctx, codebaseID, relPath); err != nil {
			return fmt.Errorf("delete indexed file record %s: %w", relPath, err)
		}
	}

	// Compute metrics
	totalFiles := len(delta.Changed) + len(delta.Added) + len(delta.Removed) + len(delta.Unchanged)
	filesIndexed := len(delta.Changed) + len(delta.Added)
	duration := time.Since(startTime)
	durationMs := duration.Milliseconds()
	filesPerSecond := float64(0)
	if durationMs > 0 {
		filesPerSecond = float64(filesIndexed) / duration.Seconds()
	}

	result := map[string]interface{}{
		"codebase_id":      codebaseID,
		"codebase_path":    codebasePath,
		"total_files":      totalFiles,
		"files_indexed":    filesIndexed,
		"files_skipped":    len(delta.Unchanged),
		"files_changed":    len(delta.Changed),
		"files_added":      len(delta.Added),
		"files_removed":    len(delta.Removed),
		"total_chunks":     totalChunks,
		"duration_ms":      durationMs,
		"files_per_second": filesPerSecond,
		"incremental":      true,
	}

	return printJSON(result)
}

func (ic *indexCmd) resolveTarget(ctx context.Context, repo *store.CatalogRepo, resolved config.Runtime) (int64, string, error) {
	return resolveCodebaseTarget(ctx, repo, resolved, ic.codebaseID, ic.path, ic.codebasePath, true)
}

// wrapChunkErr enriches a chunk insert error with a remediation hint when the
// database rejects a duplicate key — a symptom of stale chunks from a previous
// interrupted run.  The hint is only appended when the error message contains
// the SQLite UNIQUE constraint text so routine errors are not affected.
func wrapChunkErr(err error, key string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return fmt.Errorf("create chunk %s: %w\n  hint: stale chunks may exist — re-run without --incremental to clear and rebuild", key, err)
	}
	return fmt.Errorf("create chunk %s: %w", key, err)
}
