-- name: CreateFileMetadata :one
INSERT INTO file_metadata (
    path,
    language,
    last_parsed_at,
    checksum,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, strftime('%s', 'now'), strftime('%s', 'now')
)
RETURNING *;

-- name: GetFileMetadata :one
SELECT *
FROM file_metadata
WHERE path = ? LIMIT 1;

-- name: UpdateFileMetadata :exec
UPDATE file_metadata
SET
    last_parsed_at = ?,
    checksum = ?,
    updated_at = strftime('%s', 'now')
WHERE path = ?;

-- name: DeleteFileMetadata :exec
DELETE FROM file_metadata
WHERE path = ?;

-- name: ListFileMetadata :many
SELECT *
FROM file_metadata
ORDER BY updated_at DESC;

-- name: CreateSymbol :one
INSERT INTO symbols (
    id,
    name,
    type,
    file_path,
    line,
    column,
    scope,
    visibility,
    return_type,
    parameters,
    parent_id,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'), strftime('%s', 'now')
)
RETURNING *;

-- name: GetSymbol :one
SELECT *
FROM symbols
WHERE id = ? LIMIT 1;

-- name: ListSymbolsByFile :many
SELECT *
FROM symbols
WHERE file_path = ?
ORDER BY line ASC, column ASC;

-- name: ListSymbolsByType :many
SELECT *
FROM symbols
WHERE type = ?
ORDER BY file_path ASC, line ASC;

-- name: ListSymbolsByName :many
SELECT *
FROM symbols
WHERE name = ?
ORDER BY file_path ASC, line ASC;

-- name: UpdateSymbol :exec
UPDATE symbols
SET
    name = ?,
    type = ?,
    line = ?,
    column = ?,
    scope = ?,
    visibility = ?,
    return_type = ?,
    parameters = ?,
    parent_id = ?,
    updated_at = strftime('%s', 'now')
WHERE id = ?;

-- name: DeleteSymbol :exec
DELETE FROM symbols
WHERE id = ?;

-- name: DeleteSymbolsByFile :exec
DELETE FROM symbols
WHERE file_path = ?;

-- name: CreateRelationship :one
INSERT INTO relationships (
    id,
    from_symbol_id,
    to_symbol_id,
    type,
    line,
    column,
    created_at
) VALUES (
    ?, ?, ?, ?, ?, ?, strftime('%s', 'now')
)
RETURNING *;

-- name: GetRelationship :one
SELECT *
FROM relationships
WHERE id = ? LIMIT 1;

-- name: ListRelationshipsBySymbol :many
SELECT *
FROM relationships
WHERE from_symbol_id = ? OR to_symbol_id = ?
ORDER BY created_at ASC;

-- name: ListRelationshipsByType :many
SELECT *
FROM relationships
WHERE type = ?
ORDER BY created_at ASC;

-- name: DeleteRelationship :exec
DELETE FROM relationships
WHERE id = ?;

-- name: DeleteRelationshipsBySymbol :exec
DELETE FROM relationships
WHERE from_symbol_id = ? OR to_symbol_id = ?;

-- name: ListRelationshipsByFile :many
SELECT r.*
FROM relationships r
WHERE r.from_symbol_id IN (
    SELECT s.id FROM symbols s WHERE s.file_path = ?
) OR r.to_symbol_id IN (
    SELECT s.id FROM symbols s WHERE s.file_path = ?
);

-- name: DeleteRelationshipsByFile :exec
DELETE FROM relationships
WHERE from_symbol_id IN (
    SELECT s.id FROM symbols s WHERE s.file_path = ?
) OR to_symbol_id IN (
    SELECT s.id FROM symbols s WHERE s.file_path = ?
);