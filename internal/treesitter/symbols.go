package treesitter

import (
	"context"
	"fmt"
	"log/slog"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Symbol represents a symbol extracted from the AST
type Symbol struct {
	ID         string // Unique identifier for the symbol
	Name       string
	Type       SymbolType
	Position   Position
	Scope      ScopeType
	Visibility VisibilityType
	ReturnType string      // For functions/methods
	Parameters []Parameter // For functions/methods
	Parent     *Symbol     // Parent symbol for nested structures
}

// SymbolType represents the type of symbol
type SymbolType string

const (
	SymbolTypeFunction  SymbolType = "function"
	SymbolTypeVariable  SymbolType = "variable"
	SymbolTypeClass     SymbolType = "class"
	SymbolTypeMethod    SymbolType = "method"
	SymbolTypeField     SymbolType = "field"
	SymbolTypeImport    SymbolType = "import"
	SymbolTypeType      SymbolType = "type"
	SymbolTypeInterface SymbolType = "interface"
	SymbolTypeConstant  SymbolType = "constant"
)

// ScopeType represents the scope of a symbol
type ScopeType string

const (
	ScopeGlobal ScopeType = "global"
	ScopeLocal  ScopeType = "local"
	ScopeBlock  ScopeType = "block"
)

// VisibilityType represents the visibility of a symbol
type VisibilityType string

const (
	VisibilityPublic    VisibilityType = "public"
	VisibilityPrivate   VisibilityType = "private"
	VisibilityProtected VisibilityType = "protected"
	VisibilityPackage   VisibilityType = "package"
)

// Parameter represents a function parameter
type Parameter struct {
	Name string
	Type string
}

// Relationship represents a relationship between symbols
type Relationship struct {
	From     string // Symbol ID
	To       string // Symbol ID
	Type     RelationshipType
	Position Position // Position of the relationship in code
}

// RelationshipType represents the type of relationship
type RelationshipType string

const (
	RelationshipTypeCall      RelationshipType = "call"
	RelationshipTypeReference RelationshipType = "reference"
	RelationshipTypeInherit   RelationshipType = "inherit"
	RelationshipTypeImport    RelationshipType = "import"
	RelationshipTypeImplement RelationshipType = "implement"
)

// SymbolGraph represents a graph of symbols and their relationships
type SymbolGraph struct {
	Symbols       map[string]*Symbol        // Symbol ID to Symbol
	Relationships map[string][]Relationship // Symbol ID to list of relationships
}

// NewSymbolGraph creates a new symbol graph
func NewSymbolGraph() *SymbolGraph {
	return &SymbolGraph{
		Symbols:       make(map[string]*Symbol),
		Relationships: make(map[string][]Relationship),
	}
}

// AddSymbol adds a symbol to the graph
func (g *SymbolGraph) AddSymbol(symbol *Symbol) {
	g.Symbols[symbol.ID] = symbol
}

// AddRelationship adds a relationship to the graph
func (g *SymbolGraph) AddRelationship(rel Relationship) {
	g.Relationships[rel.From] = append(g.Relationships[rel.From], rel)
}

// GetSymbol returns a symbol by ID
func (g *SymbolGraph) GetSymbol(id string) (*Symbol, bool) {
	symbol, exists := g.Symbols[id]
	return symbol, exists
}

// GetRelationships returns relationships for a symbol ID
func (g *SymbolGraph) GetRelationships(id string) []Relationship {
	return g.Relationships[id]
}

// GenerateSymbolID generates a unique ID for a symbol
func GenerateSymbolID(name string, pos Position) string {
	return fmt.Sprintf("%s:%d:%d", name, pos.Line, pos.Column)
}

// Position represents a position in the source code
type Position struct {
	Line   uint32
	Column uint32
}

// SymbolExtractor defines the interface for extracting symbols and relationships from AST
type SymbolExtractor interface {
	ExtractSymbols(tree *sitter.Tree, source []byte) ([]Symbol, []Relationship, error)
}

// BaseSymbolExtractor provides common functionality for symbol extraction
type BaseSymbolExtractor struct{}

// ExtractSymbols is a placeholder implementation
func (e *BaseSymbolExtractor) ExtractSymbols(tree *sitter.Tree, source []byte) ([]Symbol, []Relationship, error) {
	// This is a basic implementation that can be extended
	// For now, return empty slices
	return []Symbol{}, []Relationship{}, nil
}

// GoSymbolExtractor extracts symbols from Go code
type GoSymbolExtractor struct {
	BaseSymbolExtractor
	currentFunction string // Track current function context for relationships
}

// ExtractSymbols extracts symbols from Go AST
func (e *GoSymbolExtractor) ExtractSymbols(tree *sitter.Tree, source []byte) ([]Symbol, []Relationship, error) {
	var symbols []Symbol
	var relationships []Relationship

	if tree == nil || tree.RootNode() == nil {
		return symbols, relationships, nil
	}

	// Use recover to handle potential segfaults from tree-sitter
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("Panic in GoSymbolExtractor", "panic", r)
			}
		}()

		e.walkTree(tree.RootNode(), source, &symbols, &relationships)
	}()

	return symbols, relationships, nil
}

// walkTree recursively walks the AST tree to extract symbols and relationships
func (e *GoSymbolExtractor) walkTree(node *sitter.Node, source []byte, symbols *[]Symbol, relationships *[]Relationship) {
	if node == nil {
		return
	}

	var previousFunction string
	contextChanged := false

	switch node.Kind() {
	case "function_declaration":
		symbolID := e.extractFunctionDeclaration(node, source, symbols)
		if symbolID != "" {
			previousFunction = e.currentFunction
			e.currentFunction = symbolID
			contextChanged = true
		}
	case "method_declaration":
		symbolID := e.extractMethodDeclaration(node, source, symbols)
		if symbolID != "" {
			previousFunction = e.currentFunction
			e.currentFunction = symbolID
			contextChanged = true
		}
	case "var_declaration":
		e.extractVarDeclaration(node, source, symbols)
	case "const_declaration":
		e.extractConstDeclaration(node, source, symbols)
	case "type_declaration":
		e.extractTypeDeclaration(node, source, symbols)
	case "import_declaration":
		e.extractImportDeclaration(node, source, symbols)
	case "call_expression":
		e.extractCallExpression(node, source, relationships)
	}

	// Recurse on children
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		e.walkTree(child, source, symbols, relationships)
	}

	if contextChanged {
		e.currentFunction = previousFunction
	}
}

// extractFunctionDeclaration extracts a function symbol and returns its ID
func (e *GoSymbolExtractor) extractFunctionDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) string {
	// Try multiple approaches to find the function name
	var name string
	var pos Position

	// Approach 1: Try field names
	fieldNames := []string{"name", "identifier"}
	for _, fieldName := range fieldNames {
		nameNode := node.ChildByFieldName(fieldName)
		if nameNode != nil {
			name = string(source[nameNode.StartByte():nameNode.EndByte()])
			pos = Position{Line: uint32(nameNode.StartPosition().Row) + 1, Column: uint32(nameNode.StartPosition().Column) + 1}
			goto found
		}
	}

	// Approach 2: Look for identifier children
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil && child.Kind() == "identifier" {
			// Check if this is the function name (usually the first identifier after "func")
			name = string(source[child.StartByte():child.EndByte()])
			pos = Position{Line: uint32(child.StartPosition().Row) + 1, Column: uint32(child.StartPosition().Column) + 1}
			goto found
		}
	}

	// If we can't find a name, skip this function
	return ""

found:
	symbolID := GenerateSymbolID(name, pos)
	symbol := Symbol{
		ID:         symbolID,
		Name:       name,
		Type:       SymbolTypeFunction,
		Position:   pos,
		Scope:      ScopeGlobal,
		Visibility: VisibilityPublic,
	}

	*symbols = append(*symbols, symbol)
	return symbolID
}

// extractMethodDeclaration extracts a method symbol and returns its ID
func (e *GoSymbolExtractor) extractMethodDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	name := string(source[nameNode.StartByte():nameNode.EndByte()])
	pos := Position{Line: uint32(nameNode.StartPosition().Row) + 1, Column: uint32(nameNode.StartPosition().Column) + 1}
	symbolID := GenerateSymbolID(name, pos)

	symbol := Symbol{
		ID:         symbolID,
		Name:       name,
		Type:       SymbolTypeMethod,
		Position:   pos,
		Scope:      ScopeGlobal,
		Visibility: VisibilityPublic,
	}

	// Extract parameters
	paramsNode := node.ChildByFieldName("parameters")
	if paramsNode != nil {
		symbol.Parameters = e.extractParameters(paramsNode, source)
	}

	// Extract return type
	resultNode := node.ChildByFieldName("result")
	if resultNode != nil {
		symbol.ReturnType = e.extractType(resultNode, source)
	}

	*symbols = append(*symbols, symbol)
	return symbolID
}

// extractVarDeclaration extracts variable symbols
func (e *GoSymbolExtractor) extractVarDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) {
	// Handle multiple variables in one declaration
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "var_spec" {
			e.extractVarSpec(child, source, symbols)
		} else if child.Kind() == "identifier" {
			// Direct identifier in var_declaration
			name := string(source[child.StartByte():child.EndByte()])
			pos := Position{Line: uint32(child.StartPosition().Row) + 1, Column: uint32(child.StartPosition().Column) + 1}

			symbol := Symbol{
				ID:         GenerateSymbolID(name, pos),
				Name:       name,
				Type:       SymbolTypeVariable,
				Position:   pos,
				Scope:      ScopeGlobal,
				Visibility: VisibilityPublic,
			}

			*symbols = append(*symbols, symbol)
		}
	}
}

// extractVarSpec extracts a single variable specification
func (e *GoSymbolExtractor) extractVarSpec(node *sitter.Node, source []byte, symbols *[]Symbol) {
	if node == nil {
		return
	}

	// Use recover to handle potential panics from TreeSitter
	defer func() {
		if r := recover(); r != nil {
			// Log the panic but don't crash
			slog.Error("Panic in extractVarSpec", "error", r)
		}
	}()

	// Try to find identifiers manually by traversing children
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil && child.Kind() == "identifier" {
			name := string(source[child.StartByte():child.EndByte()])
			pos := Position{Line: uint32(child.StartPosition().Row) + 1, Column: uint32(child.StartPosition().Column) + 1}

			symbol := Symbol{
				ID:         GenerateSymbolID(name, pos),
				Name:       name,
				Type:       SymbolTypeVariable,
				Position:   pos,
				Scope:      ScopeGlobal, // Assume global for now
				Visibility: VisibilityPublic,
			}

			*symbols = append(*symbols, symbol)
		}
	}
}

// extractConstDeclaration extracts constant symbols
func (e *GoSymbolExtractor) extractConstDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "const_spec" {
			e.extractConstSpec(child, source, symbols)
		}
	}
}

// extractConstSpec extracts a single constant specification
func (e *GoSymbolExtractor) extractConstSpec(node *sitter.Node, source []byte, symbols *[]Symbol) {
	if node == nil {
		return
	}

	// Use recover to handle potential panics from TreeSitter
	defer func() {
		if r := recover(); r != nil {
			// Log the panic but don't crash
			slog.Error("Panic in extractConstSpec", "error", r)
		}
	}()

	// Try to find identifiers manually by traversing children
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil && child.Kind() == "identifier" {
			name := string(source[child.StartByte():child.EndByte()])
			pos := Position{Line: uint32(child.StartPosition().Row) + 1, Column: uint32(child.StartPosition().Column) + 1}

			symbol := Symbol{
				ID:         GenerateSymbolID(name, pos),
				Name:       name,
				Type:       SymbolTypeConstant,
				Position:   pos,
				Scope:      ScopeGlobal,
				Visibility: VisibilityPublic,
			}

			*symbols = append(*symbols, symbol)
		}
	}
}

// extractTypeDeclaration extracts type symbols
func (e *GoSymbolExtractor) extractTypeDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "type_spec" {
			e.extractTypeSpec(child, source, symbols)
		}
	}

	// Also try to extract types directly if no type_spec found
	if len(*symbols) == 0 || !e.hasTypeSymbols(*symbols) {
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.Kind() == "identifier" {
				name := string(source[child.StartByte():child.EndByte()])
				pos := Position{Line: uint32(child.StartPosition().Row) + 1, Column: uint32(child.StartPosition().Column) + 1}

				symbol := Symbol{
					ID:         GenerateSymbolID(name, pos),
					Name:       name,
					Type:       SymbolTypeType,
					Position:   pos,
					Scope:      ScopeGlobal,
					Visibility: VisibilityPublic,
				}

				*symbols = append(*symbols, symbol)
			}
		}
	}
}

// hasTypeSymbols checks if the symbols slice contains any type symbols
func (e *GoSymbolExtractor) hasTypeSymbols(symbols []Symbol) bool {
	for _, symbol := range symbols {
		if symbol.Type == SymbolTypeType {
			return true
		}
	}
	return false
}

// extractTypeSpec extracts a single type specification
func (e *GoSymbolExtractor) extractTypeSpec(node *sitter.Node, source []byte, symbols *[]Symbol) {
	// Try multiple field names for type name
	fieldNames := []string{"name", "identifier"}
	var nameNode *sitter.Node
	for _, fieldName := range fieldNames {
		nameNode = node.ChildByFieldName(fieldName)
		if nameNode != nil {
			break
		}
	}

	// If no field found, try to find identifier child directly
	if nameNode == nil {
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child != nil && child.Kind() == "identifier" {
				nameNode = child
				break
			}
		}
	}

	if nameNode == nil {
		return
	}

	name := string(source[nameNode.StartByte():nameNode.EndByte()])
	pos := Position{Line: uint32(nameNode.StartPosition().Row) + 1, Column: uint32(nameNode.StartPosition().Column) + 1}

	symbol := Symbol{
		ID:         GenerateSymbolID(name, pos),
		Name:       name,
		Type:       SymbolTypeType,
		Position:   pos,
		Scope:      ScopeGlobal,
		Visibility: VisibilityPublic,
	}

	*symbols = append(*symbols, symbol)
}

// extractImportDeclaration extracts import symbols
func (e *GoSymbolExtractor) extractImportDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "import_spec" {
			e.extractImportSpec(child, source, symbols)
		}
	}
}

// extractImportSpec extracts a single import specification
func (e *GoSymbolExtractor) extractImportSpec(node *sitter.Node, source []byte, symbols *[]Symbol) {
	pathNode := node.ChildByFieldName("path")
	if pathNode == nil {
		return
	}

	path := string(source[pathNode.StartByte():pathNode.EndByte()])
	pos := Position{Line: uint32(pathNode.StartPosition().Row) + 1, Column: uint32(pathNode.StartPosition().Column) + 1}

	symbol := Symbol{
		ID:         GenerateSymbolID(path, pos),
		Name:       path,
		Type:       SymbolTypeImport,
		Position:   pos,
		Scope:      ScopeGlobal,
		Visibility: VisibilityPublic,
	}

	*symbols = append(*symbols, symbol)
}

// extractCallExpression extracts call relationships
func (e *GoSymbolExtractor) extractCallExpression(node *sitter.Node, source []byte, relationships *[]Relationship) {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil {
		return
	}

	// For now, only handle direct function calls
	if functionNode.Kind() == "identifier" {
		functionName := string(source[functionNode.StartByte():functionNode.EndByte()])
		pos := Position{Line: uint32(node.StartPosition().Row) + 1, Column: uint32(node.StartPosition().Column) + 1}

		// Create relationship with current function context
		// Use function name for both From and To since we can't guarantee symbol IDs exist yet
		rel := Relationship{
			From:     e.currentFunction, // This will be resolved later to actual symbol ID
			To:       functionName,      // Use function name directly
			Type:     RelationshipTypeCall,
			Position: pos,
		}

		// Debug logging
		slog.Debug("Found call relationship", "from", rel.From, "to", rel.To, "type", rel.Type)

		*relationships = append(*relationships, rel)
	}
}

// extractParameters extracts function parameters
func (e *GoSymbolExtractor) extractParameters(node *sitter.Node, source []byte) []Parameter {
	var params []Parameter

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "parameter_declaration" {
			param := e.extractParameter(child, source)
			if param.Name != "" {
				params = append(params, param)
			}
		}
	}

	return params
}

// extractParameter extracts a single parameter
func (e *GoSymbolExtractor) extractParameter(node *sitter.Node, source []byte) Parameter {
	if node == nil {
		return Parameter{}
	}

	nameNodes := node.ChildrenByFieldName("name", nil)
	if len(nameNodes) == 0 {
		return Parameter{}
	}

	name := string(source[nameNodes[0].StartByte():nameNodes[0].EndByte()])
	paramType := ""

	typeNode := node.ChildByFieldName("type")
	if typeNode != nil {
		paramType = e.extractType(typeNode, source)
	}

	return Parameter{Name: name, Type: paramType}
}

// extractType extracts type information from a type node
func (e *GoSymbolExtractor) extractType(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}

// JavaScriptSymbolExtractor extracts symbols from JavaScript code
type JavaScriptSymbolExtractor struct {
	BaseSymbolExtractor
}

// ExtractSymbols extracts symbols from JavaScript AST
func (e *JavaScriptSymbolExtractor) ExtractSymbols(tree *sitter.Tree, source []byte) ([]Symbol, []Relationship, error) {
	var symbols []Symbol
	var relationships []Relationship

	// Use recover to handle potential segfaults from tree-sitter
	defer func() {
		if r := recover(); r != nil {
			// Log the panic but don't crash - return empty results
			// In a real implementation, you'd want proper logging here
			slog.Error("Panic in ExtractSymbols", "error", r)
		}
	}()

	if tree == nil || tree.RootNode() == nil {
		return symbols, relationships, nil
	}

	e.walkTreeJS(tree.RootNode(), source, &symbols, &relationships)

	return symbols, relationships, nil
}

// walkTreeJS recursively walks the JavaScript AST tree to extract symbols and relationships
func (e *JavaScriptSymbolExtractor) walkTreeJS(node *sitter.Node, source []byte, symbols *[]Symbol, relationships *[]Relationship) {
	switch node.Kind() {
	case "function_declaration":
		e.extractJSFunctionDeclaration(node, source, symbols)
	case "arrow_function":
		e.extractJSArrowFunction(node, source, symbols)
	case "method_definition":
		e.extractJSMethodDefinition(node, source, symbols)
	case "class_declaration":
		e.extractJSClassDeclaration(node, source, symbols)
	case "variable_declaration":
		e.extractJSVariableDeclaration(node, source, symbols)
	case "lexical_declaration":
		e.extractJSLexicalDeclaration(node, source, symbols)
	case "import_statement":
		e.extractJSImportStatement(node, source, symbols)
	case "export_statement":
		e.extractJSExportStatement(node, source, symbols)
	case "call_expression":
		e.extractJSCallExpression(node, source, relationships)
	}

	// Recurse on children
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		e.walkTreeJS(child, source, symbols, relationships)
	}
}

// extractJSFunctionDeclaration extracts function symbols
func (e *JavaScriptSymbolExtractor) extractJSFunctionDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := string(source[nameNode.StartByte():nameNode.EndByte()])
	pos := Position{Line: uint32(nameNode.StartPosition().Row) + 1, Column: uint32(nameNode.StartPosition().Column) + 1}

	symbol := Symbol{
		ID:         GenerateSymbolID(name, pos),
		Name:       name,
		Type:       SymbolTypeFunction,
		Position:   pos,
		Scope:      ScopeGlobal,
		Visibility: VisibilityPublic,
	}

	// Extract parameters
	paramsNode := node.ChildByFieldName("parameters")
	if paramsNode != nil {
		symbol.Parameters = e.extractJSParameters(paramsNode, source)
	}

	*symbols = append(*symbols, symbol)
}

// extractJSArrowFunction extracts arrow function symbols
func (e *JavaScriptSymbolExtractor) extractJSArrowFunction(node *sitter.Node, source []byte, symbols *[]Symbol) {
	// Arrow functions might not have names, skip for now
}

// extractJSMethodDefinition extracts method symbols
func (e *JavaScriptSymbolExtractor) extractJSMethodDefinition(node *sitter.Node, source []byte, symbols *[]Symbol) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := string(source[nameNode.StartByte():nameNode.EndByte()])
	pos := Position{Line: uint32(nameNode.StartPosition().Row) + 1, Column: uint32(nameNode.StartPosition().Column) + 1}

	symbol := Symbol{
		ID:         GenerateSymbolID(name, pos),
		Name:       name,
		Type:       SymbolTypeMethod,
		Position:   pos,
		Scope:      ScopeGlobal,
		Visibility: VisibilityPublic,
	}

	// Extract parameters
	paramsNode := node.ChildByFieldName("parameters")
	if paramsNode != nil {
		symbol.Parameters = e.extractJSParameters(paramsNode, source)
	}

	*symbols = append(*symbols, symbol)
}

// extractJSClassDeclaration extracts class symbols
func (e *JavaScriptSymbolExtractor) extractJSClassDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := string(source[nameNode.StartByte():nameNode.EndByte()])
	pos := Position{Line: uint32(nameNode.StartPosition().Row) + 1, Column: uint32(nameNode.StartPosition().Column) + 1}

	symbol := Symbol{
		ID:         GenerateSymbolID(name, pos),
		Name:       name,
		Type:       SymbolTypeClass,
		Position:   pos,
		Scope:      ScopeGlobal,
		Visibility: VisibilityPublic,
	}

	*symbols = append(*symbols, symbol)
}

// extractJSVariableDeclaration extracts variable symbols
func (e *JavaScriptSymbolExtractor) extractJSVariableDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "variable_declarator" {
			e.extractJSVariableDeclarator(child, source, symbols)
		}
	}
}

// extractJSLexicalDeclaration extracts lexical variable symbols
func (e *JavaScriptSymbolExtractor) extractJSLexicalDeclaration(node *sitter.Node, source []byte, symbols *[]Symbol) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "variable_declarator" {
			e.extractJSVariableDeclarator(child, source, symbols)
		}
	}
}

// extractJSVariableDeclarator extracts a single variable declarator
func (e *JavaScriptSymbolExtractor) extractJSVariableDeclarator(node *sitter.Node, source []byte, symbols *[]Symbol) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}

	name := string(source[nameNode.StartByte():nameNode.EndByte()])
	pos := Position{Line: uint32(nameNode.StartPosition().Row) + 1, Column: uint32(nameNode.StartPosition().Column) + 1}

	symbol := Symbol{
		ID:         GenerateSymbolID(name, pos),
		Name:       name,
		Type:       SymbolTypeVariable,
		Position:   pos,
		Scope:      ScopeGlobal, // Assume global for now
		Visibility: VisibilityPublic,
	}

	*symbols = append(*symbols, symbol)
}

// extractJSImportStatement extracts import symbols
func (e *JavaScriptSymbolExtractor) extractJSImportStatement(node *sitter.Node, source []byte, symbols *[]Symbol) {
	sourceNode := node.ChildByFieldName("source")
	if sourceNode == nil {
		return
	}

	sourceStr := string(source[sourceNode.StartByte():sourceNode.EndByte()])
	pos := Position{Line: uint32(sourceNode.StartPosition().Row) + 1, Column: uint32(sourceNode.StartPosition().Column) + 1}

	symbol := Symbol{
		ID:         GenerateSymbolID(sourceStr, pos),
		Name:       sourceStr,
		Type:       SymbolTypeImport,
		Position:   pos,
		Scope:      ScopeGlobal,
		Visibility: VisibilityPublic,
	}

	*symbols = append(*symbols, symbol)
}

// extractJSExportStatement extracts export symbols
func (e *JavaScriptSymbolExtractor) extractJSExportStatement(node *sitter.Node, source []byte, symbols *[]Symbol) {
	// For now, just extract the declaration if present
	declNode := node.ChildByFieldName("declaration")
	if declNode != nil {
		e.walkTreeJS(declNode, source, symbols, &[]Relationship{})
	}
}

// extractJSCallExpression extracts call relationships
func (e *JavaScriptSymbolExtractor) extractJSCallExpression(node *sitter.Node, source []byte, relationships *[]Relationship) {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil {
		return
	}

	if functionNode.Kind() == "identifier" {
		functionName := string(source[functionNode.StartByte():functionNode.EndByte()])
		pos := Position{Line: uint32(node.StartPosition().Row) + 1, Column: uint32(node.StartPosition().Column) + 1}

		rel := Relationship{
			From:     "",
			To:       functionName,
			Type:     RelationshipTypeCall,
			Position: pos,
		}

		*relationships = append(*relationships, rel)
	}
}

// extractJSParameters extracts function parameters
func (e *JavaScriptSymbolExtractor) extractJSParameters(node *sitter.Node, source []byte) []Parameter {
	var params []Parameter

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "identifier" {
			name := string(source[child.StartByte():child.EndByte()])
			params = append(params, Parameter{Name: name, Type: ""})
		}
	}

	return params
}

// SymbolStorage defines the interface for persisting and retrieving symbol graphs
type SymbolStorage interface {
	StoreSymbolGraph(ctx context.Context, filename, language string, graph *SymbolGraph) error
	GetSymbolGraph(ctx context.Context, filename string) (*SymbolGraph, error)
	UpdateFileMetadata(ctx context.Context, filename string, checksum string) error
	DeleteFileData(ctx context.Context, filename string) error
	ListFilesWithSymbols(ctx context.Context) ([]interface{}, error) // Will be []db.FileMetadatum
}

// GetSymbolExtractor returns the appropriate symbol extractor for the language
func GetSymbolExtractor(lang Language) SymbolExtractor {
	switch lang {
	case LanguageGo:
		return &GoSymbolExtractor{}
	case LanguageJavaScript:
		return &JavaScriptSymbolExtractor{}
	default:
		return &BaseSymbolExtractor{}
	}
}
