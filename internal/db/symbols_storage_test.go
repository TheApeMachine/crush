package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/charmbracelet/crush/internal/treesitter"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDatabase runs migrations on an in-memory database
func setupTestDatabase(db *sql.DB) error {
	// Set dialect
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}

	// Run migrations
	goose.SetBaseFS(FS)
	return goose.Up(db, "migrations")
}

func TestSymbolGraphStorage_StoreAndGetSymbolGraph(t *testing.T) {
	// Setup in-memory SQLite database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Run migrations manually for in-memory database
	require.NoError(t, setupTestDatabase(db))

	// Create queries
	queries := New(db)
	storage := NewSymbolGraphStorage(queries)

	// Create a test symbol graph
	graph := treesitter.NewSymbolGraph()

	// Add a function symbol
	funcSymbol := &treesitter.Symbol{
		ID:   "test.go:main:1:1",
		Name: "main",
		Type: treesitter.SymbolTypeFunction,
		Position: treesitter.Position{
			Line:   1,
			Column: 1,
		},
		Scope:      treesitter.ScopeGlobal,
		Visibility: treesitter.VisibilityPublic,
		ReturnType: "",
		Parameters: []treesitter.Parameter{
			{Name: "args", Type: "[]string"},
		},
	}

	// Add a variable symbol
	varSymbol := &treesitter.Symbol{
		ID:   "test.go:globalVar:2:1",
		Name: "globalVar",
		Type: treesitter.SymbolTypeVariable,
		Position: treesitter.Position{
			Line:   2,
			Column: 1,
		},
		Scope:      treesitter.ScopeGlobal,
		Visibility: treesitter.VisibilityPublic,
		ReturnType: "int",
	}

	// Set parent relationship
	varSymbol.Parent = funcSymbol

	graph.AddSymbol(funcSymbol)
	graph.AddSymbol(varSymbol)

	// Add a relationship
	rel := treesitter.Relationship{
		From: funcSymbol.ID,
		To:   varSymbol.ID,
		Type: treesitter.RelationshipTypeReference,
		Position: treesitter.Position{
			Line:   3,
			Column: 5,
		},
	}
	graph.AddRelationship(rel)

	// Store the graph
	err = storage.StoreSymbolGraph(context.Background(), "test.go", "go", graph)
	require.NoError(t, err)

	// Retrieve the graph
	retrievedGraph, err := storage.GetSymbolGraph(context.Background(), "test.go")
	require.NoError(t, err)
	require.NotNil(t, retrievedGraph)

	// Verify symbols
	assert.Len(t, retrievedGraph.Symbols, 2)

	retrievedFunc, exists := retrievedGraph.Symbols[funcSymbol.ID]
	require.True(t, exists)
	assert.Equal(t, funcSymbol.Name, retrievedFunc.Name)
	assert.Equal(t, funcSymbol.Type, retrievedFunc.Type)
	assert.Equal(t, funcSymbol.Position, retrievedFunc.Position)
	assert.Len(t, retrievedFunc.Parameters, 1)
	assert.Equal(t, "args", retrievedFunc.Parameters[0].Name)

	retrievedVar, exists := retrievedGraph.Symbols[varSymbol.ID]
	require.True(t, exists)
	assert.Equal(t, varSymbol.Name, retrievedVar.Name)
	assert.Equal(t, varSymbol.ReturnType, retrievedVar.ReturnType)
	assert.NotNil(t, retrievedVar.Parent)
	assert.Equal(t, funcSymbol.ID, retrievedVar.Parent.ID)

	// Verify relationships
	relationships := retrievedGraph.GetRelationships(funcSymbol.ID)
	assert.Len(t, relationships, 1)
	assert.Equal(t, rel.Type, relationships[0].Type)
	assert.Equal(t, rel.To, relationships[0].To)
}

func TestSymbolGraphStorage_UpdateFileMetadata(t *testing.T) {
	// Setup in-memory SQLite database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, setupTestDatabase(db))

	queries := New(db)
	storage := NewSymbolGraphStorage(queries)

	// Create a simple graph first
	graph := treesitter.NewSymbolGraph()
	symbol := &treesitter.Symbol{
		ID:   "test.go:sym:1:1",
		Name: "sym",
		Type: treesitter.SymbolTypeFunction,
		Position: treesitter.Position{
			Line:   1,
			Column: 1,
		},
	}
	graph.AddSymbol(symbol)

	err = storage.StoreSymbolGraph(context.Background(), "test.go", "go", graph)
	require.NoError(t, err)

	// Update metadata
	checksum := "abc123"
	err = storage.UpdateFileMetadata(context.Background(), "test.go", checksum)
	require.NoError(t, err)

	// Verify metadata was updated
	files, err := storage.ListFilesWithSymbols(context.Background())
	require.NoError(t, err)
	require.Len(t, files, 1)
	file0, ok := files[0].(FileMetadatum)
	require.True(t, ok)
	assert.Equal(t, "test.go", file0.Path)
	assert.Equal(t, "go", file0.Language)
	assert.True(t, file0.Checksum.Valid)
	assert.Equal(t, checksum, file0.Checksum.String)
}

func TestSymbolGraphStorage_DeleteFileData(t *testing.T) {
	// Setup in-memory SQLite database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, setupTestDatabase(db))

	queries := New(db)
	storage := NewSymbolGraphStorage(queries)

	// Create and store a graph
	graph := treesitter.NewSymbolGraph()
	symbol := &treesitter.Symbol{
		ID:   "test.go:sym:1:1",
		Name: "sym",
		Type: treesitter.SymbolTypeFunction,
		Position: treesitter.Position{
			Line:   1,
			Column: 1,
		},
	}
	graph.AddSymbol(symbol)

	err = storage.StoreSymbolGraph(context.Background(), "test.go", "go", graph)
	require.NoError(t, err)

	// Verify data exists
	retrieved, err := storage.GetSymbolGraph(context.Background(), "test.go")
	require.NoError(t, err)
	assert.Len(t, retrieved.Symbols, 1)

	// Delete data
	err = storage.DeleteFileData(context.Background(), "test.go")
	require.NoError(t, err)

	// Verify data is gone
	files, err := storage.ListFilesWithSymbols(context.Background())
	require.NoError(t, err)
	assert.Len(t, files, 0)

	// Try to retrieve - should return empty graph
	retrieved, err = storage.GetSymbolGraph(context.Background(), "test.go")
	require.NoError(t, err)
	assert.Len(t, retrieved.Symbols, 0)
}

func TestSymbolGraphStorage_ListFilesWithSymbols(t *testing.T) {
	// Setup in-memory SQLite database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, setupTestDatabase(db))

	queries := New(db)
	storage := NewSymbolGraphStorage(queries)

	// Initially empty
	files, err := storage.ListFilesWithSymbols(context.Background())
	require.NoError(t, err)
	assert.Len(t, files, 0)

	// Add a file
	graph := treesitter.NewSymbolGraph()
	symbol := &treesitter.Symbol{
		ID:   "file1.go:sym:1:1",
		Name: "sym",
		Type: treesitter.SymbolTypeFunction,
		Position: treesitter.Position{
			Line:   1,
			Column: 1,
		},
	}
	graph.AddSymbol(symbol)

	err = storage.StoreSymbolGraph(context.Background(), "file1.go", "go", graph)
	require.NoError(t, err)

	// Add another file
	graph2 := treesitter.NewSymbolGraph()
	symbol2 := &treesitter.Symbol{
		ID:   "file2.js:sym:1:1",
		Name: "sym",
		Type: treesitter.SymbolTypeFunction,
		Position: treesitter.Position{
			Line:   1,
			Column: 1,
		},
	}
	graph2.AddSymbol(symbol2)

	err = storage.StoreSymbolGraph(context.Background(), "file2.js", "javascript", graph2)
	require.NoError(t, err)

	// List files
	files, err = storage.ListFilesWithSymbols(context.Background())
	require.NoError(t, err)
	assert.Len(t, files, 2)

	// Check file details
	filePaths := make(map[string]string)
	for _, file := range files {
		fm, ok := file.(FileMetadatum)
		require.True(t, ok)
		filePaths[fm.Path] = fm.Language
	}

	assert.Equal(t, "go", filePaths["file1.go"])
	assert.Equal(t, "javascript", filePaths["file2.js"])
}
