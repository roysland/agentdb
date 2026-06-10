package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/observe"
)

// ConnectionHandle manages a single persistent database connection with
// application-layer write serialization and strict timeout enforcement.
type ConnectionHandle struct {
	db       *sql.DB
	dbMu     sync.RWMutex  // Guards db pointer replacement and reads
	writeSem chan struct{} // Write serializer semaphore (capacity 1)
	semOnce  sync.Once     // Lazy init for test-constructed handles
	writeTTL time.Duration // Max write operation duration (default: 5m)
	readTTL  time.Duration // Max read operation duration (default: 5s)
	mutexTTL time.Duration // Max mutex acquisition timeout (default: 3s)
	logger   *observe.Logger
	cfg      config.Runtime // Retained for reconnection
}

// NewConnectionHandle opens a single persistent connection with WAL mode and
// incremental auto-vacuum. It configures the connection with SetMaxOpenConns(1)
// and executes initialization PRAGMAs.
func NewConnectionHandle(ctx context.Context, cfg config.Runtime, logger *observe.Logger) (*ConnectionHandle, error) {
	db, err := openSingleConn(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := execPragmas(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("execute initialization PRAGMAs: %w", err)
	}

	ch := &ConnectionHandle{
		db:       db,
		writeSem: make(chan struct{}, 1),
		writeTTL: 5 * time.Minute,
		readTTL:  5 * time.Second,
		mutexTTL: 3 * time.Second,
		logger:   logger,
		cfg:      cfg,
	}

	return ch, nil
}

// ReadContext returns a context with the configured read deadline (default: 5s).
func (ch *ConnectionHandle) ReadContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, ch.readTTL)
}

// WriteContext acquires the write semaphore (with timeout) and returns a context
// with the configured write deadline (default: 5m). Returns an error if mutex
// acquisition times out. Caller MUST call ReleaseWrite after the write completes.
func (ch *ConnectionHandle) WriteContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	ch.ensureWriteSem()
	timer := time.NewTimer(ch.mutexTTL)
	defer timer.Stop()

	select {
	case ch.writeSem <- struct{}{}:
		// Semaphore acquired successfully.
		ctx, cancel := context.WithTimeout(parent, ch.writeTTL)
		return ctx, cancel, nil
	case <-timer.C:
		// Mutex acquisition timed out — log and return error
		if ch.logger != nil {
			ch.logger.Log(observe.LogEntry{
				Level:     "error",
				Operation: "write_context",
				Status:    "write_timeout",
				Error:     fmt.Sprintf("mutex acquisition timed out after %s", ch.mutexTTL),
			})
		}
		return nil, nil, fmt.Errorf("write_timeout: mutex acquisition timed out after %s", ch.mutexTTL)
	case <-parent.Done():
		// Parent context cancelled
		return nil, nil, parent.Err()
	}
}

// ReleaseWrite releases the write mutex. Must be called after WriteContext succeeds.
func (ch *ConnectionHandle) ReleaseWrite() {
	ch.ensureWriteSem()
	select {
	case <-ch.writeSem:
		return
	default:
		panic("db: ReleaseWrite called without a held write lock")
	}
}

// DB returns the underlying *sql.DB for query execution.
func (ch *ConnectionHandle) DB() *sql.DB {
	ch.dbMu.RLock()
	defer ch.dbMu.RUnlock()
	return ch.db
}

// HealthCheck pings the connection; reconnects if unhealthy.
func (ch *ConnectionHandle) HealthCheck(ctx context.Context) error {
	db := ch.DB()
	if err := db.PingContext(ctx); err != nil {
		// Log the failure
		if ch.logger != nil {
			ch.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "health_check",
				Status:    "reconnecting",
				Error:     err.Error(),
			})
		}

		// Close the failed connection
		_ = db.Close()

		// Re-open a replacement connection
		newDB, openErr := openSingleConn(ctx, ch.cfg)
		if openErr != nil {
			return fmt.Errorf("reconnect failed: %w", openErr)
		}

		if pragmaErr := execPragmas(ctx, newDB); pragmaErr != nil {
			_ = newDB.Close()
			return fmt.Errorf("reconnect PRAGMAs failed: %w", pragmaErr)
		}

		ch.dbMu.Lock()
		ch.db = newDB
		ch.dbMu.Unlock()

		if ch.logger != nil {
			ch.logger.Log(observe.LogEntry{
				Level:     "info",
				Operation: "health_check",
				Status:    "reconnected",
			})
		}
	}
	return nil
}

// Close closes the connection and releases all resources.
func (ch *ConnectionHandle) Close() error {
	ch.dbMu.Lock()
	db := ch.db
	ch.db = nil
	ch.dbMu.Unlock()

	if db != nil {
		return db.Close()
	}
	return nil
}

// EnsureSchema bootstraps the database schema if needed and applies migrations.
// This should be called after creating a ConnectionHandle to ensure the database
// is ready for use.
func (ch *ConnectionHandle) EnsureSchema(ctx context.Context) error {
	return ensureCoreSchemaOnConn(ctx, ch.DB(), ch.cfg)
}

func (ch *ConnectionHandle) ensureWriteSem() {
	ch.semOnce.Do(func() {
		if ch.writeSem == nil {
			ch.writeSem = make(chan struct{}, 1)
		}
	})
}

// ensureCoreSchemaOnConn checks and bootstraps the schema on an existing connection.
func ensureCoreSchemaOnConn(ctx context.Context, db *sql.DB, cfg config.Runtime) error {
	const probe = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('meta', 'codebases', 'memories')"
	var count int
	if err := db.QueryRowContext(ctx, probe).Scan(&count); err != nil {
		return fmt.Errorf("check schema state: %w", err)
	}

	if count < 3 {
		if _, err := BootstrapSchema(ctx, db, "data/schema.sql"); err != nil {
			return fmt.Errorf("auto-bootstrap schema: %w", err)
		}
	}

	if err := MigrateSchema(ctx, db); err != nil {
		return fmt.Errorf("apply schema migrations: %w", err)
	}

	return nil
}

// openSingleConn opens a single database connection with SetMaxOpenConns(1).
func openSingleConn(ctx context.Context, cfg config.Runtime) (*sql.DB, error) {
	driver := resolveDriver(cfg)
	if driver != "sqlite3" {
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

	db, err := sql.Open("sqlite3", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Single persistent connection — no pool
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // No expiry

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

// execPragmas executes the initialization PRAGMAs on the database connection.
func execPragmas(ctx context.Context, db *sql.DB) error {
	required := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 3000",
		"PRAGMA synchronous = NORMAL",
	}
	for _, pragma := range required {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}

	// auto_vacuum = INCREMENTAL requires the database to have been created with
	// autovacuum enabled. The Turso driver rejects this on databases created
	// without it. Treat this as best-effort: log and continue if it fails.
	if _, err := db.ExecContext(ctx, "PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
		_ = err // non-fatal: autovacuum is an optimisation, not a correctness requirement
	}

	return nil
}
