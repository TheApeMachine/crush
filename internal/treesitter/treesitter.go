package treesitter

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// Language represents a programming language supported by TreeSitter
type Language string

const (
	LanguageGo         Language = "go"
	LanguageJavaScript Language = "javascript"
	LanguageTypeScript Language = "typescript"
)

// Parser represents a TreeSitter parser for a specific language
type Parser struct {
	*sitter.Parser
	language Language
}

// NewParser creates a new parser for the given language
func NewParser(lang Language) (*Parser, error) {
	parser := sitter.NewParser()

	var sitterLang *sitter.Language
	switch lang {
	case LanguageGo:
		sitterLang = sitter.NewLanguage(tree_sitter_go.Language())
	case LanguageJavaScript:
		sitterLang = sitter.NewLanguage(tree_sitter_javascript.Language())
	// case LanguageTypeScript:
	// 	sitterLang = sitter.NewLanguage(tree_sitter_typescript.Language())
	default:
		return nil, NewLanguageError(fmt.Sprintf("unsupported language: %s", lang), nil)
	}

	if sitterLang == nil {
		return nil, NewLanguageError(fmt.Sprintf("failed to create language for %s", lang), nil)
	}

	if err := parser.SetLanguage(sitterLang); err != nil {
		return nil, NewLanguageError(fmt.Sprintf("failed to set language %s", lang), err)
	}

	return &Parser{
		Parser:   parser,
		language: lang,
	}, nil
}

// Language returns the language of the parser
func (p *Parser) Language() Language {
	return p.language
}

// Registry manages parsers and language detection
type Registry struct {
	mu      sync.RWMutex
	parsers map[Language]*Parser
	cache   map[string]*sitter.Tree
	storage SymbolStorage // Optional storage for persistence
}

// NewRegistry creates a new parser registry
func NewRegistry() *Registry {
	return &Registry{
		parsers: make(map[Language]*Parser),
		cache:   make(map[string]*sitter.Tree),
	}
}

// NewRegistryWithStorage creates a new parser registry with storage
func NewRegistryWithStorage(storage SymbolStorage) *Registry {
	return &Registry{
		parsers: make(map[Language]*Parser),
		cache:   make(map[string]*sitter.Tree),
		storage: storage,
	}
}

// SetStorage sets the storage for the registry
func (r *Registry) SetStorage(storage SymbolStorage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.storage = storage
}

// RegisterParser registers a parser for a language
func (r *Registry) RegisterParser(lang Language) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.parsers[lang]; exists {
		return NewRegistryError(fmt.Sprintf("parser for language %s already registered", lang), nil)
	}

	parser, err := NewParser(lang)
	if err != nil {
		return err
	}

	r.parsers[lang] = parser
	return nil
}

// GetParser returns the parser for the given language
func (r *Registry) GetParser(lang Language) (*Parser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, exists := r.parsers[lang]
	if !exists {
		return nil, NewRegistryError(fmt.Sprintf("no parser registered for language %s", lang), nil)
	}

	return parser, nil
}

// DetectLanguage detects the language based on file extension
func (r *Registry) DetectLanguage(filename string) (Language, error) {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".go":
		return LanguageGo, nil
	case ".js", ".jsx", ".ts", ".tsx":
		return LanguageJavaScript, nil // Handle TypeScript as JavaScript for now
	default:
		return "", NewValidationError(fmt.Sprintf("unsupported file extension: %s", ext))
	}
}

// Parse parses the source code for the given file
func (r *Registry) Parse(filename, source string) (*sitter.Tree, error) {
	lang, err := r.DetectLanguage(filename)
	if err != nil {
		return nil, err
	}

	parser, err := r.GetParser(lang)
	if err != nil {
		return nil, err
	}

	tree := parser.Parse([]byte(source), nil)
	if tree == nil {
		return nil, NewParseError(fmt.Sprintf("failed to parse %s", filename), nil)
	}

	// Cache the tree
	r.mu.Lock()
	r.cache[filename] = tree
	r.mu.Unlock()

	return tree, nil
}

// GetCachedTree returns a cached tree for the file if available
func (r *Registry) GetCachedTree(filename string) (*sitter.Tree, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tree, exists := r.cache[filename]
	return tree, exists
}

// ClearCache clears the AST cache
func (r *Registry) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache = make(map[string]*sitter.Tree)
}

// ExtractSymbols extracts symbols and relationships from the given source code
func (r *Registry) ExtractSymbols(filename, source string) ([]Symbol, []Relationship, error) {
	tree, err := r.Parse(filename, source)
	if err != nil {
		return nil, nil, err
	}

	lang, err := r.DetectLanguage(filename)
	if err != nil {
		return nil, nil, err
	}

	extractor := GetSymbolExtractor(lang)
	return extractor.ExtractSymbols(tree, []byte(source))
}

// Close closes all registered parsers
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, parser := range r.parsers {
		parser.Close()
	}

	return nil
}

// QuerySymbols returns symbols matching the given criteria
func (r *Registry) QuerySymbols(filename, source string, symbolType SymbolType) ([]Symbol, error) {
	symbols, _, err := r.ExtractSymbols(filename, source)
	if err != nil {
		return nil, err
	}

	var filtered []Symbol
	for _, symbol := range symbols {
		if symbol.Type == symbolType {
			filtered = append(filtered, symbol)
		}
	}

	return filtered, nil
}

// QueryRelationships returns relationships for a given symbol
func (r *Registry) QueryRelationships(filename, source, symbolID string) ([]Relationship, error) {
	_, relationships, err := r.ExtractSymbols(filename, source)
	if err != nil {
		return nil, err
	}

	var filtered []Relationship
	for _, rel := range relationships {
		if rel.From == symbolID || rel.To == symbolID {
			filtered = append(filtered, rel)
		}
	}

	return filtered, nil
}

// BuildSymbolGraph builds a symbol graph from the given source
func (r *Registry) BuildSymbolGraph(filename, source string) (*SymbolGraph, error) {
	symbols, relationships, err := r.ExtractSymbols(filename, source)
	if err != nil {
		return nil, err
	}

	graph := NewSymbolGraph()
	for _, symbol := range symbols {
		graph.AddSymbol(&symbol)
	}
	for _, rel := range relationships {
		graph.AddRelationship(rel)
	}

	return graph, nil
}

// PersistSymbolGraph persists a symbol graph to storage if available
func (r *Registry) PersistSymbolGraph(ctx context.Context, filename, source string, graph *SymbolGraph) error {
	r.mu.RLock()
	storage := r.storage
	r.mu.RUnlock()

	if storage == nil {
		return nil // No storage configured, skip persistence
	}

	lang, err := r.DetectLanguage(filename)
	if err != nil {
		return err
	}

	return storage.StoreSymbolGraph(ctx, filename, string(lang), graph)
}

// GetPersistedSymbolGraph retrieves a persisted symbol graph if available
func (r *Registry) GetPersistedSymbolGraph(ctx context.Context, filename string) (*SymbolGraph, error) {
	r.mu.RLock()
	storage := r.storage
	r.mu.RUnlock()

	if storage == nil {
		return nil, fmt.Errorf("no storage configured")
	}

	return storage.GetSymbolGraph(ctx, filename)
}

// BuildSymbolGraphWithPersistence builds a symbol graph, persisting it if storage is available
func (r *Registry) BuildSymbolGraphWithPersistence(ctx context.Context, filename, source string) (*SymbolGraph, error) {
	// Try to get from storage first
	if graph, err := r.GetPersistedSymbolGraph(ctx, filename); err == nil {
		return graph, nil
	}

	// Build from source
	graph, err := r.BuildSymbolGraph(filename, source)
	if err != nil {
		return nil, err
	}

	// Persist if storage is available
	if persistErr := r.PersistSymbolGraph(ctx, filename, source, graph); persistErr != nil {
		// Log error but don't fail the operation
		// TODO: Add proper logging
	}

	return graph, nil
}
