package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/roysland/agentdb/internal/filefilter"
)

// DeltaResult categorizes files by their change status relative to stored hashes.
type DeltaResult struct {
	Changed   []string // files with different hash
	Added     []string // files not in previous manifest
	Removed   []string // files in manifest but not on disk
	Unchanged []string // files with matching hash
}

// FilesToProcess returns a copy-safe concatenation of changed and added files.
func FilesToProcess(delta DeltaResult) []string {
	files := make([]string, 0, len(delta.Changed)+len(delta.Added))
	files = append(files, delta.Changed...)
	files = append(files, delta.Added...)
	return files
}

// hexPattern matches strings containing only hexadecimal characters.
var hexPattern = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// IsLegacyHash returns true if the hash is a legacy MD5 hash (less than 64 hex chars).
// A valid SHA-256 hash is exactly 64 hex characters. Any shorter hex string is considered legacy.
func IsLegacyHash(hash string) bool {
	if len(hash) >= 64 {
		return false
	}
	// Must be a valid hex string to be considered a legacy hash
	return len(hash) > 0 && hexPattern.MatchString(hash)
}

// HashFile computes the SHA-256 hash of a file's content and returns it as a hex string.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ComputeDelta compares current file hashes on disk against storedHashes and categorizes
// each file as Changed, Added, Removed, or Unchanged.
// The codebaseID parameter is included for future use but not needed for delta computation.
func ComputeDelta(ctx context.Context, codebaseID int64, rootPath string, storedHashes map[string]string) (DeltaResult, error) {
	var result DeltaResult
	matcher := filefilter.NewMatcher(rootPath)

	// Track which stored files we've seen on disk
	seen := make(map[string]bool, len(storedHashes))

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip ignored directories for both correctness and performance.
		if info.IsDir() {
			if matcher.ShouldSkipDir(path, info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if !filefilter.IsConfinedRegularFile(rootPath, path, info) {
			return nil
		}

		if !matcher.IsCodeFile(path) {
			return nil
		}

		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}

		// Normalize to forward slashes for consistent keys
		relPath = filepath.ToSlash(relPath)

		currentHash, err := HashFile(path)
		if err != nil {
			return err
		}

		storedHash, exists := storedHashes[relPath]
		seen[relPath] = true

		if !exists {
			result.Added = append(result.Added, relPath)
		} else if IsLegacyHash(storedHash) || currentHash != storedHash {
			// Legacy MD5 hashes (< 64 hex chars) are always treated as changed
			// to force re-indexing with SHA-256
			result.Changed = append(result.Changed, relPath)
		} else {
			result.Unchanged = append(result.Unchanged, relPath)
		}

		return nil
	})
	if err != nil {
		return DeltaResult{}, err
	}

	// Files in storedHashes but not on disk are Removed
	for filePath := range storedHashes {
		if !seen[filePath] {
			result.Removed = append(result.Removed, filePath)
		}
	}

	return result, nil
}

// MigrationResult tracks the outcome of an MD5→SHA-256 migration cycle.
type MigrationResult struct {
	FilesReindexed  int
	OrphanedRemoved int
	PagesReclaimed  int64
}

// RunPostMigrationMaintenance executes incremental_vacuum and ANALYZE after migration.
// This reclaims pages freed by orphaned chunk deletions and updates query planner statistics.
func RunPostMigrationMaintenance(ctx context.Context, db *sql.DB) (MigrationResult, error) {
	var result MigrationResult

	// Get freelist count before vacuum to calculate pages reclaimed
	var freePagesBefore int64
	err := db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freePagesBefore)
	if err != nil {
		return result, err
	}

	// Execute incremental vacuum to reclaim free pages
	_, err = db.ExecContext(ctx, "PRAGMA incremental_vacuum")
	if err != nil {
		return result, err
	}

	// Get freelist count after vacuum
	var freePagesAfter int64
	err = db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freePagesAfter)
	if err != nil {
		return result, err
	}

	result.PagesReclaimed = freePagesBefore - freePagesAfter

	// Update query planner statistics
	_, err = db.ExecContext(ctx, "ANALYZE")
	if err != nil {
		return result, err
	}

	return result, nil
}

// VerifyIntegrity checks that no chunks reference file_hashes absent from indexed_files.
// Returns the count of orphaned chunks found.
func VerifyIntegrity(ctx context.Context, db *sql.DB, codebaseID int64) (orphanCount int, err error) {
	query := `
		SELECT COUNT(*) FROM chunks c
		WHERE c.codebase_id = ?
		AND NOT EXISTS (
			SELECT 1 FROM indexed_files f
			WHERE f.file_hash = c.file_hash
			AND f.codebase_id = c.codebase_id
		)
	`
	err = db.QueryRowContext(ctx, query, codebaseID).Scan(&orphanCount)
	return orphanCount, err
}
