package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/charmbracelet/crush/internal/treesitter"
)

// SymbolGraphStorage provides methods for persisting and retrieving symbol graphs
type SymbolGraphStorage struct {
	db *Queries
}

// NewSymbolGraphStorage creates a new symbol storage instance
func NewSymbolGraphStorage(db *Queries) *SymbolGraphStorage {
	return &SymbolGraphStorage{db: db}
}

// StoreSymbolGraph stores a complete symbol graph for a file
func (s *SymbolGraphStorage) StoreSymbolGraph(ctx context.Context, filename, language string, graph *treesitter.SymbolGraph) error {
	// Start transaction
	tx, err := s.db.db.(*sql.DB).BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.db.WithTx(tx)

	// Store or update file metadata
	checksum := "" // TODO: calculate checksum
	now := time.Now().Unix()

	// Try to create file metadata, if it fails due to unique constraint, update instead
	_, err = qtx.CreateFileMetadata(ctx, CreateFileMetadataParams{
		Path:         filename,
		Language:     language,
		LastParsedAt: sql.NullInt64{Int64: now, Valid: true},
		Checksum:     sql.NullString{String: checksum, Valid: checksum != ""},
	})
	if err != nil {
		// If creation failed, try to update existing metadata
		slog.Debug("File metadata creation failed, attempting update", "path", filename, "error", err)
		err = qtx.UpdateFileMetadata(ctx, UpdateFileMetadataParams{
			Path:         filename,
			LastParsedAt: sql.NullInt64{Int64: now, Valid: true},
			Checksum:     sql.NullString{String: checksum, Valid: checksum != ""},
		})
		if err != nil {
			return fmt.Errorf("failed to create or update file metadata: %w", err)
		}
		slog.Debug("Updated existing file metadata", "path", filename)
	} else {
		slog.Debug("Created new file metadata", "path", filename)
	}

	// Delete existing symbols and relationships for this file
	if err := qtx.DeleteRelationshipsByFile(ctx, DeleteRelationshipsByFileParams{
		FilePath:   filename,
		FilePath_2: filename,
	}); err != nil {
		return fmt.Errorf("failed to delete existing relationships: %w", err)
	}
	if err := qtx.DeleteSymbolsByFile(ctx, filename); err != nil {
		return fmt.Errorf("failed to delete existing symbols: %w", err)
	}

	// Store symbols
	symbolIDMap := make(map[string]string)   // treesitter ID to db ID
	nameToDBIDs := make(map[string][]string) // symbol name to db IDs (track duplicates)
	for _, symbol := range graph.Symbols {
		params, err := json.Marshal(symbol.Parameters)
		if err != nil {
			return fmt.Errorf("failed to marshal parameters: %w", err)
		}

		var parentID sql.NullString
		if symbol.Parent != nil {
			parentID = sql.NullString{String: symbol.Parent.ID, Valid: true}
		}

		dbSymbol, err := qtx.CreateSymbol(ctx, CreateSymbolParams{
			ID:         symbol.ID,
			Name:       symbol.Name,
			Type:       string(symbol.Type),
			FilePath:   filename,
			Line:       int64(symbol.Position.Line),
			Column:     int64(symbol.Position.Column),
			Scope:      sql.NullString{String: string(symbol.Scope), Valid: symbol.Scope != ""},
			Visibility: sql.NullString{String: string(symbol.Visibility), Valid: symbol.Visibility != ""},
			ReturnType: sql.NullString{String: symbol.ReturnType, Valid: symbol.ReturnType != ""},
			Parameters: sql.NullString{String: string(params), Valid: len(symbol.Parameters) > 0},
			ParentID:   parentID,
		})
		if err != nil {
			slog.Error("Failed to create symbol", "error", err, "symbol_id", symbol.ID, "symbol_name", symbol.Name)
			return fmt.Errorf("failed to create symbol: %w", err)
		}
		symbolIDMap[symbol.ID] = dbSymbol.ID
		// Track name -> IDs mapping to detect ambiguous names within the same file
		if symbol.Name != "" {
			nameToDBIDs[symbol.Name] = append(nameToDBIDs[symbol.Name], dbSymbol.ID)
		}
	}

	// Store relationships - only create relationships between symbols that exist in this file
	slog.Info("Storing relationships", "count", len(graph.Relationships))
	relationshipsStored := 0

	for _, rels := range graph.Relationships {
		for _, rel := range rels {
			slog.Debug("Processing relationship", "from", rel.From, "to", rel.To, "type", rel.Type)

			// Resolve symbols, preferring in-file IDs, then cross-file by name with language-aware filtering.
			fromSymbolID, fromExists := symbolIDMap[rel.From]
			if !fromExists && rel.From != "" {
				// Resolve by unique in-file name
				if ids, ok := nameToDBIDs[rel.From]; ok && len(ids) == 1 {
					fromSymbolID, fromExists = ids[0], true
					slog.Debug("Resolved 'from' by unique in-file name", "from_name", rel.From, "from_id", fromSymbolID)
				}
			}

			toSymbolID, toExists := symbolIDMap[rel.To]
			if !toExists && rel.To != "" {
				// Resolve by unique in-file name
				if ids, ok := nameToDBIDs[rel.To]; ok && len(ids) == 1 {
					toSymbolID, toExists = ids[0], true
					slog.Debug("Resolved 'to' by unique in-file name", "to_name", rel.To, "to_id", toSymbolID)
				}
			}

			// Cross-file fallback for 'to': gather candidates by name and prefer same language
			var targetIDs []string
			if !toExists && rel.To != "" {
				if candidates, err := qtx.ListSymbolsByName(ctx, rel.To); err == nil && len(candidates) > 0 {
					filtered := make([]string, 0, len(candidates))
					for _, c := range candidates {
						// Filter to same language if we can
						if meta, err := qtx.GetFileMetadata(ctx, c.FilePath); err == nil {
							if meta.Language == language {
								filtered = append(filtered, c.ID)
								continue
							}
						}
					}
					if len(filtered) == 1 {
						toSymbolID, toExists = filtered[0], true
						slog.Debug("Resolved 'to' cross-file by language+name", "to_name", rel.To, "to_id", toSymbolID)
					} else if len(filtered) > 1 {
						// Multiple plausible targets: create edges to all to avoid missing cross-file relations
						targetIDs = filtered
						slog.Debug("Resolved 'to' to multiple cross-file candidates", "to_name", rel.To, "count", len(filtered))
					} else {
						// No same-language candidates; fall back to all candidates
						for _, c := range candidates {
							targetIDs = append(targetIDs, c.ID)
						}
						if len(targetIDs) > 0 {
							slog.Debug("Resolved 'to' to multiple cross-file candidates (fallback any language)", "to_name", rel.To, "count", len(targetIDs))
						}
					}
				}
			}

			if !fromExists {
				slog.Debug("From symbol not found in current file, skipping relationship", "from", rel.From, "to", rel.To)
				continue
			}

			// Determine final set of target IDs
			if toExists {
				targetIDs = []string{toSymbolID}
			}
			if len(targetIDs) == 0 {
				slog.Debug("To symbol not found, skipping relationship", "from", rel.From, "to", rel.To)
				continue
			}

			// Create relationships for all targets
			for _, toID := range targetIDs {
				// Generate unique ID for relationship
				relID := fmt.Sprintf("%s-%s-%s-%d-%d", fromSymbolID, toID, rel.Type, rel.Position.Line, rel.Position.Column)
				_, err := qtx.CreateRelationship(ctx, CreateRelationshipParams{
					ID:           relID,
					FromSymbolID: fromSymbolID,
					ToSymbolID:   toID,
					Type:         string(rel.Type),
					Line:         int64(rel.Position.Line),
					Column:       int64(rel.Position.Column),
				})
				if err != nil {
					slog.Error("Failed to create relationship", "error", err, "from", fromSymbolID, "to", toID)
					return fmt.Errorf("failed to create relationship: %w", err)
				}
				relationshipsStored++
			}
		}
	}

	slog.Info("Relationship storage complete", "relationships_stored", relationshipsStored, "total_relationships", len(graph.Relationships))

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetUnifiedGraph retrieves a symbol graph for the entire codebase
func (s *SymbolGraphStorage) GetUnifiedGraph(ctx context.Context) (*treesitter.SymbolGraph, error) {
	graph := treesitter.NewSymbolGraph()

	files, err := s.db.ListFileMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	// Add all symbols
	for _, file := range files {
		// symbols
		dbSymbols, err := s.db.ListSymbolsByFile(ctx, file.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to list symbols for %s: %w", file.Path, err)
		}
		for _, dbSymbol := range dbSymbols {
			symbol := &treesitter.Symbol{
				ID:   dbSymbol.ID,
				Name: dbSymbol.Name,
				Type: treesitter.SymbolType(dbSymbol.Type),
				Position: treesitter.Position{
					Line:   uint32(dbSymbol.Line),
					Column: uint32(dbSymbol.Column),
				},
				Scope:      treesitter.ScopeType(dbSymbol.Scope.String),
				Visibility: treesitter.VisibilityType(dbSymbol.Visibility.String),
				ReturnType: dbSymbol.ReturnType.String,
			}
			if dbSymbol.Parameters.Valid {
				var params []treesitter.Parameter
				if err := json.Unmarshal([]byte(dbSymbol.Parameters.String), &params); err == nil {
					symbol.Parameters = params
				}
			}
			graph.AddSymbol(symbol)
		}
	}

	// Set parents after all symbols are added
	for _, file := range files {
		dbSymbols, err := s.db.ListSymbolsByFile(ctx, file.Path)
		if err != nil {
			return nil, err
		}
		for _, dbSymbol := range dbSymbols {
			if dbSymbol.ParentID.Valid {
				if symbol, exists := graph.Symbols[dbSymbol.ID]; exists {
					if parent, parentExists := graph.Symbols[dbSymbol.ParentID.String]; parentExists {
						symbol.Parent = parent
					}
				}
			}
		}
	}

	// Add relationships, de-duplicated by ID
	seen := make(map[string]struct{})
	for _, file := range files {
		rels, err := s.db.ListRelationshipsByFile(ctx, ListRelationshipsByFileParams{FilePath: file.Path, FilePath_2: file.Path})
		if err != nil {
			return nil, err
		}
		for _, dbRel := range rels {
			if _, ok := seen[dbRel.ID]; ok {
				continue
			}
			seen[dbRel.ID] = struct{}{}
			rel := treesitter.Relationship{
				From: dbRel.FromSymbolID,
				To:   dbRel.ToSymbolID,
				Type: treesitter.RelationshipType(dbRel.Type),
				Position: treesitter.Position{
					Line:   uint32(dbRel.Line),
					Column: uint32(dbRel.Column),
				},
			}
			graph.AddRelationship(rel)
		}
	}

	return graph, nil
}

// GetSymbolGraph retrieves a symbol graph for a file
func (s *SymbolGraphStorage) GetSymbolGraph(ctx context.Context, filename string) (*treesitter.SymbolGraph, error) {
	graph := treesitter.NewSymbolGraph()

	// Get symbols
	dbSymbols, err := s.db.ListSymbolsByFile(ctx, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to list symbols: %w", err)
	}

	// Convert db symbols to treesitter symbols
	for _, dbSymbol := range dbSymbols {
		symbol := &treesitter.Symbol{
			ID:   dbSymbol.ID,
			Name: dbSymbol.Name,
			Type: treesitter.SymbolType(dbSymbol.Type),
			Position: treesitter.Position{
				Line:   uint32(dbSymbol.Line),
				Column: uint32(dbSymbol.Column),
			},
			Scope:      treesitter.ScopeType(dbSymbol.Scope.String),
			Visibility: treesitter.VisibilityType(dbSymbol.Visibility.String),
			ReturnType: dbSymbol.ReturnType.String,
		}

		// Unmarshal parameters
		if dbSymbol.Parameters.Valid {
			var params []treesitter.Parameter
			if err := json.Unmarshal([]byte(dbSymbol.Parameters.String), &params); err != nil {
				return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
			}
			symbol.Parameters = params
		}

		graph.AddSymbol(symbol)
	}

	// Set parents after all symbols are added
	for _, dbSymbol := range dbSymbols {
		if dbSymbol.ParentID.Valid {
			if symbol, exists := graph.Symbols[dbSymbol.ID]; exists {
				if parent, parentExists := graph.Symbols[dbSymbol.ParentID.String]; parentExists {
					symbol.Parent = parent
				}
			}
		}
	}

	// Get relationships
	relationships, err := s.db.ListRelationshipsByFile(ctx, ListRelationshipsByFileParams{
		FilePath:   filename,
		FilePath_2: filename,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list relationships: %w", err)
	}

	// Convert db relationships to treesitter relationships
	for _, dbRel := range relationships {
		rel := treesitter.Relationship{
			From: dbRel.FromSymbolID,
			To:   dbRel.ToSymbolID,
			Type: treesitter.RelationshipType(dbRel.Type),
			Position: treesitter.Position{
				Line:   uint32(dbRel.Line),
				Column: uint32(dbRel.Column),
			},
		}
		graph.AddRelationship(rel)
	}

	return graph, nil
}

// UpdateFileMetadata updates the metadata for a file
func (s *SymbolGraphStorage) UpdateFileMetadata(ctx context.Context, filename string, checksum string) error {
	now := time.Now().Unix()
	return s.db.UpdateFileMetadata(ctx, UpdateFileMetadataParams{
		Path:         filename,
		LastParsedAt: sql.NullInt64{Int64: now, Valid: true},
		Checksum:     sql.NullString{String: checksum, Valid: true},
	})
}

// GetFileMetadata returns metadata for a file if it exists.
func (s *SymbolGraphStorage) GetFileMetadata(ctx context.Context, filename string) (FileMetadatum, error) {
	return s.db.GetFileMetadata(ctx, filename)
}

// ListFileMetadata returns all file metadata rows.
func (s *SymbolGraphStorage) ListFileMetadata(ctx context.Context) ([]FileMetadatum, error) {
	return s.db.ListFileMetadata(ctx)
}

// DeleteFileData deletes all symbols and relationships for a file
func (s *SymbolGraphStorage) DeleteFileData(ctx context.Context, filename string) error {
	// Start transaction
	tx, err := s.db.db.(*sql.DB).BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.db.WithTx(tx)

	// Delete relationships first (due to foreign keys)
	if err := qtx.DeleteRelationshipsByFile(ctx, DeleteRelationshipsByFileParams{
		FilePath:   filename,
		FilePath_2: filename,
	}); err != nil {
		return fmt.Errorf("failed to delete relationships: %w", err)
	}

	// Delete symbols
	if err := qtx.DeleteSymbolsByFile(ctx, filename); err != nil {
		return fmt.Errorf("failed to delete symbols: %w", err)
	}

	// Delete file metadata
	if err := qtx.DeleteFileMetadata(ctx, filename); err != nil {
		return fmt.Errorf("failed to delete file metadata: %w", err)
	}

	return tx.Commit()
}

// StoreRelationshipsForFile inserts relationships whose From symbols belong to the given file.
// It assumes rel.From and rel.To are already symbol IDs.
func (s *SymbolGraphStorage) StoreRelationshipsForFile(ctx context.Context, filename string, rels []treesitter.Relationship) error {
	// Start transaction
	tx, err := s.db.db.(*sql.DB).BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.db.WithTx(tx)

	// Insert relationships
	for _, rel := range rels {
		// Ensure the from symbol belongs to this file; otherwise skip to avoid cross-file ownership conflicts
		fromSym, err := qtx.GetSymbol(ctx, rel.From)
		if err != nil {
			slog.Debug("Skipping relationship with missing from symbol", "from", rel.From, "error", err)
			continue
		}
		if fromSym.FilePath != filename {
			continue
		}
		// Basic existence check for to symbol
		if _, err := qtx.GetSymbol(ctx, rel.To); err != nil {
			slog.Debug("Skipping relationship with missing to symbol", "to", rel.To, "error", err)
			continue
		}

		relID := fmt.Sprintf("%s-%s-%s-%d-%d", rel.From, rel.To, rel.Type, rel.Position.Line, rel.Position.Column)
		if _, err := qtx.CreateRelationship(ctx, CreateRelationshipParams{
			ID:           relID,
			FromSymbolID: rel.From,
			ToSymbolID:   rel.To,
			Type:         string(rel.Type),
			Line:         int64(rel.Position.Line),
			Column:       int64(rel.Position.Column),
		}); err != nil {
			slog.Debug("Failed inserting relationship", "id", relID, "error", err)
			// Continue inserting others rather than failing the batch
			continue
		}
	}

	return tx.Commit()
}

// ResetAll removes all symbol graph data from the database.
// This is useful to ensure no stale data remains across app launches.
func (s *SymbolGraphStorage) ResetAll(ctx context.Context) error {
	// Start transaction
	tx, err := s.db.db.(*sql.DB).BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Deleting from file_metadata will cascade to symbols and relationships
	if _, err := tx.ExecContext(ctx, "DELETE FROM file_metadata"); err != nil {
		return fmt.Errorf("failed to reset symbol storage: %w", err)
	}

	return tx.Commit()
}

// ListFilesWithSymbols returns all files that have symbols stored
func (s *SymbolGraphStorage) ListFilesWithSymbols(ctx context.Context) ([]interface{}, error) {
	files, err := s.db.ListFileMetadata(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]interface{}, len(files))
	for i, file := range files {
		result[i] = file
	}

	return result, nil
}
