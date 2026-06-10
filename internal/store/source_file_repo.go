package store

import (
	"context"
	"database/sql"
	"fmt"
)

// SourceFile mirrors the source_files table.
type SourceFile struct {
	ID          int64  `json:"id"`
	CodebaseID  int64  `json:"codebase_id"`
	FilePath    string `json:"file_path"`
	Language    string `json:"language"`
	PackageName string `json:"package_name,omitempty"`
	LOC         int    `json:"loc"`
	SymbolCount int    `json:"symbol_count"`
	FileHash    string `json:"file_hash"`
	IndexedAt   int64  `json:"indexed_at"`
}

type SourceFileData struct {
	FilePath    string
	Language    string
	PackageName string
	LOC         int
	SymbolCount int
	FileHash    string
	IndexedAt   int64
}

type SourceFileRepo struct{ db *sql.DB }

func NewSourceFileRepo(db *sql.DB) *SourceFileRepo { return &SourceFileRepo{db: db} }

func (r *SourceFileRepo) Upsert(ctx context.Context, codebaseID int64, d SourceFileData) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO source_files
			(codebase_id, file_path, language, package_name, loc, symbol_count, file_hash, indexed_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(codebase_id, file_path) DO UPDATE SET
			language     = excluded.language,
			package_name = excluded.package_name,
			loc          = excluded.loc,
			symbol_count = excluded.symbol_count,
			file_hash    = excluded.file_hash,
			indexed_at   = excluded.indexed_at`,
		codebaseID, d.FilePath, d.Language, d.PackageName, d.LOC, d.SymbolCount, d.FileHash, d.IndexedAt,
	)
	return err
}

func (r *SourceFileRepo) DeleteByCodebase(ctx context.Context, codebaseID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM source_files WHERE codebase_id = ?`, codebaseID)
	return err
}

// DeleteByFile removes the source_files record for a specific file in a codebase.
func (r *SourceFileRepo) DeleteByFile(ctx context.Context, codebaseID int64, filePath string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM source_files WHERE codebase_id = ? AND file_path = ?`,
		codebaseID, filePath,
	)
	return err
}

// GetHashesByCodebase returns a map of file_path -> file_hash for all source files in a codebase.
func (r *SourceFileRepo) GetHashesByCodebase(ctx context.Context, codebaseID int64) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT file_path, file_hash FROM source_files WHERE codebase_id = ?`,
		codebaseID,
	)
	if err != nil {
		return nil, fmt.Errorf("get source file hashes: %w", err)
	}
	defer rows.Close()

	hashes := make(map[string]string)
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, fmt.Errorf("scan source file hash: %w", err)
		}
		hashes[path] = hash
	}
	return hashes, rows.Err()
}

func (r *SourceFileRepo) GetByCodebase(ctx context.Context, codebaseID int64) ([]SourceFile, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, codebase_id, file_path, language, package_name, loc, symbol_count, file_hash, indexed_at
		FROM source_files WHERE codebase_id = ? ORDER BY file_path`,
		codebaseID,
	)
	if err != nil {
		return nil, fmt.Errorf("get source files: %w", err)
	}
	defer rows.Close()
	return scanSourceFiles(rows)
}

// Stats returns aggregate counts: total files, total LOC, language breakdown.
func (r *SourceFileRepo) Stats(ctx context.Context, codebaseID int64) (map[string]any, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(loc),0) FROM source_files WHERE codebase_id = ?`,
		codebaseID,
	)
	var fileCount, totalLOC int
	if err := row.Scan(&fileCount, &totalLOC); err != nil {
		return nil, err
	}

	langRows, err := r.db.QueryContext(ctx,
		`SELECT language, COUNT(*) FROM source_files WHERE codebase_id = ? GROUP BY language ORDER BY COUNT(*) DESC`,
		codebaseID,
	)
	if err != nil {
		return nil, err
	}
	defer langRows.Close()
	langs := make(map[string]int)
	for langRows.Next() {
		var lang string
		var cnt int
		if err := langRows.Scan(&lang, &cnt); err != nil {
			return nil, err
		}
		langs[lang] = cnt
	}
	return map[string]any{
		"file_count": fileCount,
		"total_loc":  totalLOC,
		"languages":  langs,
	}, langRows.Err()
}

// PackageList returns distinct package names in the codebase.
func (r *SourceFileRepo) PackageList(ctx context.Context, codebaseID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT package_name FROM source_files WHERE codebase_id = ? AND package_name != '' ORDER BY package_name`,
		codebaseID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var pkg string
		if err := rows.Scan(&pkg); err != nil {
			return nil, err
		}
		out = append(out, pkg)
	}
	return out, rows.Err()
}

func scanSourceFiles(rows *sql.Rows) ([]SourceFile, error) {
	var out []SourceFile
	for rows.Next() {
		var s SourceFile
		if err := rows.Scan(
			&s.ID, &s.CodebaseID, &s.FilePath, &s.Language,
			&s.PackageName, &s.LOC, &s.SymbolCount, &s.FileHash, &s.IndexedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
