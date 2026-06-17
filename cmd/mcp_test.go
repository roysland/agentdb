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

	_ "modernc.org/sqlite"

	internaldb "github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/store"
)

func TestMCPSearchCompactsOversizedSnippets(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite", t.TempDir()+"/mcp_search.db")
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

// TestMCPSearchResponseIsInline is the regression check for the transient
// content.json temp-file path bug: a large search response must arrive as
// inline JSON text in content[0].text, never as a file-system path.
func TestMCPSearchResponseIsInline(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite", t.TempDir()+"/mcp_inline.db")
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

	var chunksFTSCount2 int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'chunks_fts'`).Scan(&chunksFTSCount2); err != nil {
		t.Fatalf("check chunks_fts presence: %v", err)
	}
	if chunksFTSCount2 == 0 {
		for _, triggerName := range []string{"chunks_ai", "chunks_ad", "chunks_au"} {
			if _, err := database.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s`, triggerName)); err != nil {
				t.Fatalf("drop trigger %s: %v", triggerName, err)
			}
		}
	}

	result, err := database.ExecContext(ctx, `INSERT INTO codebases (root_path, name, indexed_at) VALUES (?, ?, ?)`, "/inline-repo", "inline-repo", time.Now().Unix())
	if err != nil {
		t.Fatalf("insert codebase: %v", err)
	}
	codebaseID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// Insert enough chunks with long text to produce a large response before compaction.
	chunkRepo := store.NewChunkRepo(database)
	longSnippet := strings.Repeat("x", 2000)
	for i := 0; i < 30; i++ {
		if err := chunkRepo.Create(ctx, codebaseID, store.ChunkData{
			FilePath:  fmt.Sprintf("file_%02d.go", i),
			ChunkKey:  fmt.Sprintf("file_%02d:1-50", i),
			Language:  "go",
			Kind:      "function",
			Name:      fmt.Sprintf("InlineCheck%02d", i),
			Signature: strings.Repeat("s", 800),
			Snippet:   longSnippet,
			StartLine: 1,
			EndLine:   50,
			FileHash:  fmt.Sprintf("hash-%02d", i),
			IndexedAt: time.Now().Unix(),
		}); err != nil {
			t.Fatalf("create chunk %d: %v", i, err)
		}
	}

	toolResult, err := mcpSearch(ctx, database, map[string]any{
		"codebase_id": codebaseID,
		"query":       "InlineCheck",
		"source":      "chunks",
		"mode":        "lexical",
		"limit":       30,
	})
	if err != nil {
		t.Fatalf("mcpSearch: %v", err)
	}

	// Simulate the full MCP wire path: wrap in a JSON-RPC response and marshal.
	resp := mcpResponse{JSONRPC: "2.0", ID: 1, Result: toolResult}
	var buf strings.Builder
	if err := writeMCPResponse(&buf, resp); err != nil {
		t.Fatalf("writeMCPResponse: %v", err)
	}
	wire := buf.String()

	// The wire output must be valid JSON.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(wire), &decoded); err != nil {
		t.Fatalf("wire output is not valid JSON: %v", err)
	}

	// Extract content[0].text — this is what the editor displays.
	result2, _ := decoded["result"].(map[string]any)
	contentList, _ := result2["content"].([]any)
	if len(contentList) == 0 {
		t.Fatal("content array is empty in wire response")
	}
	firstContent, _ := contentList[0].(map[string]any)
	text, _ := firstContent["text"].(string)

	if text == "" {
		t.Fatal("content[0].text is empty")
	}

	// The text must be parseable JSON, not a file-system path.
	var textDecoded any
	if err := json.Unmarshal([]byte(text), &textDecoded); err != nil {
		t.Fatalf("content[0].text is not JSON (possible temp-file path bug): %v\ntext: %.200s", err, text)
	}

	// Confirm it doesn't look like a path reference.
	if strings.HasPrefix(strings.TrimSpace(text), "/") || strings.Contains(text, "content.json") {
		t.Fatalf("content[0].text looks like a file path, not inline JSON: %.200s", text)
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
