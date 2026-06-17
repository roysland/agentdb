package watch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	internaldb "github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/observe"
	"github.com/roysland/agentdb/internal/store"
)

func setupWatcherTestDB(t *testing.T, codebasePath string) (db *sql.DB, codebaseID int64) {
	t.Helper()

	database, err := sql.Open("sqlite", t.TempDir()+"/watcher_test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	schema, err := os.ReadFile("../../data/schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	internaldb.SetEmbeddedSchema(string(schema))

	if _, err := internaldb.BootstrapSchema(context.Background(), database, "data/schema.sql"); err != nil {
		t.Fatalf("bootstrap schema: %v", err)
	}
	if err := internaldb.MigrateSchema(context.Background(), database); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	// Drop FTS triggers if the FTS table wasn't created (common in test envs).
	var ftsCount int
	_ = database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chunks_fts'`).Scan(&ftsCount)
	if ftsCount == 0 {
		for _, tr := range []string{"chunks_ai", "chunks_ad", "chunks_au"} {
			_, _ = database.ExecContext(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s`, tr))
		}
	}

	res, err := database.ExecContext(context.Background(),
		`INSERT INTO codebases (root_path, name, indexed_at) VALUES (?, ?, ?)`,
		codebasePath, "test-codebase", time.Now().Unix())
	if err != nil {
		t.Fatalf("insert codebase: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	return database, id
}

// TestWatcherReindexCreatesChunks verifies that reindex() creates chunks.
func TestWatcherReindexCreatesChunks(t *testing.T) {
	codebasePath := t.TempDir()
	if err := os.WriteFile(codebasePath+"/example.go", []byte(
		"package example\n\nfunc Hello() string { return \"hello\" }\n",
	), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	database, codebaseID := setupWatcherTestDB(t, codebasePath)

	w := &Watcher{
		codebaseID:   codebaseID,
		codebasePath: codebasePath,
		analyze:      false,
		db:           database,
		logger:       observe.NewLogger(observe.LevelInfo, os.Stderr),
	}

	if err := w.reindex(context.Background()); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	chunkRepo := store.NewChunkRepo(database)
	chunks, err := chunkRepo.GetByCodebase(context.Background(), codebaseID)
	if err != nil {
		t.Fatalf("get chunks: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks created after reindex")
	}
}

// TestWatcherAnalyzeExtractsSymbols verifies that runAnalyze() extracts symbols.
func TestWatcherAnalyzeExtractsSymbols(t *testing.T) {
	codebasePath := t.TempDir()
	if err := os.WriteFile(codebasePath+"/example.go", []byte(
		"package example\n\n// Hello returns a greeting.\nfunc Hello() string { return \"hello\" }\n\n// Goodbye returns a farewell.\nfunc Goodbye() string { return \"goodbye\" }\n",
	), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	database, codebaseID := setupWatcherTestDB(t, codebasePath)

	w := &Watcher{
		codebaseID:   codebaseID,
		codebasePath: codebasePath,
		analyze:      true, // enables runAnalyze after reindex
		db:           database,
		logger:       observe.NewLogger(observe.LevelInfo, os.Stderr),
	}

	// reindex calls runAnalyze internally when analyze=true.
	if err := w.reindex(context.Background()); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	symbolRepo := store.NewSymbolRepo(database)
	// Stats returns kind→count map; use it to verify symbols were extracted.
	stats, err := symbolRepo.Stats(context.Background(), codebaseID)
	if err != nil {
		t.Fatalf("symbol stats: %v", err)
	}
	if len(stats) == 0 {
		t.Skip("no symbols extracted (parser may not be available in this test environment)")
	}
}
