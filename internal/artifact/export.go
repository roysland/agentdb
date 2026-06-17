package artifact

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

func buildAttachDatabaseSQL(path string) string {
	escapedPath := strings.ReplaceAll(path, "'", "''")
	return fmt.Sprintf("ATTACH DATABASE '%s' AS artifact", escapedPath)
}

// ExportOptions configures the export operation.
type ExportOptions struct {
	CodebaseID  int64
	OutputPath  string
	StripSource bool // when true, strip source-bearing text fields from exported chunks/symbols
}

// Export creates a standalone SQLite artifact from the given codebase.
// It opens a new file at opts.OutputPath, applies the artifact schema,
// and copies all rows for the specified codebase_id.
func Export(ctx context.Context, srcDB *sql.DB, opts ExportOptions) error {
	// Verify codebase exists in srcDB.
	var exists int
	err := srcDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM codebases WHERE id = ?", opts.CodebaseID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check codebase: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("codebase not found: %d", opts.CodebaseID)
	}

	// Remove existing file if present (overwrite).
	if err := os.Remove(opts.OutputPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing artifact: %w", err)
	}

	// Create new SQLite file and apply artifact DDL.
	artifactDB, err := sql.Open("sqlite", opts.OutputPath)
	if err != nil {
		return fmt.Errorf("create artifact: %w", err)
	}
	if err := applyArtifactSchema(ctx, artifactDB); err != nil {
		artifactDB.Close()
		return fmt.Errorf("apply artifact schema: %w", err)
	}
	artifactDB.Close()

	// Attach the artifact file to the source DB and bulk-copy data.
	attachSQL := buildAttachDatabaseSQL(opts.OutputPath)
	if _, err := srcDB.ExecContext(ctx, attachSQL); err != nil {
		return fmt.Errorf("attach artifact: %w", err)
	}
	defer srcDB.ExecContext(ctx, "DETACH DATABASE artifact") //nolint:errcheck

	// Run all copy operations in a single transaction.
	tx, err := srcDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Copy codebases row.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact.codebases (id, root_path, name, indexed_at)
		SELECT id, root_path, name, indexed_at
		FROM codebases WHERE id = ?`, opts.CodebaseID); err != nil {
		return fmt.Errorf("copy codebases: %w", err)
	}

	chunkSignatureSelect := "signature"
	chunkSnippetSelect := "snippet"
	if opts.StripSource {
		chunkSignatureSelect = "''"
		chunkSnippetSelect = "''"
	}

	// Copy chunks (optionally strip snippet/signature text).
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO artifact.chunks (codebase_id, file_path, chunk_key, language, kind, name,
			signature, snippet, start_line, end_line, file_hash, indexed_at)
		SELECT codebase_id, file_path, chunk_key, language, kind, name,
			%s, %s, start_line, end_line, file_hash, indexed_at
		FROM chunks WHERE codebase_id = ?`, chunkSignatureSelect, chunkSnippetSelect), opts.CodebaseID); err != nil {
		return fmt.Errorf("copy chunks: %w", err)
	}

	// Copy indexed_files.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact.indexed_files (codebase_id, file_path, file_hash, chunk_count, indexed_at, index_status, status_reason)
		SELECT codebase_id, file_path, file_hash, chunk_count, indexed_at, index_status, status_reason
		FROM indexed_files WHERE codebase_id = ?`, opts.CodebaseID); err != nil {
		return fmt.Errorf("copy indexed_files: %w", err)
	}

	symbolSignatureSelect := "signature"
	symbolDocCommentSelect := "doc_comment"
	symbolBodySnippetSelect := "body_snippet"
	if opts.StripSource {
		symbolSignatureSelect = "''"
		symbolDocCommentSelect = "''"
		symbolBodySnippetSelect = "''"
	}

	// Copy symbols (optionally strip signature/doc/body text).
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO artifact.symbols (codebase_id, file_path, language, kind, name, qualified_name,
			receiver, signature, doc_comment, visibility, body_snippet,
			start_line, end_line, file_hash, indexed_at)
		SELECT codebase_id, file_path, language, kind, name, qualified_name,
			receiver, %s, %s, visibility, %s,
			start_line, end_line, file_hash, indexed_at
		FROM symbols WHERE codebase_id = ?`, symbolSignatureSelect, symbolDocCommentSelect, symbolBodySnippetSelect), opts.CodebaseID); err != nil {
		return fmt.Errorf("copy symbols: %w", err)
	}

	// Copy source_files.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact.source_files (codebase_id, file_path, language, package_name,
			loc, symbol_count, file_hash, indexed_at)
		SELECT codebase_id, file_path, language, package_name,
			loc, symbol_count, file_hash, indexed_at
		FROM source_files WHERE codebase_id = ?`, opts.CodebaseID); err != nil {
		return fmt.Errorf("copy source_files: %w", err)
	}

	// Copy edges.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact.edges (codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved, target_codebase_id)
		SELECT codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved, target_codebase_id
		FROM edges WHERE codebase_id = ?`, opts.CodebaseID); err != nil {
		return fmt.Errorf("copy edges: %w", err)
	}

	// Insert schema_version into artifact's meta table.
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO artifact.meta (key, value) VALUES ('schema_version', '2')`); err != nil {
		return fmt.Errorf("write schema_version: %w", err)
	}

	closedSource := "false"
	sourceStripped := "false"
	if opts.StripSource {
		closedSource = "true"
		sourceStripped = "true"
	}

	// Keep policy metadata in artifact.meta for compatibility with older importers.
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO artifact.meta (key, value) VALUES ('closed_source', ?)`, closedSource); err != nil {
		return fmt.Errorf("write closed_source metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO artifact.meta (key, value) VALUES ('source_stripped', ?)`, sourceStripped); err != nil {
		return fmt.Errorf("write source_stripped metadata: %w", err)
	}

	// Store codebase-scoped metadata for long-term multi-codebase correctness.
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO artifact.codebase_meta (codebase_id, key, value)
		VALUES (?, 'closed_source', ?),
			   (?, 'source_stripped', ?)`,
		opts.CodebaseID, closedSource,
		opts.CodebaseID, sourceStripped); err != nil {
		return fmt.Errorf("write codebase metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
