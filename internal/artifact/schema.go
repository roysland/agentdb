package artifact

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ArtifactDDL is the schema applied to artifact files.
// It mirrors the portable analysis subset of data/schema.sql, including orientation/search
// support structures, and excludes runtime-only state like memories/tasks.
const ArtifactDDL string = `
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO meta (key, value) VALUES ('schema_version', '2');

CREATE TABLE IF NOT EXISTS codebases (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    root_path   TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL DEFAULT '',
    indexed_at  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_codebases_root_path ON codebases(root_path);
CREATE INDEX IF NOT EXISTS idx_codebases_indexed_at ON codebases(indexed_at);

CREATE TABLE IF NOT EXISTS codebase_meta (
    codebase_id INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    PRIMARY KEY (codebase_id, key)
);

CREATE INDEX IF NOT EXISTS idx_codebase_meta_codebase ON codebase_meta(codebase_id);

CREATE TABLE IF NOT EXISTS chunks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    file_path   TEXT NOT NULL,
    chunk_key   TEXT NOT NULL UNIQUE,
    language    TEXT NOT NULL,
    kind        TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    signature   TEXT NOT NULL DEFAULT '',
    snippet     TEXT NOT NULL,
    start_line  INTEGER NOT NULL,
    end_line    INTEGER NOT NULL,
    file_hash   TEXT NOT NULL,
    indexed_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_chunks_codebase_id ON chunks(codebase_id);
CREATE INDEX IF NOT EXISTS idx_chunks_codebase_file ON chunks(codebase_id, file_path);
CREATE INDEX IF NOT EXISTS idx_chunks_language ON chunks(language);
CREATE INDEX IF NOT EXISTS idx_chunks_kind ON chunks(kind);
CREATE INDEX IF NOT EXISTS idx_chunks_file_hash ON chunks(file_hash);
CREATE INDEX IF NOT EXISTS idx_chunks_indexed_at ON chunks(indexed_at);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    snippet, name, file_path,
    content='chunks',
    content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, snippet, name, file_path)
    VALUES (new.id, new.snippet, new.name, new.file_path);
END;

CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
    VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
END;

CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
    VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
    INSERT INTO chunks_fts(rowid, snippet, name, file_path)
    VALUES (new.id, new.snippet, new.name, new.file_path);
END;

CREATE TABLE IF NOT EXISTS indexed_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id   INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    file_path     TEXT NOT NULL,
    file_hash     TEXT NOT NULL,
    chunk_count   INTEGER NOT NULL DEFAULT 0,
    indexed_at    INTEGER NOT NULL,
    index_status  TEXT NOT NULL DEFAULT 'complete',
    status_reason TEXT NOT NULL DEFAULT '',
    UNIQUE(codebase_id, file_path)
);

CREATE INDEX IF NOT EXISTS idx_indexed_files_codebase ON indexed_files(codebase_id);
CREATE INDEX IF NOT EXISTS idx_indexed_files_hash ON indexed_files(file_hash);

CREATE TABLE IF NOT EXISTS symbols (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id    INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    file_path      TEXT NOT NULL,
    language       TEXT NOT NULL,
    kind           TEXT NOT NULL,
    name           TEXT NOT NULL,
    qualified_name TEXT NOT NULL,
    receiver       TEXT NOT NULL DEFAULT '',
    signature      TEXT NOT NULL DEFAULT '',
    doc_comment    TEXT NOT NULL DEFAULT '',
    visibility     TEXT NOT NULL DEFAULT '',
    body_snippet   TEXT NOT NULL DEFAULT '',
    start_line     INTEGER NOT NULL,
    end_line       INTEGER NOT NULL,
    file_hash      TEXT NOT NULL,
    indexed_at     INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_symbols_codebase ON symbols(codebase_id);
CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(codebase_id, name);
CREATE INDEX IF NOT EXISTS idx_symbols_qualified ON symbols(codebase_id, qualified_name);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(codebase_id, file_path);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(codebase_id, kind);

CREATE TABLE IF NOT EXISTS source_files (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id     INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    file_path       TEXT NOT NULL,
    language        TEXT NOT NULL,
    package_name    TEXT NOT NULL DEFAULT '',
    loc             INTEGER NOT NULL DEFAULT 0,
    symbol_count    INTEGER NOT NULL DEFAULT 0,
    file_hash       TEXT NOT NULL,
    indexed_at      INTEGER NOT NULL,
    UNIQUE(codebase_id, file_path)
);

CREATE INDEX IF NOT EXISTS idx_source_files_codebase ON source_files(codebase_id);
CREATE INDEX IF NOT EXISTS idx_source_files_package ON source_files(codebase_id, package_name);

CREATE TABLE IF NOT EXISTS edges (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id        INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    from_kind          TEXT NOT NULL,
    from_ref           TEXT NOT NULL,
    to_kind            TEXT NOT NULL,
    to_ref             TEXT NOT NULL,
    edge_kind          TEXT NOT NULL,
    line               INTEGER NOT NULL DEFAULT 0,
    resolved           INTEGER NOT NULL DEFAULT 0,
    target_codebase_id INTEGER REFERENCES codebases(id)
);

CREATE INDEX IF NOT EXISTS idx_edges_codebase ON edges(codebase_id);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(codebase_id, from_ref);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(codebase_id, to_ref);
CREATE INDEX IF NOT EXISTS idx_edges_kind ON edges(codebase_id, edge_kind);
CREATE INDEX IF NOT EXISTS idx_edges_target_codebase ON edges(target_codebase_id);

CREATE TABLE IF NOT EXISTS workspaces (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS workspace_members (
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    codebase_id  INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    UNIQUE(workspace_id, codebase_id)
);

CREATE INDEX IF NOT EXISTS idx_workspace_members_workspace ON workspace_members(workspace_id);
CREATE INDEX IF NOT EXISTS idx_workspace_members_codebase ON workspace_members(codebase_id);
`

// SupportedSchemaVersions lists versions this CLI can import.
var SupportedSchemaVersions = []string{"1", "2"}

func applyArtifactSchema(ctx context.Context, db *sql.DB) error {
	stmts := splitArtifactStatements(ArtifactDDL)
	ftsUnavailable := false

	for _, stmt := range stmts {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}

		if ftsUnavailable && strings.Contains(trimmed, "chunks_fts") {
			continue
		}

		_, err := db.ExecContext(ctx, trimmed)
		if err == nil {
			continue
		}

		if strings.Contains(trimmed, "USING fts5") && isArtifactFTS5Unavailable(err) {
			ftsUnavailable = true
			continue
		}

		if ftsUnavailable && strings.Contains(trimmed, "chunks_fts") {
			continue
		}

		return fmt.Errorf("apply artifact schema statement: %w", err)
	}

	return nil
}

func isArtifactFTS5Unavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such module: fts5") || strings.Contains(msg, "unknown module: fts5")
}

func splitArtifactStatements(schema string) []string {
	var stmts []string
	var current strings.Builder

	inTrigger := false
	for _, line := range strings.Split(schema, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "--") {
			continue
		}

		if strings.HasPrefix(strings.ToUpper(trimmed), "CREATE TRIGGER") {
			inTrigger = true
		}

		current.WriteString(line)
		current.WriteString("\n")

		if inTrigger {
			if trimmed == "END;" {
				stmt := strings.TrimSpace(current.String())
				stmt = strings.TrimSuffix(stmt, ";")
				if stmt != "" {
					stmts = append(stmts, stmt)
				}
				current.Reset()
				inTrigger = false
			}
			continue
		}

		text := current.String()
		if strings.Contains(text, ";") {
			parts := strings.Split(text, ";")
			for _, part := range parts[:len(parts)-1] {
				stmt := strings.TrimSpace(part)
				if stmt != "" {
					stmts = append(stmts, stmt)
				}
			}
			current.Reset()
			current.WriteString(parts[len(parts)-1])
		}
	}

	remaining := strings.TrimSpace(current.String())
	if remaining != "" {
		stmts = append(stmts, remaining)
	}

	return stmts
}
