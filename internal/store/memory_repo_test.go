package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

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
