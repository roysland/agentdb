package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/roysland/agentdb/internal/db"
)

//go:embed data/schema.sql
var schemaFS embed.FS

func init() {
	content, err := schemaFS.ReadFile("data/schema.sql")
	if err == nil {
		db.SetEmbeddedSchema(string(content))
		return
	}

	_, _ = fmt.Fprintf(os.Stderr, "agentdb: warning: embedded schema unavailable, falling back to disk reads: %v\n", err)
}
