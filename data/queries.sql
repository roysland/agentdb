

-- name: RegisterCodebase :execlastid
INSERT INTO codebases (root_path, name, indexed_at)
VALUES (?, ?, ?);

-- name: ListCodebases :many
SELECT id, root_path, name, indexed_at
FROM codebases
ORDER BY id DESC;

-- name: CreateChunk :exec
INSERT INTO chunks (codebase_id, file_path, chunk_key, language, kind, name, signature, snippet, start_line, end_line, file_hash, indexed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(codebase_id, chunk_key) DO UPDATE SET
    file_path  = excluded.file_path,
    language   = excluded.language,
    kind       = excluded.kind,
    name       = excluded.name,
    signature  = excluded.signature,
    snippet    = excluded.snippet,
    start_line = excluded.start_line,
    end_line   = excluded.end_line,
    file_hash  = excluded.file_hash,
    indexed_at = excluded.indexed_at;

-- name: GetChunksByCodebase :many
SELECT id, codebase_id, file_path, chunk_key, language, kind, name, signature, snippet, start_line, end_line, file_hash, indexed_at
FROM chunks
WHERE codebase_id = ?
ORDER BY file_path, start_line;

-- name: DeleteChunksByCodebase :exec
DELETE FROM chunks WHERE codebase_id = ?;
