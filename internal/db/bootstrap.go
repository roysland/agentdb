package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

type BootstrapStats struct {
	StatementsApplied int `json:"statements_applied"`
}

func BootstrapSchema(ctx context.Context, db *sql.DB, schemaPath string) (BootstrapStats, error) {
	var content []byte
	var err error

	// Try embedded schema first if using the default path
	if schemaPath == "data/schema.sql" {
		schemaStr, embErr := GetEmbeddedSchema()
		if embErr == nil {
			content = []byte(schemaStr)
		} else {
			// Fall back to reading from disk
			content, err = os.ReadFile(schemaPath)
			if err != nil {
				return BootstrapStats{}, fmt.Errorf("read schema file: %w", err)
			}
		}
	} else {
		// For custom paths, always read from disk
		content, err = os.ReadFile(schemaPath)
		if err != nil {
			return BootstrapStats{}, fmt.Errorf("read schema file: %w", err)
		}
	}

	stmts := splitStatements(string(content))
	count := 0
	fts5Unavailable := false

	for _, stmt := range stmts {
		if stmt == "" {
			continue
		}
		// If FTS5 is unavailable, skip any statement that references chunks_fts.
		// This covers both the virtual table and the triggers that reference it —
		// SQLite allows creating triggers against nonexistent tables, so they must
		// be skipped proactively rather than caught on error.
		if fts5Unavailable && strings.Contains(stmt, "chunks_fts") {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if isModuleNotFoundError(err) {
				fts5Unavailable = true
				continue
			}
			if isTriggerUnsupportedError(err, stmt) {
				continue
			}
			return BootstrapStats{}, fmt.Errorf("apply schema statement: %w", err)
		}
		count++
	}

	return BootstrapStats{StatementsApplied: count}, nil
}

// MigrateSchema applies incremental schema migrations for existing databases.
// It handles adding new columns and tables that were introduced after the initial
// schema was created. This is safe to call multiple times (idempotent).
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	// Migration 1: Add target_codebase_id column to edges table if missing.
	// This column was added for cross-repository workspace support (Req 4.6).
	if err := migrateEdgesTargetCodebaseID(ctx, db); err != nil {
		return fmt.Errorf("migrate edges.target_codebase_id: %w", err)
	}

	// Migration 2: Create workspaces table if not exists (Req 3.4).
	// Handled by CREATE TABLE IF NOT EXISTS in the main schema, but we ensure
	// it exists for databases bootstrapped before workspace support was added.
	if err := migrateCreateWorkspaces(ctx, db); err != nil {
		return fmt.Errorf("migrate workspaces table: %w", err)
	}

	// Migration 3: Create workspace_members table if not exists (Req 3.5).
	if err := migrateCreateWorkspaceMembers(ctx, db); err != nil {
		return fmt.Errorf("migrate workspace_members table: %w", err)
	}

	// Migration 4: Create indexed_files table if not exists.
	if err := migrateCreateIndexedFiles(ctx, db); err != nil {
		return fmt.Errorf("migrate indexed_files table: %w", err)
	}

	// Migration 5: Add embedding_status column to chunks table (Req 1.7).
	if err := migrateChunksEmbeddingStatus(ctx, db); err != nil {
		return fmt.Errorf("migrate chunks.embedding_status: %w", err)
	}

	// Migration 6: Add index_status and status_reason columns to indexed_files (Req 4.9).
	if err := migrateIndexedFilesStatus(ctx, db); err != nil {
		return fmt.Errorf("migrate indexed_files status columns: %w", err)
	}

	// Migration 7: Create FTS5 virtual table and synchronization triggers (Req 1.1, 1.6).
	// FTS5 may not be available in all SQLite builds; gracefully skip if not supported.
	if err := migrateCreateChunksFTS(ctx, db); err != nil {
		// If FTS5 module is not available, log and continue — the search layer
		// will fall back to in-memory scan per Requirement 1.5.
		if isModuleNotFoundError(err) {
			// Non-fatal: FTS5 not compiled into this SQLite build
			_ = err
		} else {
			return fmt.Errorf("migrate chunks_fts: %w", err)
		}
	}

	// Migration 8: Add workspace/codebase scope columns to memories.
	if err := migrateMemoriesScopeColumns(ctx, db); err != nil {
		return fmt.Errorf("migrate memories scope columns: %w", err)
	}

	// Migration 9: Create codebase_meta table for per-codebase metadata.
	if err := migrateCreateCodebaseMeta(ctx, db); err != nil {
		return fmt.Errorf("migrate codebase_meta table: %w", err)
	}

	// Migration 10: Record current schema version in meta.
	if err := UpsertSchemaVersion(ctx, db); err != nil {
		return fmt.Errorf("migrate meta.schema_version: %w", err)
	}

	// Migration 11: Scope chunk_key uniqueness to (codebase_id, chunk_key).
	if err := migrateChunksKeyUniqueness(ctx, db); err != nil {
		return fmt.Errorf("migrate chunks key uniqueness: %w", err)
	}

	return nil
}

// migrateCreateCodebaseMeta ensures the codebase_meta table exists.
func migrateCreateCodebaseMeta(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS codebase_meta (
		codebase_id INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
		key         TEXT NOT NULL,
		value       TEXT NOT NULL,
		PRIMARY KEY (codebase_id, key)
	)`)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_codebase_meta_codebase ON codebase_meta(codebase_id)`)
	return err
}

// migrateEdgesTargetCodebaseID adds the target_codebase_id column to the edges
// table if it doesn't already exist. This supports cross-repo link creation.
func migrateEdgesTargetCodebaseID(ctx context.Context, db *sql.DB) error {
	exists, err := columnExists(ctx, db, "edges", "target_codebase_id")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = db.ExecContext(ctx, `ALTER TABLE edges ADD COLUMN target_codebase_id INTEGER REFERENCES codebases(id)`)
	if err != nil {
		return fmt.Errorf("alter table edges: %w", err)
	}

	// Create index for the new column
	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_edges_target_codebase ON edges(target_codebase_id)`)
	if err != nil {
		return fmt.Errorf("create index on edges.target_codebase_id: %w", err)
	}

	return nil
}

// migrateCreateWorkspaces ensures the workspaces table exists.
func migrateCreateWorkspaces(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS workspaces (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL
	)`)
	return err
}

// migrateCreateWorkspaceMembers ensures the workspace_members table exists.
func migrateCreateWorkspaceMembers(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS workspace_members (
		workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
		codebase_id  INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
		UNIQUE(workspace_id, codebase_id)
	)`)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_workspace_members_workspace ON workspace_members(workspace_id)`)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_workspace_members_codebase ON workspace_members(codebase_id)`)
	return err
}

// migrateCreateIndexedFiles ensures the indexed_files table exists.
func migrateCreateIndexedFiles(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS indexed_files (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		codebase_id INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
		file_path   TEXT NOT NULL,
		file_hash   TEXT NOT NULL,
		chunk_count INTEGER NOT NULL DEFAULT 0,
		indexed_at  INTEGER NOT NULL,
		UNIQUE(codebase_id, file_path)
	)`)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_indexed_files_codebase ON indexed_files(codebase_id)`)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_indexed_files_hash ON indexed_files(file_hash)`)
	return err
}

// splitStatements splits a SQL schema string into individual statements,
// correctly handling trigger bodies that contain semicolons within BEGIN...END blocks.
func splitStatements(content string) []string {
	var stmts []string
	var current strings.Builder
	inTrigger := false

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)

		// Detect start of a trigger (CREATE TRIGGER ... BEGIN)
		if strings.Contains(upper, "CREATE TRIGGER") {
			inTrigger = true
		}

		current.WriteString(line)
		current.WriteString("\n")

		if inTrigger {
			// End of trigger block
			if upper == "END;" || strings.HasSuffix(upper, " END;") {
				stmt := strings.TrimSpace(current.String())
				// Remove trailing semicolon for consistency with ExecContext
				stmt = strings.TrimSuffix(stmt, ";")
				if stmt != "" {
					stmts = append(stmts, stmt)
				}
				current.Reset()
				inTrigger = false
			}
		} else {
			// Normal statement: split on semicolons
			text := current.String()
			if strings.Contains(text, ";") {
				parts := strings.Split(text, ";")
				// All parts except the last are complete statements
				for _, part := range parts[:len(parts)-1] {
					stmt := strings.TrimSpace(part)
					if stmt != "" {
						stmts = append(stmts, stmt)
					}
				}
				// Keep the remainder (after last semicolon) as the start of next statement
				current.Reset()
				current.WriteString(parts[len(parts)-1])
			}
		}
	}

	// Handle any remaining content
	remaining := strings.TrimSpace(current.String())
	if remaining != "" {
		stmts = append(stmts, remaining)
	}

	return stmts
}

// columnExists checks whether a column exists in a given table using PRAGMA table_info.
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	pragmaQuery, err := tableInfoPragma(table)
	if err != nil {
		return false, err
	}
	rows, err := db.QueryContext(ctx, pragmaQuery)
	if err != nil {
		return false, fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false, fmt.Errorf("scan table_info row: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func tableInfoPragma(table string) (string, error) {
	switch table {
	case "chunks":
		return "PRAGMA table_info(chunks)", nil
	case "indexed_files":
		return "PRAGMA table_info(indexed_files)", nil
	case "edges":
		return "PRAGMA table_info(edges)", nil
	case "memories":
		return "PRAGMA table_info(memories)", nil
	default:
		return "", fmt.Errorf("unsupported table for PRAGMA table_info: %s", table)
	}
}

// migrateMemoriesScopeColumns adds workspace/codebase scope columns to memories
// and creates supporting indexes if missing.
func migrateMemoriesScopeColumns(ctx context.Context, db *sql.DB) error {
	exists, err := columnExists(ctx, db, "memories", "workspace_id")
	if err != nil {
		return err
	}
	if !exists {
		_, err = db.ExecContext(ctx, `ALTER TABLE memories ADD COLUMN workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL`)
		if err != nil {
			return fmt.Errorf("alter table memories add workspace_id: %w", err)
		}
	}

	exists, err = columnExists(ctx, db, "memories", "codebase_id")
	if err != nil {
		return err
	}
	if !exists {
		_, err = db.ExecContext(ctx, `ALTER TABLE memories ADD COLUMN codebase_id INTEGER REFERENCES codebases(id) ON DELETE SET NULL`)
		if err != nil {
			return fmt.Errorf("alter table memories add codebase_id: %w", err)
		}
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_memories_workspace_id ON memories(workspace_id)`)
	if err != nil {
		return fmt.Errorf("create index idx_memories_workspace_id: %w", err)
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_memories_codebase_id ON memories(codebase_id)`)
	if err != nil {
		return fmt.Errorf("create index idx_memories_codebase_id: %w", err)
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_memories_scope_created ON memories(workspace_id, codebase_id, created_at DESC)`)
	if err != nil {
		return fmt.Errorf("create index idx_memories_scope_created: %w", err)
	}

	return nil
}

// migrateChunksEmbeddingStatus adds the embedding_status column to the chunks table
// if it doesn't already exist. This supports the embedding pipeline status tracking.
func migrateChunksEmbeddingStatus(ctx context.Context, db *sql.DB) error {
	exists, err := columnExists(ctx, db, "chunks", "embedding_status")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = db.ExecContext(ctx, `ALTER TABLE chunks ADD COLUMN embedding_status TEXT NOT NULL DEFAULT 'complete'`)
	if err != nil {
		return fmt.Errorf("alter table chunks add embedding_status: %w", err)
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_chunks_embedding_status ON chunks(embedding_status)`)
	if err != nil {
		return fmt.Errorf("create index on chunks.embedding_status: %w", err)
	}

	return nil
}

// migrateIndexedFilesStatus adds the index_status and status_reason columns to the
// indexed_files table if they don't already exist. These support resilient parser
// degradation metadata tracking.
func migrateIndexedFilesStatus(ctx context.Context, db *sql.DB) error {
	exists, err := columnExists(ctx, db, "indexed_files", "index_status")
	if err != nil {
		return err
	}
	if !exists {
		_, err = db.ExecContext(ctx, `ALTER TABLE indexed_files ADD COLUMN index_status TEXT NOT NULL DEFAULT 'complete'`)
		if err != nil {
			return fmt.Errorf("alter table indexed_files add index_status: %w", err)
		}
	}

	exists, err = columnExists(ctx, db, "indexed_files", "status_reason")
	if err != nil {
		return err
	}
	if !exists {
		_, err = db.ExecContext(ctx, `ALTER TABLE indexed_files ADD COLUMN status_reason TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("alter table indexed_files add status_reason: %w", err)
		}
	}

	return nil
}

// isModuleNotFoundError checks if an error indicates that a SQLite module (like FTS5)
// is not available in the current build.
func isModuleNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such module") || strings.Contains(msg, "unknown module")
}

// isTriggerUnsupportedError checks if an error is caused by trigger creation failing
// because triggers are not supported (experimental feature) or because the trigger
// references a table (like chunks_fts) that doesn't exist.
func isTriggerUnsupportedError(err error, stmt string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	upperStmt := strings.ToUpper(stmt)

	// Triggers are an experimental feature in some SQLite builds (e.g., turso)
	if strings.Contains(upperStmt, "CREATE TRIGGER") && strings.Contains(msg, "experimental") {
		return true
	}

	// Trigger references a table that doesn't exist (e.g., FTS5 not available)
	if strings.Contains(stmt, "chunks_fts") &&
		(strings.Contains(msg, "no such table") || strings.Contains(msg, "no such module")) {
		return true
	}

	return false
}

// migrateCreateChunksFTS creates the FTS5 virtual table and synchronization triggers
// for full-text search over the chunks table. This is idempotent.
// Returns an error wrapping the root cause; callers should check isModuleNotFoundError
// to determine if FTS5 is simply not available.
func migrateCreateChunksFTS(ctx context.Context, db *sql.DB) error {
	// Create the FTS5 virtual table if it doesn't exist
	_, err := db.ExecContext(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
		snippet, name, file_path,
		content='chunks',
		content_rowid='id'
	)`)
	if err != nil {
		return fmt.Errorf("create chunks_fts virtual table: %w", err)
	}

	// Create synchronization triggers (IF NOT EXISTS handles idempotency).
	// Triggers may not be supported in all builds (e.g., turso requires --experimental-triggers).
	// If triggers fail, FTS5 sync will be handled at the application layer.
	triggerStmts := []struct {
		name string
		sql  string
	}{
		{
			name: "chunks_ai",
			sql: `CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
				INSERT INTO chunks_fts(rowid, snippet, name, file_path)
				VALUES (new.id, new.snippet, new.name, new.file_path);
			END`,
		},
		{
			name: "chunks_ad",
			sql: `CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
				INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
				VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
			END`,
		},
		{
			name: "chunks_au",
			sql: `CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
				INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
				VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
				INSERT INTO chunks_fts(rowid, snippet, name, file_path)
				VALUES (new.id, new.snippet, new.name, new.file_path);
			END`,
		},
	}

	for _, trig := range triggerStmts {
		if _, err := db.ExecContext(ctx, trig.sql); err != nil {
			// If triggers are experimental/unsupported, skip gracefully
			if strings.Contains(err.Error(), "experimental") {
				return nil // All triggers will fail; stop trying
			}
			return fmt.Errorf("create %s trigger: %w", trig.name, err)
		}
	}

	return nil
}

// migrateChunksKeyUniqueness adds a composite unique index on (codebase_id, chunk_key)
// to scope chunk key uniqueness per codebase rather than globally. This is idempotent.
func migrateChunksKeyUniqueness(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chunks_codebase_chunk_key ON chunks(codebase_id, chunk_key)`,
	)
	if err != nil {
		return fmt.Errorf("create idx_chunks_codebase_chunk_key: %w", err)
	}
	return nil
}
