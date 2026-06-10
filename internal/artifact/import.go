package artifact

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// ImportOptions configures the import operation.
type ImportOptions struct {
	ArtifactPath string
	NameOverride string // if non-empty, overrides the codebase name from artifact
}

// Import reads an artifact file and upserts its codebase data into dstDB.
// It validates schema_version compatibility before copying data.
func Import(ctx context.Context, dstDB *sql.DB, opts ImportOptions) error {
	// Validate artifact file exists.
	if _, err := os.Stat(opts.ArtifactPath); err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}

	// Attach the artifact as read-only.
	attachSQL := buildAttachDatabaseSQL(opts.ArtifactPath)
	if _, err := dstDB.ExecContext(ctx, attachSQL); err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer dstDB.ExecContext(ctx, "DETACH DATABASE artifact") //nolint:errcheck

	// Validate meta table exists in artifact.
	var metaExists int
	err := dstDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM artifact.sqlite_master WHERE type='table' AND name='meta'",
	).Scan(&metaExists)
	if err != nil || metaExists == 0 {
		return fmt.Errorf("invalid artifact: missing meta table")
	}

	// Read and validate schema_version.
	var version string
	err = dstDB.QueryRowContext(ctx,
		"SELECT value FROM artifact.meta WHERE key = 'schema_version'",
	).Scan(&version)
	if err != nil {
		return fmt.Errorf("invalid artifact: missing meta table")
	}

	if !isSupportedVersion(version) {
		supported := strings.Join(SupportedSchemaVersions, ", ")
		return fmt.Errorf("unsupported artifact schema version: %s (supported: %s)", version, supported)
	}

	// Read codebase record from artifact.
	var rootPath, name string
	var indexedAt int64
	err = dstDB.QueryRowContext(ctx,
		"SELECT root_path, name, indexed_at FROM artifact.codebases LIMIT 1",
	).Scan(&rootPath, &name, &indexedAt)
	if err != nil {
		return fmt.Errorf("invalid artifact: no codebase record")
	}

	// Apply name override if set.
	if opts.NameOverride != "" {
		name = opts.NameOverride
	}

	// Begin transaction for all write operations.
	tx, err := dstDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := ensureCodebaseMetaTable(ctx, tx); err != nil {
		return fmt.Errorf("ensure codebase_meta table: %w", err)
	}

	// Upsert codebase into local DB by matching on root_path.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO codebases (root_path, name, indexed_at)
		VALUES (?, ?, ?)
		ON CONFLICT(root_path) DO UPDATE SET name = excluded.name, indexed_at = excluded.indexed_at`,
		rootPath, name, indexedAt)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Get the local codebase_id.
	var localCodebaseID int64
	err = tx.QueryRowContext(ctx,
		"SELECT id FROM codebases WHERE root_path = ?", rootPath,
	).Scan(&localCodebaseID)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Delete existing rows for this codebase_id.
	tables := []string{"chunks", "symbols", "edges", "source_files", "indexed_files"}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE codebase_id = ?", table), localCodebaseID); err != nil {
			return fmt.Errorf("import failed: %w", err)
		}
	}

	// Bulk-copy rows from artifact tables, remapping codebase_id.
	// Copy chunks. The delete loop above already cleared existing rows for this
	// codebase_id, so no conflict is possible within this transaction.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chunks (codebase_id, file_path, chunk_key, language, kind, name,
			signature, snippet, start_line, end_line, file_hash, indexed_at,
			embedding, embedding_model, embedding_status)
		SELECT ?, file_path, chunk_key, language, kind, name,
			signature, snippet, start_line, end_line, file_hash, indexed_at,
			embedding, embedding_model, embedding_status
		FROM artifact.chunks`, localCodebaseID); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Copy symbols.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO symbols (codebase_id, file_path, language, kind, name, qualified_name,
			receiver, signature, doc_comment, visibility, body_snippet,
			start_line, end_line, file_hash, indexed_at,
			embedding, embedding_model)
		SELECT ?, file_path, language, kind, name, qualified_name,
			receiver, signature, doc_comment, visibility, body_snippet,
			start_line, end_line, file_hash, indexed_at,
			embedding, embedding_model
		FROM artifact.symbols`, localCodebaseID); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Copy edges.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO edges (codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved, target_codebase_id)
		SELECT ?, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved, target_codebase_id
		FROM artifact.edges`, localCodebaseID); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Copy source_files.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO source_files (codebase_id, file_path, language, package_name,
			loc, symbol_count, file_hash, indexed_at)
		SELECT ?, file_path, language, package_name,
			loc, symbol_count, file_hash, indexed_at
		FROM artifact.source_files`, localCodebaseID); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Copy indexed_files.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO indexed_files (codebase_id, file_path, file_hash, chunk_count, indexed_at, index_status, status_reason)
		SELECT ?, file_path, file_hash, chunk_count, indexed_at, index_status, status_reason
		FROM artifact.indexed_files`, localCodebaseID); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Copy metadata rows from artifact.meta into local meta table,
	// excluding schema_version and per-codebase keys managed in codebase_meta.
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO meta (key, value)
		SELECT key, value FROM artifact.meta
		WHERE key != 'schema_version'
		  AND key NOT IN ('has_embeddings', 'embedding_model', 'embedding_dimensions', 'closed_source', 'source_stripped')`); err != nil {
		return fmt.Errorf("import metadata: %w", err)
	}

	if err := importCodebaseMeta(ctx, tx, localCodebaseID); err != nil {
		return fmt.Errorf("import codebase metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	return nil
}

func ensureCodebaseMetaTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS codebase_meta (
			codebase_id INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
			key         TEXT NOT NULL,
			value       TEXT NOT NULL,
			PRIMARY KEY (codebase_id, key)
		)`); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_codebase_meta_codebase ON codebase_meta(codebase_id)`); err != nil {
		return err
	}

	return nil
}

func importCodebaseMeta(ctx context.Context, tx *sql.Tx, localCodebaseID int64) error {
	var codebaseMetaExists int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM artifact.sqlite_master WHERE type='table' AND name='codebase_meta'",
	).Scan(&codebaseMetaExists); err != nil {
		return err
	}

	if codebaseMetaExists > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO codebase_meta (codebase_id, key, value)
			SELECT ?, key, value FROM artifact.codebase_meta`, localCodebaseID); err != nil {
			return err
		}
		return nil
	}

	// Backward-compatible fallback for older artifacts that only carry global metadata.
	scopedKeys := []string{"has_embeddings", "embedding_model", "embedding_dimensions", "closed_source", "source_stripped"}
	for _, k := range scopedKeys {
		var v string
		err := tx.QueryRowContext(ctx, "SELECT value FROM artifact.meta WHERE key = ?", k).Scan(&v)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO codebase_meta (codebase_id, key, value) VALUES (?, ?, ?)`,
			localCodebaseID, k, v); err != nil {
			return err
		}
	}

	// Default for very old artifacts that lacked embedding metadata entirely.
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO codebase_meta (codebase_id, key, value) VALUES (?, 'has_embeddings', 'false')`, localCodebaseID); err != nil {
		return err
	}

	return nil
}

// isSupportedVersion checks if the given version is in SupportedSchemaVersions.
func isSupportedVersion(version string) bool {
	for _, v := range SupportedSchemaVersions {
		if v == version {
			return true
		}
	}
	return false
}
