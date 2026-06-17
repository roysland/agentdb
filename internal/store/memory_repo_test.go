package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestMemoryEmbeddingWriteIsBinary(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/memory_binary.db"
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			embedding BLOB,
			category TEXT NOT NULL,
			workspace_id INTEGER,
			codebase_id INTEGER,
			created_at INTEGER NOT NULL,
			last_retrieved INTEGER,
			retrieval_count INTEGER DEFAULT 0,
			source_task TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	repo := NewMemoryRepo(database)
	item := Memory{
		ID:        "m1",
		Content:   "hello",
		Category:  "notes",
		CreatedAt: 1,
		Embedding: []float32{1.25, -2.5, 3.75},
	}
	if err := repo.Create(ctx, item); err != nil {
		t.Fatalf("create memory: %v", err)
	}

	var raw []byte
	if err := database.QueryRowContext(ctx, `SELECT embedding FROM memories WHERE id = ?`, "m1").Scan(&raw); err != nil {
		t.Fatalf("select embedding: %v", err)
	}
	if len(raw) != len(item.Embedding)*4 {
		t.Fatalf("embedding blob length = %d, want %d", len(raw), len(item.Embedding)*4)
	}

	var decodedJSON []float32
	if err := json.Unmarshal(raw, &decodedJSON); err == nil && len(decodedJSON) > 0 {
		t.Fatalf("embedding blob should not be JSON-encoded")
	}

	got, err := repo.GetByID(ctx, "m1")
	if err != nil {
		t.Fatalf("get memory: %v", err)
	}
	if len(got.Embedding) != len(item.Embedding) {
		t.Fatalf("decoded embedding length = %d, want %d", len(got.Embedding), len(item.Embedding))
	}
}

func TestMemoryEmbeddingReadBackwardCompatibleJSON(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/memory_json.db"
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			embedding BLOB,
			category TEXT NOT NULL,
			workspace_id INTEGER,
			codebase_id INTEGER,
			created_at INTEGER NOT NULL,
			last_retrieved INTEGER,
			retrieval_count INTEGER DEFAULT 0,
			source_task TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	legacyEmbedding := []float32{0.5, 1.5, 2.5}
	legacyJSON, err := json.Marshal(legacyEmbedding)
	if err != nil {
		t.Fatalf("marshal legacy json: %v", err)
	}

	if _, err := database.ExecContext(ctx, `
		INSERT INTO memories(id, content, embedding, category, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, "legacy", "old", legacyJSON, "notes", 1); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	repo := NewMemoryRepo(database)
	got, err := repo.GetByID(ctx, "legacy")
	if err != nil {
		t.Fatalf("get legacy memory: %v", err)
	}
	if len(got.Embedding) != len(legacyEmbedding) {
		t.Fatalf("legacy embedding length = %d, want %d", len(got.Embedding), len(legacyEmbedding))
	}
}

func TestMemoryScopeFilteringAndBulkRetrieve(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/memory_scope.db"
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			embedding BLOB,
			category TEXT NOT NULL,
			workspace_id INTEGER,
			codebase_id INTEGER,
			created_at INTEGER NOT NULL,
			last_retrieved INTEGER,
			retrieval_count INTEGER DEFAULT 0,
			source_task TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	repo := NewMemoryRepo(database)
	now := time.Now().Unix()
	ws := int64(10)
	cb := int64(20)

	seed := []Memory{
		{ID: "a", Content: "retry login", Category: "notes", WorkspaceID: &ws, CodebaseID: &cb, CreatedAt: now},
		{ID: "b", Content: "retry budget", Category: "notes", WorkspaceID: &ws, CreatedAt: now + 1},
		{ID: "c", Content: "other scope", Category: "notes", CreatedAt: now + 2},
	}
	for _, m := range seed {
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("create memory %s: %v", m.ID, err)
		}
	}

	hits, err := repo.SearchLexical(ctx, "retry", "notes", 10, ws, 0)
	if err != nil {
		t.Fatalf("search lexical: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 scoped hits, got %d", len(hits))
	}

	list, err := repo.List(ctx, ListMemoryParams{Category: "notes", WorkspaceID: ws, CodebaseID: cb, Limit: 10})
	if err != nil {
		t.Fatalf("list scoped: %v", err)
	}
	if len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("expected only memory 'a' in workspace+codebase scope, got %+v", list)
	}

	if err := repo.MarkRetrievedMany(ctx, []string{"a", "a", "b"}, now+100); err != nil {
		t.Fatalf("mark retrieved many: %v", err)
	}

	gotA, err := repo.GetByID(ctx, "a")
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if gotA.RetrievalCount != 1 {
		t.Fatalf("memory a retrieval_count=%d, want 1", gotA.RetrievalCount)
	}

	gotB, err := repo.GetByID(ctx, "b")
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if gotB.RetrievalCount != 1 {
		t.Fatalf("memory b retrieval_count=%d, want 1", gotB.RetrievalCount)
	}
}
