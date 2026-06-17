-- name: CreateMemory :exec
INSERT INTO memories (id, content, category, workspace_id, codebase_id, created_at, source_task)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetMemoryByID :one
SELECT id, content, category, workspace_id, codebase_id, created_at, last_retrieved, retrieval_count, source_task
FROM memories
WHERE id = ?;

-- name: ListMemoriesFiltered :many
SELECT id, content, category, workspace_id, codebase_id, created_at, last_retrieved, retrieval_count, source_task
FROM memories
WHERE (:category = '' OR category = :category)
	AND (:workspace_id IS NULL OR workspace_id = :workspace_id)
	AND (:codebase_id IS NULL OR codebase_id = :codebase_id)
ORDER BY created_at DESC
LIMIT :limit;

-- name: DeleteMemoryByID :execrows
DELETE FROM memories WHERE id = ?;

-- name: UpdateMemory :execrows
UPDATE memories
SET content = ?, category = ?, workspace_id = ?, codebase_id = ?, source_task = ?
WHERE id = ?;

-- name: MarkMemoryRetrieved :execrows
UPDATE memories
SET last_retrieved = ?, retrieval_count = retrieval_count + 1
WHERE id = ?;

-- name: SearchMemoriesLexicalFiltered :many
SELECT id, content, category, workspace_id, codebase_id, created_at, last_retrieved, retrieval_count, source_task
FROM memories
WHERE (content LIKE :content OR category LIKE :category_like)
	AND (:category = '' OR category = :category)
	AND (:workspace_id IS NULL OR workspace_id = :workspace_id)
	AND (:codebase_id IS NULL OR codebase_id = :codebase_id)
ORDER BY created_at DESC
LIMIT :limit;

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
