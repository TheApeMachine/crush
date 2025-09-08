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
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/resolution"
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

	// Track seen relative paths during scan
	seen := make(map[string]struct{})

	errWalk := fastwalk.Walk(&conf, i.cfg.WorkingDir(), func(path string, d os.DirEntry, err error) error {
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
			if rel, rerr := filepath.Rel(i.cfg.WorkingDir(), path); rerr == nil {
				seen[rel] = struct{}{}
			}
			slog.Debug("Indexing file", "path", path)
			if err := i.IndexFile(path); err != nil {
				slog.Warn("Failed to index file", "path", path, "error", err)
			}
		}

		return nil
	})

	// After scan, remove stale DB entries for files not seen
	if errWalk == nil {
		if files, err := i.storage.ListFileMetadata(i.ctx); err == nil {
			for _, f := range files {
				if _, ok := seen[f.Path]; !ok {
					slog.Debug("Removing stale file metadata", "path", f.Path)
					_ = i.storage.DeleteFileData(i.ctx, f.Path)
				}
			}
		}
	}

	return errWalk
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
			// include a minimal stack hint using runtime callers (without importing heavy debug stack)
			slog.Error("Panic during symbol extraction", "path", relPath, "panic", r)
		}
	}()

	// Compute a simple checksum for incremental indexing
	checksum := computeChecksum(content)
	// Skip unchanged files if checksum matches
	if meta, err := i.storage.GetFileMetadata(i.ctx, relPath); err == nil {
		if meta.Checksum.Valid && meta.Checksum.String == checksum {
			slog.Debug("Skipping unchanged file", "path", relPath)
			return nil
		}
	}

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
	// Store graph and then update checksum
	err = i.storage.StoreSymbolGraph(i.ctx, relPath, string(lang), graph)
	if err != nil {
		slog.Error("Failed to store symbol graph", "path", relPath, "error", err)
		return err
	}
	if err := i.storage.UpdateFileMetadata(i.ctx, relPath, checksum); err != nil {
		slog.Warn("Failed to update file metadata", "path", relPath, "error", err)
		IndexerWarn("Failed to update file metadata", relPath)
	}

	// For Go files, resolve exact call edges using go/types and store them
	if lang == treesitter.LanguageGo {
		if resolved, rerr := resolution.ResolveCallsForFile(i.ctx, i.cfg.WorkingDir(), relPath); rerr == nil {
			if len(resolved) > 0 {
				slog.Debug("Storing resolved Go call relationships", "count", len(resolved), "path", relPath)
				if err := i.storage.StoreRelationshipsForFile(i.ctx, relPath, resolved); err != nil {
					slog.Warn("Failed to store resolved Go relationships", "path", relPath, "error", err)
					IndexerWarn("Failed storing Go relationships", relPath)
				}
			}
		} else {
			slog.Debug("Go resolver failed", "path", relPath, "error", rerr)
			IndexerWarn("Go call resolution failed", relPath)
		}
	}

	// For JS/TS files, resolve via TS LSP client if available
	if lang == treesitter.LanguageJavaScript {
		// Find any LSP client that handles this file (tsserver/typescript-language-server)
		absPath := filepath.Join(i.cfg.WorkingDir(), relPath)
		var tsClient *lsp.Client
		// The indexer currently doesn't own LSP clients.
		// If you want this to run, inject a getter via a package-level hook.
		if appLSPGetter != nil {
			clients := appLSPGetter()
			for _, c := range clients {
				if c != nil && c.HandlesFile(absPath) {
					tsClient = c
					break
				}
			}
		}
		if tsClient != nil {
			if resolved, rerr := resolution.ResolveTSCallsForFile(i.ctx, tsClient, i.cfg.WorkingDir(), relPath); rerr == nil {
				if len(resolved) > 0 {
					slog.Debug("Storing resolved TS/JS call relationships", "count", len(resolved), "path", relPath)
					if err := i.storage.StoreRelationshipsForFile(i.ctx, relPath, resolved); err != nil {
						slog.Warn("Failed to store resolved TS/JS relationships", "path", relPath, "error", err)
						IndexerWarn("Failed storing TS/JS relationships", relPath)
					}
				}
			} else {
				slog.Debug("TS/JS resolver failed", "path", relPath, "error", rerr)
				IndexerWarn("TS/JS call resolution failed", relPath)
			}
		}
	}

	slog.Info("Successfully indexed file", "path", relPath)
	return nil
}

// isValidUTF8 checks if the given bytes represent valid UTF-8 encoded text
func isValidUTF8(data []byte) bool {
	return utf8.Valid(data)
}

// computeChecksum computes a lightweight checksum of file contents for incremental indexing.
// We avoid cryptographic cost; collisions are acceptable as a rare re-index.
func computeChecksum(data []byte) string {
	var h uint64 = 1469598103934665603 // FNV-1a 64-bit offset basis
	const prime uint64 = 1099511628211
	for _, b := range data {
		h ^= uint64(b)
		h *= prime
	}
	// Represent as hex string
	const hex = "0123456789abcdef"
	buf := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		n := h & 0xF
		buf[i] = hex[n]
		h >>= 4
	}
	return string(buf)
}
