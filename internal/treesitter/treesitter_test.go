package treesitter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.parsers)
	assert.NotNil(t, registry.cache)
}

func TestRegistry_RegisterParser(t *testing.T) {
	registry := NewRegistry()

	// Register Go parser
	err := registry.RegisterParser(LanguageGo)
	require.NoError(t, err)

	// Try to register again - should fail
	err = registry.RegisterParser(LanguageGo)
	assert.Error(t, err)
	assert.IsType(t, &Error{}, err)
	tsErr := err.(*Error)
	assert.Equal(t, ErrorTypeRegistry, tsErr.Type)
}

func TestRegistry_GetParser(t *testing.T) {
	registry := NewRegistry()

	// Try to get unregistered parser
	_, err := registry.GetParser(LanguageGo)
	assert.Error(t, err)
	assert.IsType(t, &Error{}, err)

	// Register and get
	err = registry.RegisterParser(LanguageGo)
	require.NoError(t, err)

	parser, err := registry.GetParser(LanguageGo)
	assert.NoError(t, err)
	assert.NotNil(t, parser)
	assert.Equal(t, LanguageGo, parser.Language())
}

func TestRegistry_DetectLanguage(t *testing.T) {
	registry := NewRegistry()

	tests := []struct {
		filename string
		expected Language
		hasError bool
	}{
		{"main.go", LanguageGo, false},
		{"app.js", LanguageJavaScript, false},
		{"component.jsx", LanguageJavaScript, false},
		{"unknown.txt", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			lang, err := registry.DetectLanguage(tt.filename)
			if tt.hasError {
				assert.Error(t, err)
				assert.IsType(t, &Error{}, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, lang)
			}
		})
	}
}

func TestRegistry_Parse(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterParser(LanguageGo)
	require.NoError(t, err)

	source := `package main

func main() {
	println("Hello, World!")
}`

	tree, err := registry.Parse("main.go", source)
	assert.NoError(t, err)
	assert.NotNil(t, tree)

	// Check if cached
	cached, exists := registry.GetCachedTree("main.go")
	assert.True(t, exists)
	assert.Equal(t, tree, cached)
}

func TestRegistry_ExtractSymbols(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterParser(LanguageGo)
	require.NoError(t, err)

	source := `package main

func main() {
	println("Hello, World!")
}`

	symbols, relationships, err := registry.ExtractSymbols("main.go", source)
	assert.NoError(t, err)
	// Should extract the main function
	assert.NotEmpty(t, symbols)
	found := false
	for _, sym := range symbols {
		if sym.Name == "main" && sym.Type == SymbolTypeFunction {
			found = true
			break
		}
	}
	assert.True(t, found, "main function should be extracted")
	assert.NotEmpty(t, relationships) // Should have call relationship
}

func TestRegistry_ExtractSymbolsJavaScript(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterParser(LanguageJavaScript)
	require.NoError(t, err)

	source := `function hello() {
	console.log("Hello, World!");
}

hello();`

	symbols, relationships, err := registry.ExtractSymbols("app.js", source)
	assert.NoError(t, err)
	// Should extract the hello function
	assert.NotEmpty(t, symbols)
	found := false
	for _, sym := range symbols {
		if sym.Name == "hello" && sym.Type == SymbolTypeFunction {
			found = true
			break
		}
	}
	assert.True(t, found, "hello function should be extracted")
	assert.NotEmpty(t, relationships) // Should have call relationship
}

func TestRegistry_QuerySymbols(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterParser(LanguageGo)
	require.NoError(t, err)

	source := `package main

func main() {
	println("Hello, World!")
}`

	functions, err := registry.QuerySymbols("main.go", source, SymbolTypeFunction)
	assert.NoError(t, err)
	assert.NotEmpty(t, functions)
	assert.Equal(t, "main", functions[0].Name)
}

func TestRegistry_BuildSymbolGraph(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterParser(LanguageGo)
	require.NoError(t, err)

	source := `package main

func main() {
	println("Hello, World!")
}`

	graph, err := registry.BuildSymbolGraph("main.go", source)
	assert.NoError(t, err)
	assert.NotNil(t, graph)
	assert.NotEmpty(t, graph.Symbols)
	assert.NotEmpty(t, graph.Relationships)
}

func TestRegistry_ClearCache(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterParser(LanguageGo)
	require.NoError(t, err)

	source := `package main

func main() {}`

	_, err = registry.Parse("main.go", source)
	require.NoError(t, err)

	// Check cache
	_, exists := registry.GetCachedTree("main.go")
	assert.True(t, exists)

	// Clear cache
	registry.ClearCache()

	// Check cache is empty
	_, exists = registry.GetCachedTree("main.go")
	assert.False(t, exists)
}

func TestRegistry_Close(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterParser(LanguageGo)
	require.NoError(t, err)

	err = registry.Close()
	assert.NoError(t, err)
}

func TestNewParser(t *testing.T) {
	parser, err := NewParser(LanguageGo)
	assert.NoError(t, err)
	assert.NotNil(t, parser)
	assert.Equal(t, LanguageGo, parser.Language())

	// Test unsupported language
	_, err = NewParser("unsupported")
	assert.Error(t, err)
	assert.IsType(t, &Error{}, err)
}
