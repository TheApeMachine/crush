-- +goose Up
-- +goose StatementBegin

-- File metadata table
CREATE TABLE IF NOT EXISTS file_metadata (
    path TEXT PRIMARY KEY,
    language TEXT NOT NULL,
    last_parsed_at INTEGER,
    checksum TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE TRIGGER IF NOT EXISTS update_file_metadata_updated_at
AFTER UPDATE ON file_metadata
BEGIN
    UPDATE file_metadata SET updated_at = strftime('%s', 'now')
    WHERE path = new.path;
END;

-- Symbols table
CREATE TABLE IF NOT EXISTS symbols (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    file_path TEXT NOT NULL,
    line INTEGER NOT NULL,
    column INTEGER NOT NULL,
    scope TEXT,
    visibility TEXT,
    return_type TEXT,
    parameters TEXT, -- JSON array of parameters
    parent_id TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    FOREIGN KEY (file_path) REFERENCES file_metadata (path) ON DELETE CASCADE,
    FOREIGN KEY (parent_id) REFERENCES symbols (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_symbols_file_path ON symbols (file_path);
CREATE INDEX IF NOT EXISTS idx_symbols_type ON symbols (type);
CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols (name);

CREATE TRIGGER IF NOT EXISTS update_symbols_updated_at
AFTER UPDATE ON symbols
BEGIN
    UPDATE symbols SET updated_at = strftime('%s', 'now')
    WHERE id = new.id;
END;

-- Relationships table
CREATE TABLE IF NOT EXISTS relationships (
    id TEXT PRIMARY KEY,
    from_symbol_id TEXT NOT NULL,
    to_symbol_id TEXT NOT NULL,
    type TEXT NOT NULL,
    line INTEGER NOT NULL,
    column INTEGER NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    FOREIGN KEY (from_symbol_id) REFERENCES symbols (id) ON DELETE CASCADE,
    FOREIGN KEY (to_symbol_id) REFERENCES symbols (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_relationships_from_symbol ON relationships (from_symbol_id);
CREATE INDEX IF NOT EXISTS idx_relationships_to_symbol ON relationships (to_symbol_id);
CREATE INDEX IF NOT EXISTS idx_relationships_type ON relationships (type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS update_file_metadata_updated_at;
DROP TRIGGER IF EXISTS update_symbols_updated_at;

DROP TABLE IF EXISTS relationships;
DROP TABLE IF EXISTS symbols;
DROP TABLE IF EXISTS file_metadata;
-- +goose StatementEnd