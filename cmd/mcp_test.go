package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	internaldb "github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/store"
)

func TestMCPSearchCompactsOversizedSnippets(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite3", t.TempDir()+"/mcp_search.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	content, err := os.ReadFile("../data/schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	internaldb.SetEmbeddedSchema(string(content))

	if _, err := internaldb.BootstrapSchema(ctx, database, "data/schema.sql"); err != nil {
		t.Fatalf("bootstrap schema: %v", err)
	}
	if err := internaldb.MigrateSchema(ctx, database); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	var chunksFTSCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'chunks_fts'`).Scan(&chunksFTSCount); err != nil {
		t.Fatalf("check chunks_fts presence: %v", err)
	}
	if chunksFTSCount == 0 {
		for _, triggerName := range []string{"chunks_ai", "chunks_ad", "chunks_au"} {
			if _, err := database.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s`, triggerName)); err != nil {
				t.Fatalf("drop trigger %s: %v", triggerName, err)
			}
		}
	}

	result, err := database.ExecContext(ctx, `INSERT INTO codebases (root_path, name, indexed_at) VALUES (?, ?, ?)`, "/repo", "repo", time.Now().Unix())
	if err != nil {
		t.Fatalf("insert codebase: %v", err)
	}
	codebaseID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	chunkRepo := store.NewChunkRepo(database)
	longSnippet := strings.Repeat("todo item with enough surrounding context to inflate the MCP payload significantly. ", 30)
	for i := 0; i < 20; i++ {
		err := chunkRepo.Create(ctx, codebaseID, store.ChunkData{
			FilePath:  fmt.Sprintf("file_%02d.go", i),
			ChunkKey:  fmt.Sprintf("file_%02d:1-20", i),
			Language:  "go",
			Kind:      "function",
			Name:      fmt.Sprintf("TodoHandler%02d", i),
			Signature: "func TodoHandler(ctx context.Context, input string) error",
			Snippet:   longSnippet,
			StartLine: 1,
			EndLine:   20,
			FileHash:  fmt.Sprintf("hash-%02d", i),
			IndexedAt: time.Now().Unix(),
		})
		if err != nil {
			t.Fatalf("create chunk %d: %v", i, err)
		}
	}

	toolResult, err := mcpSearch(ctx, database, map[string]any{
		"codebase_id": codebaseID,
		"query":       "todo",
		"source":      "chunks",
		"mode":        "lexical",
		"limit":       20,
	})
	if err != nil {
		t.Fatalf("mcpSearch: %v", err)
	}

	structured, ok := toolResult["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent missing or wrong type: %T", toolResult["structuredContent"])
	}

	rawResults, ok := structured["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results missing or wrong type: %T", structured["results"])
	}
	if len(rawResults) != 20 {
		t.Fatalf("got %d results, want 20", len(rawResults))
	}

	for i, hit := range rawResults {
		snippet, _ := hit["snippet"].(string)
		if len(snippet) > mcpSearchSnippetLimit {
			t.Fatalf("result %d snippet length = %d, want <= %d", i, len(snippet), mcpSearchSnippetLimit)
		}
		if !strings.Contains(snippet, "(truncated)") {
			t.Fatalf("result %d snippet was not marked truncated", i)
		}
	}

	encoded, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if len(encoded) > 15*1024 {
		t.Fatalf("structured content size = %d bytes, want <= %d", len(encoded), 15*1024)
	}
}

func TestMCPToolTextResultSanitizesNonFiniteFloats(t *testing.T) {
	result := mcpToolTextResult(map[string]any{
		"results": []map[string]any{{
			"source":       "chunk",
			"score":        math.NaN(),
			"bm25_score":   math.Inf(1),
			"cosine_score": math.Inf(-1),
		}},
	})

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent missing or wrong type: %T", result["structuredContent"])
	}

	rawResults, ok := structured["results"].([]map[string]any)
	if !ok || len(rawResults) != 1 {
		t.Fatalf("results missing or wrong type: %T", structured["results"])
	}

	for _, key := range []string{"score", "bm25_score", "cosine_score"} {
		if value, exists := rawResults[0][key]; !exists || value != nil {
			t.Fatalf("%s = %#v, want nil after sanitization", key, value)
		}
	}

	if _, err := json.Marshal(mcpResponse{JSONRPC: "2.0", ID: 1, Result: result}); err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if err := writeMCPResponse(io.Discard, mcpResponse{JSONRPC: "2.0", ID: 1, Result: result}); err != nil {
		t.Fatalf("writeMCPResponse: %v", err)
	}
}
