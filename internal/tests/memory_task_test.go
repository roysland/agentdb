package tests

import (
	"context"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/db"
)

func init() {
	// Load the schema from the relative path so that db.Open() auto-bootstrap works in tests.
	content, err := os.ReadFile("../../data/schema.sql")
	if err == nil {
		db.SetEmbeddedSchema(string(content))
	}
}

func TestMemoryFlow(t *testing.T) {
	ctx := context.Background()
	tmpDB := t.TempDir() + "/test_memory.db"
	dbConn, err := db.Open(ctx, config.Runtime{
		DatabaseURL:              tmpDB,
		DatabaseDriver:           "sqlite3",
		SuppressBootstrapWarning: true,
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer dbConn.Close()

	// Add memory
	_, err = dbConn.ExecContext(ctx, `INSERT INTO memories (id, content, category, created_at) VALUES (?, ?, ?, ?)`, "1", "Test memory", "notes", 1234567890)
	if err != nil {
		t.Fatalf("failed to add memory: %v", err)
	}

	// Retrieve memory
	var content string
	err = dbConn.QueryRowContext(ctx, `SELECT content FROM memories WHERE id = ?`, "1").Scan(&content)
	if err != nil {
		t.Fatalf("failed to retrieve memory: %v", err)
	}

	if content != "Test memory" {
		t.Errorf("expected memory content 'Test memory', got '%s'", content)
	}
}
