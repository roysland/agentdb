package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/roysland/agentdb/internal/config"
)

var autoBootstrapWarnOnce sync.Once

func Open(ctx context.Context, cfg config.Runtime) (*sql.DB, error) {
	driver := resolveDriver(cfg)
	if driver != "sqlite" {
		return nil, fmt.Errorf("unsupported driver: %s", driver)
	}

	if isLocalFilePath(cfg.DatabaseURL) {
		dir := filepath.Dir(cfg.DatabaseURL)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
	}

	db, err := sql.Open("sqlite", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Keep the CLI path aligned with MCP single-connection semantics.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	bootstrapped, err := ensureCoreSchema(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if bootstrapped && !cfg.SuppressBootstrapWarning {
		autoBootstrapWarnOnce.Do(func() {
			_, _ = fmt.Fprintln(os.Stderr, "agentdb: database schema was missing and has been auto-bootstrapped from data/schema.sql")
		})
	}

	return db, nil
}

func isLocalFilePath(dbURL string) bool {
	u := strings.TrimSpace(dbURL)
	if u == "" {
		return true
	}
	if strings.HasPrefix(u, "libsql://") || strings.HasPrefix(u, "turso://") {
		return false
	}
	return !strings.Contains(u, "://")
}

func ensureCoreSchema(ctx context.Context, db *sql.DB) (bool, error) {
	const probe = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('meta', 'codebases', 'memories')"
	var count int
	if err := db.QueryRowContext(ctx, probe).Scan(&count); err != nil {
		return false, fmt.Errorf("check schema state: %w", err)
	}

	bootstrapped := false
	if count < 3 {
		if _, err := BootstrapSchema(ctx, db, "data/schema.sql"); err != nil {
			return false, fmt.Errorf("auto-bootstrap schema: %w", err)
		}
		bootstrapped = true
	}

	if err := EnsureSchemaVersionCompatible(ctx, db); err != nil {
		return bootstrapped, err
	}

	return bootstrapped, nil
}

func resolveDriver(cfg config.Runtime) string {
	driver := strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver))
	if driver == "" || driver == "auto" || driver == "turso" {
		return "sqlite"
	}

	if driver == "sqlite" || driver == "sqlite3" {
		return "sqlite"
	}

	return driver
}
