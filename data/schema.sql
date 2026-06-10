CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO meta (key, value) VALUES ('schema_version', '3');

CREATE TABLE IF NOT EXISTS codebases (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    root_path   TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL DEFAULT '',
    indexed_at  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_codebases_root_path ON codebases(root_path);
CREATE INDEX IF NOT EXISTS idx_codebases_indexed_at ON codebases(indexed_at);

CREATE TABLE IF NOT EXISTS codebase_meta (
    codebase_id INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    PRIMARY KEY (codebase_id, key)
);

CREATE INDEX IF NOT EXISTS idx_codebase_meta_codebase ON codebase_meta(codebase_id);

CREATE TABLE IF NOT EXISTS chunks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id      INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    file_path        TEXT NOT NULL,
    chunk_key        TEXT NOT NULL,
    language         TEXT NOT NULL,
    kind             TEXT NOT NULL,
    name             TEXT NOT NULL DEFAULT '',
    signature        TEXT NOT NULL DEFAULT '',
    snippet          TEXT NOT NULL,
    start_line       INTEGER NOT NULL,
    end_line         INTEGER NOT NULL,
    file_hash        TEXT NOT NULL,
    indexed_at       INTEGER NOT NULL,
    embedding        F8_BLOB(384),
    embedding_model  TEXT DEFAULT '',
    embedding_status TEXT NOT NULL DEFAULT 'complete',
    UNIQUE(codebase_id, chunk_key)
);

CREATE INDEX IF NOT EXISTS idx_chunks_codebase_id ON chunks(codebase_id);
CREATE INDEX IF NOT EXISTS idx_chunks_codebase_file ON chunks(codebase_id, file_path);
CREATE INDEX IF NOT EXISTS idx_chunks_language ON chunks(language);
CREATE INDEX IF NOT EXISTS idx_chunks_kind ON chunks(kind);
CREATE INDEX IF NOT EXISTS idx_chunks_file_hash ON chunks(file_hash);
CREATE INDEX IF NOT EXISTS idx_chunks_indexed_at ON chunks(indexed_at);
CREATE INDEX IF NOT EXISTS idx_chunks_embedding_status ON chunks(embedding_status);

-- FTS5 virtual table for full-text search over chunks
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    snippet, name, file_path,
    content='chunks',
    content_rowid='id'
);

-- Synchronization triggers to keep FTS5 index in sync with chunks table
CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, snippet, name, file_path)
    VALUES (new.id, new.snippet, new.name, new.file_path);
END;

CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
    VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
END;

CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, snippet, name, file_path)
    VALUES ('delete', old.id, old.snippet, old.name, old.file_path);
    INSERT INTO chunks_fts(rowid, snippet, name, file_path)
    VALUES (new.id, new.snippet, new.name, new.file_path);
END;

CREATE TABLE IF NOT EXISTS indexed_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id   INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    file_path     TEXT NOT NULL,
    file_hash     TEXT NOT NULL,
    chunk_count   INTEGER NOT NULL DEFAULT 0,
    indexed_at    INTEGER NOT NULL,
    index_status  TEXT NOT NULL DEFAULT 'complete',
    status_reason TEXT NOT NULL DEFAULT '',
    UNIQUE(codebase_id, file_path)
);

CREATE INDEX IF NOT EXISTS idx_indexed_files_codebase ON indexed_files(codebase_id);
CREATE INDEX IF NOT EXISTS idx_indexed_files_hash ON indexed_files(file_hash);

-- Project database: AST-extracted symbols
CREATE TABLE IF NOT EXISTS symbols (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id     INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    file_path       TEXT NOT NULL,
    language        TEXT NOT NULL,
    kind            TEXT NOT NULL,
    name            TEXT NOT NULL,
    qualified_name  TEXT NOT NULL,
    receiver        TEXT NOT NULL DEFAULT '',
    signature       TEXT NOT NULL DEFAULT '',
    doc_comment     TEXT NOT NULL DEFAULT '',
    visibility      TEXT NOT NULL DEFAULT '',
    body_snippet    TEXT NOT NULL DEFAULT '',
    start_line      INTEGER NOT NULL,
    end_line        INTEGER NOT NULL,
    file_hash       TEXT NOT NULL,
    indexed_at      INTEGER NOT NULL,
    embedding       F8_BLOB(384),
    embedding_model TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_symbols_codebase ON symbols(codebase_id);
CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(codebase_id, name);
CREATE INDEX IF NOT EXISTS idx_symbols_qualified ON symbols(codebase_id, qualified_name);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(codebase_id, file_path);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(codebase_id, kind);

-- Project database: file-level metadata
CREATE TABLE IF NOT EXISTS source_files (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id     INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    file_path       TEXT NOT NULL,
    language        TEXT NOT NULL,
    package_name    TEXT NOT NULL DEFAULT '',
    loc             INTEGER NOT NULL DEFAULT 0,
    symbol_count    INTEGER NOT NULL DEFAULT 0,
    file_hash       TEXT NOT NULL,
    indexed_at      INTEGER NOT NULL,
    UNIQUE(codebase_id, file_path)
);

CREATE INDEX IF NOT EXISTS idx_source_files_codebase ON source_files(codebase_id);
CREATE INDEX IF NOT EXISTS idx_source_files_package ON source_files(codebase_id, package_name);

-- Project database: directed relationship edges (imports, calls, uses_type, etc.)
CREATE TABLE IF NOT EXISTS edges (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    codebase_id       INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    from_kind         TEXT NOT NULL,
    from_ref          TEXT NOT NULL,
    to_kind           TEXT NOT NULL,
    to_ref            TEXT NOT NULL,
    edge_kind         TEXT NOT NULL,
    line              INTEGER NOT NULL DEFAULT 0,
    resolved          INTEGER NOT NULL DEFAULT 0,
    target_codebase_id INTEGER REFERENCES codebases(id)
);

CREATE INDEX IF NOT EXISTS idx_edges_codebase ON edges(codebase_id);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(codebase_id, from_ref);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(codebase_id, to_ref);
CREATE INDEX IF NOT EXISTS idx_edges_kind ON edges(codebase_id, edge_kind);
CREATE INDEX IF NOT EXISTS idx_edges_target_codebase ON edges(target_codebase_id);

-- Cross-repository workspaces: logical grouping of codebases
CREATE TABLE IF NOT EXISTS workspaces (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

-- Workspace membership (many-to-many)
CREATE TABLE IF NOT EXISTS workspace_members (
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    codebase_id  INTEGER NOT NULL REFERENCES codebases(id) ON DELETE CASCADE,
    UNIQUE(workspace_id, codebase_id)
);

CREATE INDEX IF NOT EXISTS idx_workspace_members_workspace ON workspace_members(workspace_id);
CREATE INDEX IF NOT EXISTS idx_workspace_members_codebase ON workspace_members(codebase_id);

CREATE TABLE IF NOT EXISTS memories (
    id              TEXT PRIMARY KEY,
    content         TEXT NOT NULL,
    embedding       F8_BLOB(384),
    category        TEXT NOT NULL,
    workspace_id    INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
    codebase_id     INTEGER REFERENCES codebases(id) ON DELETE SET NULL,
    created_at      INTEGER NOT NULL,
    last_retrieved  INTEGER,
    retrieval_count INTEGER DEFAULT 0,
    source_task     TEXT
);

CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category);
CREATE INDEX IF NOT EXISTS idx_memories_created_at ON memories(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_category_created ON memories(category, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_source_task ON memories(source_task);
CREATE INDEX IF NOT EXISTS idx_memories_workspace_id ON memories(workspace_id);
CREATE INDEX IF NOT EXISTS idx_memories_codebase_id ON memories(codebase_id);
CREATE INDEX IF NOT EXISTS idx_memories_scope_created ON memories(workspace_id, codebase_id, created_at DESC);
