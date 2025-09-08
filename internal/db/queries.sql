-- name: FindSymbolByName :one
SELECT * FROM symbols WHERE name = ? LIMIT 1;

