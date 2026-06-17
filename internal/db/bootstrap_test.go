package db

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSplitStatements(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int // number of statements
	}{
		{
			name:     "simple statements",
			input:    "CREATE TABLE foo (id INTEGER);\nCREATE TABLE bar (id INTEGER);",
			expected: 2,
		},
		{
			name:     "empty input",
			input:    "",
			expected: 0,
		},
		{
			name: "trigger with semicolons inside",
			input: `CREATE TABLE chunks (id INTEGER, snippet TEXT, name TEXT, file_path TEXT);
CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, snippet, name, file_path)
    VALUES (new.id, new.snippet, new.name, new.file_path);
END;
CREATE TABLE other (id INTEGER);`,
			expected: 3,
		},
		{
			name: "multiple triggers",
			input: `CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, snippet, name, file_path)
    VALUES (new.id, new.snippet, new.name, new.file_path);
END;
CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
    VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
END;`,
			expected: 2,
		},
		{
			name: "trigger with multiple statements inside",
			input: `CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
    VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
    INSERT INTO chunks_fts(rowid, snippet, name, file_path)
    VALUES (new.id, new.snippet, new.name, new.file_path);
END;`,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := splitStatements(tt.input)
			if len(stmts) != tt.expected {
				t.Errorf("expected %d statements, got %d", tt.expected, len(stmts))
				for i, s := range stmts {
					t.Logf("  stmt[%d]: %s", i, s)
				}
			}
		})
	}
}

func TestSplitStatements_TriggerContent(t *testing.T) {
	input := `CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, snippet, name, file_path)
    VALUES (new.id, new.snippet, new.name, new.file_path);
END;`

	stmts := splitStatements(input)
	if len(stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(stmts))
	}

	// The trigger statement should contain BEGIN and END but not trailing semicolon
	stmt := stmts[0]
	if stmt == "" {
		t.Fatal("trigger statement is empty")
	}
	if len(stmt) < 10 {
		t.Fatalf("trigger statement too short: %s", stmt)
	}
	// Should contain the full trigger body
	if !contains(stmt, "BEGIN") || !contains(stmt, "END") {
		t.Errorf("trigger statement missing BEGIN/END: %s", stmt)
	}
	if !contains(stmt, "chunks_fts") {
		t.Errorf("trigger statement missing body content: %s", stmt)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestMigrateSchema_FTS5AndNewColumns(t *testing.T) {
	// Open a temporary SQLite database using the turso driver
	tmpDB := t.TempDir() + "/test_migrate.db"
	database, err := sql.Open("sqlite", tmpDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Create the base schema (without new columns/FTS5)
	baseSchema := `
		CREATE TABLE meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO meta (key, value) VALUES ('schema_version', '1');
		CREATE TABLE codebases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			root_path TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL DEFAULT '',
			indexed_at INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			codebase_id INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
			file_path TEXT NOT NULL,
			chunk_key TEXT NOT NULL UNIQUE,
			language TEXT NOT NULL,
			kind TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			signature TEXT NOT NULL DEFAULT '',
			snippet TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			file_hash TEXT NOT NULL,
			indexed_at INTEGER NOT NULL,
			embedding_model TEXT DEFAULT ''
		);
		CREATE TABLE edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			codebase_id INTEGER NOT NULL,
			from_kind TEXT NOT NULL,
			from_ref TEXT NOT NULL,
			to_kind TEXT NOT NULL,
			to_ref TEXT NOT NULL,
			edge_kind TEXT NOT NULL,
			line INTEGER NOT NULL DEFAULT 0,
			resolved INTEGER NOT NULL DEFAULT 0,
			target_codebase_id INTEGER
		);
		CREATE TABLE indexed_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			codebase_id INTEGER NOT NULL,
			file_path TEXT NOT NULL,
			file_hash TEXT NOT NULL,
			chunk_count INTEGER NOT NULL DEFAULT 0,
			indexed_at INTEGER NOT NULL,
			UNIQUE(codebase_id, file_path)
		);
		CREATE TABLE memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			embedding BLOB,
			category TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_retrieved INTEGER,
			retrieval_count INTEGER DEFAULT 0,
			source_task TEXT
		);
	`
	for _, stmt := range splitStatements(baseSchema) {
		if _, err := database.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply base schema: %v (stmt: %s)", err, stmt)
		}
	}

	// Run migrations — this should succeed even if FTS5 is not available
	if err := MigrateSchema(ctx, database); err != nil {
		t.Fatalf("MigrateSchema: %v", err)
	}

	// Verify index_status column exists on indexed_files
	exists, err := columnExists(ctx, database, "indexed_files", "index_status")
	if err != nil {
		t.Fatalf("check index_status: %v", err)
	}
	if !exists {
		t.Error("index_status column not found on indexed_files table")
	}

	// Verify status_reason column exists on indexed_files
	exists, err = columnExists(ctx, database, "indexed_files", "status_reason")
	if err != nil {
		t.Fatalf("check status_reason: %v", err)
	}
	if !exists {
		t.Error("status_reason column not found on indexed_files table")
	}

	// Verify workspace_id column exists on memories
	exists, err = columnExists(ctx, database, "memories", "workspace_id")
	if err != nil {
		t.Fatalf("check workspace_id: %v", err)
	}
	if !exists {
		t.Error("workspace_id column not found on memories table")
	}

	// Verify codebase_id column exists on memories
	exists, err = columnExists(ctx, database, "memories", "codebase_id")
	if err != nil {
		t.Fatalf("check codebase_id: %v", err)
	}
	if !exists {
		t.Error("codebase_id column not found on memories table")
	}

	// Check if FTS5 is available in this build
	fts5Available := true
	_, err = database.ExecContext(ctx, `SELECT * FROM chunks_fts LIMIT 0`)
	if err != nil {
		// FTS5 not available in this SQLite build — skip FTS5-specific assertions
		t.Logf("FTS5 not available in this build, skipping FTS5 assertions: %v", err)
		fts5Available = false
	}

	if fts5Available {
		// Verify triggers work by inserting a chunk and checking FTS5
		_, err = database.ExecContext(ctx, `INSERT INTO codebases (root_path, name, indexed_at) VALUES ('/test', 'test', 0)`)
		if err != nil {
			t.Fatalf("insert codebase: %v", err)
		}

		_, err = database.ExecContext(ctx, `INSERT INTO chunks (codebase_id, file_path, chunk_key, language, kind, name, signature, snippet, start_line, end_line, file_hash, indexed_at)
			VALUES (1, 'main.go', 'key1', 'go', 'function', 'TestFunc', 'func TestFunc()', 'func TestFunc() { return }', 1, 3, 'abc123', 1000)`)
		if err != nil {
			t.Fatalf("insert chunk: %v", err)
		}

		// Search FTS5 for the inserted chunk
		var count int
		err = database.QueryRowContext(ctx, `SELECT count(*) FROM chunks_fts WHERE chunks_fts MATCH 'TestFunc'`).Scan(&count)
		if err != nil {
			t.Fatalf("FTS5 search: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 FTS5 result, got %d", count)
		}
	}

	// Verify idempotency: run migrations again
	if err := MigrateSchema(ctx, database); err != nil {
		t.Fatalf("MigrateSchema (second run): %v", err)
	}
}

func TestTableInfoPragma_Allowlist(t *testing.T) {
	tests := []struct {
		table     string
		wantQuery string
		wantErr   bool
	}{
		{table: "chunks", wantQuery: "PRAGMA table_info(chunks)", wantErr: false},
		{table: "indexed_files", wantQuery: "PRAGMA table_info(indexed_files)", wantErr: false},
		{table: "edges", wantQuery: "PRAGMA table_info(edges)", wantErr: false},
		{table: "memories", wantQuery: "PRAGMA table_info(memories)", wantErr: false},
		{table: "codebases", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			got, err := tableInfoPragma(tt.table)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for table %s", tt.table)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for table %s: %v", tt.table, err)
			}
			if got != tt.wantQuery {
				t.Fatalf("tableInfoPragma(%s) = %q, want %q", tt.table, got, tt.wantQuery)
			}
		})
	}
}
