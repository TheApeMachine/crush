package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/charlievieth/fastwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/treesitter"
)

// Indexer is responsible for scanning the workspace and keeping the symbol graph up to date.
type Indexer struct {
	wg       *sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
	cfg      *config.Config
	registry *treesitter.Registry
	storage  *db.SymbolGraphStorage
	parseMu  sync.Mutex // Mutex to ensure thread-safe parsing
}

// NewIndexer creates a new indexer.
func NewIndexer(ctx context.Context, cfg *config.Config, querier db.Querier) (*Indexer, error) {
	registry := treesitter.NewRegistry()
	if err := registry.RegisterParser(treesitter.LanguageGo); err != nil {
		slog.Warn("Failed to register Go parser for TreeSitter tools", "error", err)
	}
	if err := registry.RegisterParser(treesitter.LanguageJavaScript); err != nil {
		slog.Warn("Failed to register JavaScript/TypeScript parser for TreeSitter tools", "error", err)
	}
	storage := db.NewSymbolGraphStorage(querier.(*db.Queries))

	ctx, cancel := context.WithCancel(ctx)
	return &Indexer{
		wg:       &sync.WaitGroup{},
		ctx:      ctx,
		cancel:   cancel,
		cfg:      cfg,
		registry: registry,
		storage:  storage,
	}, nil
}

// Start kicks off the initial workspace scan in the background.
func (i *Indexer) Start() {
	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		slog.Info("Starting initial workspace scan...")
		// Clear previous symbol graph data to avoid stale or duplicate entries
		if err := i.storage.ResetAll(i.ctx); err != nil {
			slog.Error("Failed to reset symbol storage", "error", err)
		}
		if err := i.scanWorkspace(); err != nil {
			slog.Error("Error during initial workspace scan", "error", err)
		} else {
			slog.Info("Initial workspace scan complete.")
		}
	}()
}

// Stop gracefully shuts down the indexer.
func (i *Indexer) Stop() {
	i.cancel()
	i.wg.Wait()
}

// scanWorkspace walks the workspace directory and indexes all supported files.
func (i *Indexer) scanWorkspace() error {
	conf := fastwalk.Config{
		Follow: true,
	}

	return fastwalk.Walk(&conf, i.cfg.WorkingDir(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files we don't have permission to access
		}

		if i.ctx.Err() != nil {
			return i.ctx.Err()
		}

		if d.IsDir() {
			return nil
		}

		if _, err := i.registry.DetectLanguage(path); err == nil {
			slog.Debug("Indexing file", "path", path)
			if err := i.IndexFile(path); err != nil {
				slog.Warn("Failed to index file", "path", path, "error", err)
			}
		}

		return nil
	})
}

// IndexFile reads a file, builds its symbol graph, and persists it to storage.
func (i *Indexer) IndexFile(path string) error {
	// For now, we'll just use the relative path from the workspace root.
	// In the future, we may want to make this more robust.
	relPath, err := filepath.Rel(i.cfg.WorkingDir(), path)
	if err != nil {
		slog.Error("Failed to get relative path", "path", path, "error", err)
		return err
	}

	slog.Info("Indexing file", "path", relPath)

	content, err := os.ReadFile(path)
	if err != nil {
		slog.Error("Failed to read file", "path", relPath, "error", err)
		return err
	}

	// Skip files that are too large to avoid memory issues
	if len(content) > 10*1024*1024 { // 10MB limit
		slog.Debug("Skipping large file", "path", relPath, "size", len(content))
		return nil
	}

	// Skip binary files or files with null bytes
	if len(content) > 0 && content[0] == 0 {
		slog.Debug("Skipping binary file", "path", relPath)
		return nil
	}

	// Skip files with invalid UTF-8 encoding
	if !isValidUTF8(content) {
		slog.Debug("Skipping file with invalid UTF-8 encoding", "path", relPath)
		return nil
	}

	// Lock to ensure thread-safe parsing
	i.parseMu.Lock()
	defer i.parseMu.Unlock()

	// Use recover to handle TreeSitter panics
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic during symbol extraction", "path", relPath, "panic", r)
		}
	}()

	slog.Debug("Building symbol graph", "path", relPath)
	graph, err := i.registry.BuildSymbolGraph(relPath, string(content))
	if err != nil {
		slog.Error("Failed to build symbol graph", "path", relPath, "error", err)
		return nil // Don't fail the entire indexing process
	}

	slog.Info("Extracted symbols", "path", relPath, "symbol_count", len(graph.Symbols), "relationship_count", len(graph.Relationships))

	// Log some details about what was extracted
	for _, symbol := range graph.Symbols {
		slog.Debug("Symbol found", "path", relPath, "name", symbol.Name, "type", symbol.Type, "id", symbol.ID)
	}

	for _, rel := range graph.Relationships {
		for _, r := range rel {
			slog.Debug("Relationship found", "path", relPath, "from", r.From, "to", r.To, "type", r.Type)
		}
	}

	lang, err := i.registry.DetectLanguage(relPath)
	if err != nil {
		slog.Error("Failed to detect language", "path", relPath, "error", err)
		return nil
	}

	slog.Debug("Storing symbol graph", "path", relPath, "language", lang)
	err = i.storage.StoreSymbolGraph(i.ctx, relPath, string(lang), graph)
	if err != nil {
		slog.Error("Failed to store symbol graph", "path", relPath, "error", err)
		return err
	}

	slog.Info("Successfully indexed file", "path", relPath)
	return nil
}

// isValidUTF8 checks if the given bytes represent valid UTF-8 encoded text
func isValidUTF8(data []byte) bool {
	return utf8.Valid(data)
}
