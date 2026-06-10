package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// CurrentSchemaVersion must be bumped whenever data/schema.sql changes.
const CurrentSchemaVersion = "3"

// EnsureSchemaVersionCompatible verifies that the opened database was created
// with a schema version compatible with this executable.
func EnsureSchemaVersionCompatible(ctx context.Context, db *sql.DB) error {
	got, err := readSchemaVersion(ctx, db)
	if err != nil {
		return err
	}
	if got == CurrentSchemaVersion {
		return nil
	}
	return fmt.Errorf("schema version mismatch: database=%q executable=%q; recreate database", got, CurrentSchemaVersion)
}

// UpsertSchemaVersion stores the current executable schema version in meta.
func UpsertSchemaVersion(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES ('schema_version', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		CurrentSchemaVersion,
	)
	if err != nil {
		return fmt.Errorf("upsert schema_version: %w", err)
	}
	return nil
}

func readSchemaVersion(ctx context.Context, db *sql.DB) (string, error) {
	var v string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key = 'schema_version'`,
	).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("database is missing meta.schema_version; recreate or bootstrap with current agentdb")
		}
		return "", fmt.Errorf("read meta.schema_version: %w", err)
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("database has empty meta.schema_version; recreate or bootstrap with current agentdb")
	}
	return v, nil
}
