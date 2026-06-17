package artifact

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBuildAttachDatabaseSQL_EscapesSingleQuotes(t *testing.T) {
	path := "/tmp/it's-agentdb.db"
	sqlText := buildAttachDatabaseSQL(path)
	if sqlText != "ATTACH DATABASE '/tmp/it''s-agentdb.db' AS artifact" {
		t.Fatalf("unexpected attach SQL: %s", sqlText)
	}
}

func TestExportImport_PreservesTargetCodebaseID(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	srcDB, err := sql.Open("sqlite", tmp+"/src.db")
	if err != nil {
		t.Fatalf("open src db: %v", err)
	}
	defer srcDB.Close()
	if err := createMainSchemaForArtifactTests(ctx, srcDB); err != nil {
		t.Fatalf("create src schema: %v", err)
	}

	if err := seedMainDBForArtifactTests(ctx, srcDB); err != nil {
		t.Fatalf("seed src db: %v", err)
	}

	artifactPath := tmp + "/out.agentdb"
	if err := Export(ctx, srcDB, ExportOptions{
		CodebaseID:        1,
		OutputPath:        artifactPath,
		IncludeEmbeddings: false,
	}); err != nil {
		if strings.Contains(err.Error(), "ATTACH is an experimental feature") {
			t.Skip("driver does not support ATTACH in this test runtime")
		}
		t.Fatalf("export: %v", err)
	}

	artifactDB, err := sql.Open("sqlite", artifactPath)
	if err != nil {
		t.Fatalf("open artifact db: %v", err)
	}
	defer artifactDB.Close()

	var exportedTarget sql.NullInt64
	if err := artifactDB.QueryRowContext(ctx, "SELECT target_codebase_id FROM edges LIMIT 1").Scan(&exportedTarget); err != nil {
		t.Fatalf("read exported edge: %v", err)
	}
	if !exportedTarget.Valid || exportedTarget.Int64 != 42 {
		t.Fatalf("expected exported target_codebase_id=42, got %#v", exportedTarget)
	}

	dstDB, err := sql.Open("sqlite", tmp+"/dst.db")
	if err != nil {
		t.Fatalf("open dst db: %v", err)
	}
	defer dstDB.Close()
	if err := createMainSchemaForArtifactTests(ctx, dstDB); err != nil {
		t.Fatalf("create dst schema: %v", err)
	}

	if err := Import(ctx, dstDB, ImportOptions{ArtifactPath: artifactPath}); err != nil {
		t.Fatalf("import: %v", err)
	}

	var importedTarget sql.NullInt64
	if err := dstDB.QueryRowContext(ctx, "SELECT target_codebase_id FROM edges LIMIT 1").Scan(&importedTarget); err != nil {
		t.Fatalf("read imported edge: %v", err)
	}
	if !importedTarget.Valid || importedTarget.Int64 != 42 {
		t.Fatalf("expected imported target_codebase_id=42, got %#v", importedTarget)
	}

	var closedSource string
	if err := dstDB.QueryRowContext(ctx, "SELECT value FROM codebase_meta WHERE codebase_id = 1 AND key = 'closed_source'").Scan(&closedSource); err != nil {
		t.Fatalf("read imported codebase metadata: %v", err)
	}
	if closedSource != "false" {
		t.Fatalf("expected imported closed_source=false, got %q", closedSource)
	}
}

func TestArtifactDDL_IncludesWave4SchemaParity(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", t.TempDir()+"/artifact.db")
	if err != nil {
		t.Fatalf("open artifact db: %v", err)
	}
	defer db.Close()

	if err := applyArtifactSchema(ctx, db); err != nil {
		t.Fatalf("apply artifact ddl: %v", err)
	}

	assertHasColumn(t, ctx, db, "chunks", "embedding_status")
	assertHasColumn(t, ctx, db, "indexed_files", "index_status")
	assertHasColumn(t, ctx, db, "indexed_files", "status_reason")
	assertHasColumn(t, ctx, db, "edges", "target_codebase_id")
	if fts5AvailableInDB(t, ctx, db) {
		assertTableExists(t, ctx, db, "chunks_fts")
	}
	assertTableExists(t, ctx, db, "workspaces")
	assertTableExists(t, ctx, db, "workspace_members")
	assertTableExists(t, ctx, db, "codebase_meta")
}

func TestExport_StripSourceBehavior(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	srcDB, err := sql.Open("sqlite", tmp+"/src.db")
	if err != nil {
		t.Fatalf("open src db: %v", err)
	}
	defer srcDB.Close()
	if err := createMainSchemaForArtifactTests(ctx, srcDB); err != nil {
		t.Fatalf("create src schema: %v", err)
	}
	if err := seedMainDBForArtifactTests(ctx, srcDB); err != nil {
		t.Fatalf("seed src db: %v", err)
	}

	t.Run("strip_source_disabled", func(t *testing.T) {
		artifactPath := tmp + "/out-keep.agentdb"
		err := Export(ctx, srcDB, ExportOptions{
			CodebaseID:        1,
			OutputPath:        artifactPath,
			IncludeEmbeddings: false,
			StripSource:       false,
		})
		if err != nil {
			if strings.Contains(err.Error(), "ATTACH is an experimental feature") {
				t.Skip("driver does not support ATTACH in this test runtime")
			}
			t.Fatalf("export keep source: %v", err)
		}

		artifactDB, err := sql.Open("sqlite", artifactPath)
		if err != nil {
			t.Fatalf("open artifact db: %v", err)
		}
		defer artifactDB.Close()

		var chunkSnippet string
		if err := artifactDB.QueryRowContext(ctx, "SELECT snippet FROM chunks LIMIT 1").Scan(&chunkSnippet); err != nil {
			t.Fatalf("read chunk snippet: %v", err)
		}
		if chunkSnippet == "" {
			t.Fatalf("expected chunk snippet to be preserved")
		}

		var docComment string
		var bodySnippet string
		if err := artifactDB.QueryRowContext(ctx, "SELECT doc_comment, body_snippet FROM symbols LIMIT 1").Scan(&docComment, &bodySnippet); err != nil {
			t.Fatalf("read symbol snippets: %v", err)
		}
		if docComment == "" {
			t.Fatalf("expected doc_comment to be preserved")
		}
		if bodySnippet == "" {
			t.Fatalf("expected body_snippet to be preserved")
		}
	})

	t.Run("strip_source_enabled", func(t *testing.T) {
		artifactPath := tmp + "/out-strip.agentdb"
		err := Export(ctx, srcDB, ExportOptions{
			CodebaseID:        1,
			OutputPath:        artifactPath,
			IncludeEmbeddings: false,
			StripSource:       true,
		})
		if err != nil {
			if strings.Contains(err.Error(), "ATTACH is an experimental feature") {
				t.Skip("driver does not support ATTACH in this test runtime")
			}
			t.Fatalf("export strip source: %v", err)
		}

		artifactDB, err := sql.Open("sqlite", artifactPath)
		if err != nil {
			t.Fatalf("open artifact db: %v", err)
		}
		defer artifactDB.Close()

		var chunkSnippet string
		if err := artifactDB.QueryRowContext(ctx, "SELECT snippet FROM chunks LIMIT 1").Scan(&chunkSnippet); err != nil {
			t.Fatalf("read chunk snippet: %v", err)
		}
		if chunkSnippet != "" {
			t.Fatalf("expected chunk snippet to be stripped, got %q", chunkSnippet)
		}

		var docComment string
		var bodySnippet string
		if err := artifactDB.QueryRowContext(ctx, "SELECT doc_comment, body_snippet FROM symbols LIMIT 1").Scan(&docComment, &bodySnippet); err != nil {
			t.Fatalf("read symbol snippets: %v", err)
		}
		if docComment != "" {
			t.Fatalf("expected doc_comment to be stripped, got %q", docComment)
		}
		if bodySnippet != "" {
			t.Fatalf("expected body_snippet to be stripped, got %q", bodySnippet)
		}

		var closedSource string
		var sourceStripped string
		if err := artifactDB.QueryRowContext(ctx, "SELECT value FROM codebase_meta WHERE codebase_id = 1 AND key = 'closed_source'").Scan(&closedSource); err != nil {
			t.Fatalf("read codebase_meta closed_source: %v", err)
		}
		if err := artifactDB.QueryRowContext(ctx, "SELECT value FROM codebase_meta WHERE codebase_id = 1 AND key = 'source_stripped'").Scan(&sourceStripped); err != nil {
			t.Fatalf("read codebase_meta source_stripped: %v", err)
		}
		if closedSource != "true" {
			t.Fatalf("expected closed_source=true, got %q", closedSource)
		}
		if sourceStripped != "true" {
			t.Fatalf("expected source_stripped=true, got %q", sourceStripped)
		}
	})
}

func fts5AvailableInDB(t *testing.T, ctx context.Context, db *sql.DB) bool {
	t.Helper()
	_, err := db.ExecContext(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS __agentdb_fts_probe USING fts5(content)`)
	if err != nil {
		return false
	}
	_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS __agentdb_fts_probe`)
	return true
}

func createMainSchemaForArtifactTests(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS codebases (
	id INTEGER PRIMARY KEY,
	root_path TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	indexed_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS codebase_meta (
	codebase_id INTEGER NOT NULL,
	key TEXT NOT NULL,
	value TEXT NOT NULL,
	PRIMARY KEY (codebase_id, key)
);
CREATE TABLE IF NOT EXISTS chunks (
	id INTEGER PRIMARY KEY,
	codebase_id INTEGER NOT NULL,
	file_path TEXT NOT NULL,
	chunk_key TEXT NOT NULL,
	language TEXT NOT NULL,
	kind TEXT NOT NULL,
	name TEXT NOT NULL,
	signature TEXT NOT NULL,
	snippet TEXT NOT NULL,
	start_line INTEGER NOT NULL,
	end_line INTEGER NOT NULL,
	file_hash TEXT NOT NULL,
	indexed_at INTEGER NOT NULL,
	embedding BLOB,
	embedding_model TEXT NOT NULL,
	embedding_status TEXT NOT NULL,
	UNIQUE(codebase_id, chunk_key)
);
CREATE TABLE IF NOT EXISTS indexed_files (
	id INTEGER PRIMARY KEY,
	codebase_id INTEGER NOT NULL,
	file_path TEXT NOT NULL,
	file_hash TEXT NOT NULL,
	chunk_count INTEGER NOT NULL,
	indexed_at INTEGER NOT NULL,
	index_status TEXT NOT NULL,
	status_reason TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS symbols (
	id INTEGER PRIMARY KEY,
	codebase_id INTEGER NOT NULL,
	file_path TEXT NOT NULL,
	language TEXT NOT NULL,
	kind TEXT NOT NULL,
	name TEXT NOT NULL,
	qualified_name TEXT NOT NULL,
	receiver TEXT NOT NULL,
	signature TEXT NOT NULL,
	doc_comment TEXT NOT NULL,
	visibility TEXT NOT NULL,
	body_snippet TEXT NOT NULL,
	start_line INTEGER NOT NULL,
	end_line INTEGER NOT NULL,
	file_hash TEXT NOT NULL,
	indexed_at INTEGER NOT NULL,
	embedding BLOB,
	embedding_model TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS source_files (
	id INTEGER PRIMARY KEY,
	codebase_id INTEGER NOT NULL,
	file_path TEXT NOT NULL,
	language TEXT NOT NULL,
	package_name TEXT NOT NULL,
	loc INTEGER NOT NULL,
	symbol_count INTEGER NOT NULL,
	file_hash TEXT NOT NULL,
	indexed_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS edges (
	id INTEGER PRIMARY KEY,
	codebase_id INTEGER NOT NULL,
	from_kind TEXT NOT NULL,
	from_ref TEXT NOT NULL,
	to_kind TEXT NOT NULL,
	to_ref TEXT NOT NULL,
	edge_kind TEXT NOT NULL,
	line INTEGER NOT NULL,
	resolved INTEGER NOT NULL,
	target_codebase_id INTEGER
);
`)
	return err
}

func seedMainDBForArtifactTests(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO codebases(id, root_path, name, indexed_at) VALUES (1, '/repo/a', 'a', 1);
INSERT INTO chunks(id, codebase_id, file_path, chunk_key, language, kind, name, signature, snippet, start_line, end_line, file_hash, indexed_at, embedding, embedding_model, embedding_status)
VALUES (1, 1, 'main.go', 'main.go:1-1', 'go', 'function', 'main', 'func main()', 'func main(){}', 1, 1, 'h1', 1, NULL, '', 'complete');
INSERT INTO indexed_files(id, codebase_id, file_path, file_hash, chunk_count, indexed_at, index_status, status_reason)
VALUES (1, 1, 'main.go', 'h1', 1, 1, 'complete', '');
INSERT INTO symbols(id, codebase_id, file_path, language, kind, name, qualified_name, receiver, signature, doc_comment, visibility, body_snippet, start_line, end_line, file_hash, indexed_at, embedding, embedding_model)
VALUES (1, 1, 'main.go', 'go', 'function', 'main', 'main', '', 'func main()', 'main entry point', 'public', 'func main(){}', 1, 1, 'h1', 1, NULL, '');
INSERT INTO source_files(id, codebase_id, file_path, language, package_name, loc, symbol_count, file_hash, indexed_at)
VALUES (1, 1, 'main.go', 'go', 'main', 1, 1, 'h1', 1);
INSERT INTO edges(id, codebase_id, from_kind, from_ref, to_kind, to_ref, edge_kind, line, resolved, target_codebase_id)
VALUES (1, 1, 'symbol', 'main', 'symbol', 'other', 'calls', 1, 1, 42);
`)
	return err
}

func assertHasColumn(t *testing.T, ctx context.Context, db *sql.DB, table, column string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		if name == column {
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info(%s): %v", table, err)
	}
	t.Fatalf("missing column %s.%s", table, column)
}

func assertTableExists(t *testing.T, ctx context.Context, db *sql.DB, table string) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	if count == 0 {
		t.Fatalf("missing table %s", table)
	}
}
