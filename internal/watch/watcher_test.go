package watch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	internaldb "github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/observe"
	"github.com/roysland/agentdb/internal/store"
)

// countingProvider is a minimal embed.Provider that records call count
// and returns a fixed non-empty vector.
type countingProvider struct {
	calls atomic.Int64
}

func (p *countingProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	p.calls.Add(1)
	return []float32{0.1, 0.2, 0.3}, nil
}

func (p *countingProvider) ModelName() string { return "test-model" }

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

// TestWatcherReindexEmbedsChunks verifies that reindex() calls the embed
// provider for chunks (Bug 1 from the lost-branch fix: chunks were being
// stored without embeddings on every watch-triggered re-index).
func TestWatcherReindexEmbedsChunks(t *testing.T) {
	codebasePath := t.TempDir()
	if err := os.WriteFile(codebasePath+"/example.go", []byte(
		"package example\n\nfunc Hello() string { return \"hello\" }\n",
	), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	database, codebaseID := setupWatcherTestDB(t, codebasePath)
	provider := &countingProvider{}

	w := &Watcher{
		codebaseID:    codebaseID,
		codebasePath:  codebasePath,
		analyze:       false,
		embedProvider: provider,
		db:            database,
		logger:        observe.NewLogger(observe.LevelInfo, os.Stderr),
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

	embeddedCount := 0
	for _, c := range chunks {
		if len(c.Embedding) > 0 {
			embeddedCount++
		}
	}
	if embeddedCount == 0 {
		t.Errorf("no chunks have embeddings after reindex with embed provider configured (embed called %d times)", provider.calls.Load())
	}
	if provider.calls.Load() == 0 {
		t.Error("embed provider was never called during reindex")
	}
}

// TestWatcherAnalyzeEmbedsSymbols verifies that runAnalyze() calls the embed
// provider for symbols (Bug 2 from the lost-branch fix: symbol embeddings were
// silently dropped on every watch-triggered analyze pass).
func TestWatcherAnalyzeEmbedsSymbols(t *testing.T) {
	codebasePath := t.TempDir()
	if err := os.WriteFile(codebasePath+"/example.go", []byte(
		"package example\n\n// Hello returns a greeting.\nfunc Hello() string { return \"hello\" }\n\n// Goodbye returns a farewell.\nfunc Goodbye() string { return \"goodbye\" }\n",
	), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	database, codebaseID := setupWatcherTestDB(t, codebasePath)
	provider := &countingProvider{}

	w := &Watcher{
		codebaseID:    codebaseID,
		codebasePath:  codebasePath,
		analyze:       true, // enables runAnalyze after reindex
		embedProvider: provider,
		db:            database,
		logger:        observe.NewLogger(observe.LevelInfo, os.Stderr),
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

	// Check that symbols have embeddings.
	// ListWithEmbeddings returns only symbols that have an embedding vector.
	withEmbeddings, err := symbolRepo.ListWithEmbeddings(context.Background(), codebaseID, 100)
	if err != nil {
		t.Fatalf("list symbols with embeddings: %v", err)
	}
	if len(withEmbeddings) == 0 {
		t.Errorf("no symbols have embeddings after runAnalyze with embed provider configured (embed called %d times total)", provider.calls.Load())
	}
}
