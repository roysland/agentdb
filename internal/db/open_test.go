package db

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/roysland/agentdb/internal/config"
)

func TestResolveDriver(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Runtime
		want string
	}{
		{
			name: "auto defaults to sqlite3",
			cfg:  config.Runtime{DatabaseDriver: "auto", DatabaseURL: "agentdb.sqlite"},
			want: "sqlite3",
		},
		{
			name: "explicit turso maps to sqlite3",
			cfg:  config.Runtime{DatabaseDriver: "turso", DatabaseURL: "agentdb.sqlite"},
			want: "sqlite3",
		},
		{
			name: "unsupported explicit driver is preserved for validation",
			cfg:  config.Runtime{DatabaseDriver: "postgres", DatabaseURL: "agentdb.sqlite"},
			want: "postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDriver(tt.cfg)
			if got != tt.want {
				t.Fatalf("resolveDriver() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenRejectsNewerDatabaseSchemaVersion(t *testing.T) {
	ctx := context.Background()
	if content, err := os.ReadFile("../../data/schema.sql"); err == nil {
		SetEmbeddedSchema(string(content))
	}
	dbPath := t.TempDir() + "/schema_mismatch.db"
	cfg := config.Runtime{
		DatabaseURL:              dbPath,
		DatabaseDriver:           "sqlite3",
		SuppressBootstrapWarning: true,
	}

	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("initial open failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value = '999' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("set schema_version: %v", err)
	}
	_ = db.Close()

	_, err = Open(ctx, cfg)
	if err == nil {
		t.Fatal("expected schema version mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "schema version mismatch") || !strings.Contains(err.Error(), "recreate database") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMigrateSchemaUpsertsCurrentSchemaVersion(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite3", t.TempDir()+"/migrate_upsert.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	seed := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta (key, value) VALUES ('schema_version', '1')`,
		`CREATE TABLE codebases (id INTEGER PRIMARY KEY AUTOINCREMENT, root_path TEXT NOT NULL UNIQUE, name TEXT NOT NULL DEFAULT '', indexed_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE chunks (id INTEGER PRIMARY KEY AUTOINCREMENT, codebase_id INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE, file_path TEXT NOT NULL, chunk_key TEXT NOT NULL UNIQUE, language TEXT NOT NULL, kind TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', signature TEXT NOT NULL DEFAULT '', snippet TEXT NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, file_hash TEXT NOT NULL, indexed_at INTEGER NOT NULL, embedding_model TEXT DEFAULT '')`,
		`CREATE TABLE edges (id INTEGER PRIMARY KEY AUTOINCREMENT, codebase_id INTEGER NOT NULL, from_kind TEXT NOT NULL, from_ref TEXT NOT NULL, to_kind TEXT NOT NULL, to_ref TEXT NOT NULL, edge_kind TEXT NOT NULL, line INTEGER NOT NULL DEFAULT 0, resolved INTEGER NOT NULL DEFAULT 0, target_codebase_id INTEGER)`,
		`CREATE TABLE indexed_files (id INTEGER PRIMARY KEY AUTOINCREMENT, codebase_id INTEGER NOT NULL, file_path TEXT NOT NULL, file_hash TEXT NOT NULL, chunk_count INTEGER NOT NULL DEFAULT 0, indexed_at INTEGER NOT NULL, UNIQUE(codebase_id, file_path))`,
		`CREATE TABLE memories (id TEXT PRIMARY KEY, content TEXT NOT NULL, embedding BLOB, category TEXT NOT NULL, created_at INTEGER NOT NULL, last_retrieved INTEGER, retrieval_count INTEGER DEFAULT 0, source_task TEXT)`,
	}
	for _, stmt := range seed {
		if _, err := database.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed statement failed: %v", err)
		}
	}

	if err := MigrateSchema(ctx, database); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	var got string
	if err := database.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&got); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if got != CurrentSchemaVersion {
		t.Fatalf("schema_version=%q, want %q", got, CurrentSchemaVersion)
	}
}
