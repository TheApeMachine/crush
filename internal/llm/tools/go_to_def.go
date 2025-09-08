package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/crush/internal/treesitter"
)

type GoToDefParams struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type DefinitionResult struct {
	Symbol    string `json:"symbol"`
	Type      string `json:"type"`
	File      string `json:"file"`
	Line      uint32 `json:"line"`
	Column    uint32 `json:"column"`
	Signature string `json:"signature,omitempty"`
	Context   string `json:"context"`
	Found     bool   `json:"found"`
}

type GoToDefResponseMetadata struct {
	SearchTime  int  `json:"search_time_ms"`
	SymbolFound bool `json:"symbol_found"`
	ExactMatch  bool `json:"exact_match"`
}

type goToDefTool struct {
	registry   *treesitter.Registry
	workingDir string
}

const (
	GoToDefToolName    = "go-to-def"
	goToDefDescription = `Navigate to the definition of a symbol at a specific position using AST-aware analysis.

WHEN TO USE THIS TOOL:
- Use when you need to find where a symbol is defined
- Great for code navigation and understanding symbol origins
- Useful for exploring code structure and dependencies
- Helpful for debugging and understanding complex codebases

HOW TO USE:
- Provide the file path and exact line/column position
- The tool will find the symbol at that position and locate its definition
- Works with functions, variables, classes, methods, and other symbols
- Returns detailed information about the symbol definition

ACCURACY FEATURES:
- AST-aware: Uses parsed syntax tree for accurate symbol identification
- Position-sensitive: Finds symbol at exact cursor position
- Type-aware: Recognizes different symbol types (function, variable, class, etc.)
- Context-preserving: Includes surrounding code context

SUPPORTED SYMBOL TYPES:
- Functions and methods
- Variables and constants
- Classes and interfaces
- Type definitions
- Import statements

EXAMPLES:
- Find definition at cursor: {"path": "main.go", "line": 15, "column": 10}
- Navigate to function definition: {"path": "utils.go", "line": 25, "column": 5}
- Find variable definition: {"path": "config.go", "line": 8, "column": 1}

LIMITATIONS:
- Only works within the specified file (not cross-file navigation)
- Requires exact line/column position
- Only supports Go and JavaScript/TypeScript files
- May not work with dynamically generated symbols

TIPS:
- Use this tool for precise code navigation
- Combine with 'find-refs' to see all usages of a symbol
- Use 'symbols' tool first to see all symbols in a file
- Position accuracy is important for correct results`
)

func NewGoToDefTool(registry *treesitter.Registry, workingDir string) BaseTool {
	return &goToDefTool{
		registry:   registry,
		workingDir: workingDir,
	}
}

func (g *goToDefTool) Name() string {
	return GoToDefToolName
}

func (g *goToDefTool) Info() ToolInfo {
	return ToolInfo{
		Name:        GoToDefToolName,
		Description: goToDefDescription,
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the source code file (relative to current working directory)",
			},
			"line": map[string]any{
				"type":        "integer",
				"description": "Line number of the symbol (1-based)",
			},
			"column": map[string]any{
				"type":        "integer",
				"description": "Column number of the symbol (1-based)",
			},
		},
		Required: []string{"path", "line", "column"},
	}
}

func (g *goToDefTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params GoToDefParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Path == "" {
		return NewTextErrorResponse("path is required"), nil
	}

	if params.Line <= 0 || params.Column <= 0 {
		return NewTextErrorResponse("line and column must be positive integers"), nil
	}

	// Resolve file path
	filePath := filepath.Join(g.workingDir, params.Path)
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(g.workingDir, params.Path)
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return NewTextErrorResponse(fmt.Sprintf("file does not exist: %s", params.Path)), nil
	}

	// Read file content
	source, err := os.ReadFile(filePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error reading file: %s", err)), nil
	}

	// Find definition
	result, err := g.findDefinition(params, string(source))
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error finding definition: %s", err)), nil
	}

	// Format response
	output := g.formatDefinitionResult(result, params)

	return WithResponseMetadata(
		NewTextResponse(output),
		GoToDefResponseMetadata{
			SearchTime:  0, // TODO: track timing
			SymbolFound: result.Found,
			ExactMatch:  result.Found,
		},
	), nil
}

func (g *goToDefTool) findDefinition(params GoToDefParams, source string) (*DefinitionResult, error) {
	result := &DefinitionResult{
		File:  params.Path,
		Found: false,
	}

	// Extract symbols from the file
	symbols, _, err := g.registry.ExtractSymbols(params.Path, source)
	if err != nil {
		return nil, fmt.Errorf("failed to extract symbols: %w", err)
	}

	// Extract the identifier at the specified position
	identifier := g.extractIdentifierAtPosition(params.Line, params.Column, source)
	if identifier == "" {
		result.Context = fmt.Sprintf("No identifier found at position %d:%d", params.Line, params.Column)
		return result, nil
	}

	// Find the symbol with that name
	var targetSymbol *treesitter.Symbol
	for _, symbol := range symbols {
		if symbol.Name == identifier {
			targetSymbol = &symbol
			break
		}
	}

	if targetSymbol == nil {
		result.Context = fmt.Sprintf("Symbol '%s' not found in file", identifier)
		return result, nil
	}

	// Return the definition of the found symbol
	result.Found = true
	result.Symbol = targetSymbol.Name
	result.Type = string(targetSymbol.Type)
	result.Line = targetSymbol.Position.Line
	result.Column = targetSymbol.Position.Column
	result.Signature = g.createSymbolSignature(*targetSymbol)
	result.Context = g.getSymbolContext(source, *targetSymbol)

	return result, nil
}

func (g *goToDefTool) positionInSymbol(line, column int, symbol treesitter.Symbol, source string) bool {
	symbolLine := int(symbol.Position.Line)
	symbolCol := int(symbol.Position.Column)

	// Check if position is at or near the symbol start
	if symbolLine == line && abs(symbolCol-column) <= len(symbol.Name) {
		return true
	}

	// For more complex symbols, check if position is within the symbol's extent
	// This is a simplified check - in practice, you'd use the AST node extent
	if symbol.Type == treesitter.SymbolTypeFunction || symbol.Type == treesitter.SymbolTypeMethod {
		// Check if we're within the function signature area
		if symbolLine == line && column >= symbolCol && column <= symbolCol+50 { // Rough estimate
			return true
		}
	}

	return false
}

func (g *goToDefTool) findClosestSymbol(line, column int, symbols []treesitter.Symbol) *treesitter.Symbol {
	var closest *treesitter.Symbol
	minDistance := int(^uint(0) >> 1) // Max int

	for _, symbol := range symbols {
		symbolLine := int(symbol.Position.Line)
		symbolCol := int(symbol.Position.Column)

		// Calculate Manhattan distance
		distance := abs(symbolLine-line) + abs(symbolCol-column)

		if distance < minDistance {
			minDistance = distance
			closest = &symbol
		}
	}

	return closest
}

func (g *goToDefTool) extractIdentifierAtPosition(line, column int, source string) string {
	lines := strings.Split(source, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}

	// Get the line (convert to 0-based)
	lineText := lines[line-1]
	if column < 1 || column > len(lineText)+1 {
		return ""
	}

	// Find the identifier at the column position
	// Start from the column and expand left and right to find word boundaries
	start := column - 1
	end := column - 1

	// Expand left to find start of identifier
	for start > 0 && isIdentifierChar(rune(lineText[start-1])) {
		start--
	}

	// Expand right to find end of identifier
	for end < len(lineText) && isIdentifierChar(rune(lineText[end])) {
		end++
	}

	if start < end {
		return lineText[start:end]
	}

	return ""
}

func (g *goToDefTool) createSymbolSignature(symbol treesitter.Symbol) string {
	var signature strings.Builder

	switch symbol.Type {
	case treesitter.SymbolTypeFunction:
		signature.WriteString("func ")
		signature.WriteString(symbol.Name)
	case treesitter.SymbolTypeMethod:
		signature.WriteString("func (")
		// Add receiver if available
		signature.WriteString(") ")
		signature.WriteString(symbol.Name)
	case treesitter.SymbolTypeVariable:
		signature.WriteString("var ")
		signature.WriteString(symbol.Name)
		if symbol.ReturnType != "" {
			signature.WriteString(" ")
			signature.WriteString(symbol.ReturnType)
		}
	case treesitter.SymbolTypeConstant:
		signature.WriteString("const ")
		signature.WriteString(symbol.Name)
		if symbol.ReturnType != "" {
			signature.WriteString(" ")
			signature.WriteString(symbol.ReturnType)
		}
	case treesitter.SymbolTypeClass:
		signature.WriteString("class ")
		signature.WriteString(symbol.Name)
	case treesitter.SymbolTypeInterface:
		signature.WriteString("interface ")
		signature.WriteString(symbol.Name)
	case treesitter.SymbolTypeType:
		signature.WriteString("type ")
		signature.WriteString(symbol.Name)
	default:
		signature.WriteString(string(symbol.Type))
		signature.WriteString(" ")
		signature.WriteString(symbol.Name)
	}

	// Add parameters for functions/methods
	if (symbol.Type == treesitter.SymbolTypeFunction || symbol.Type == treesitter.SymbolTypeMethod) && len(symbol.Parameters) > 0 {
		params := make([]string, len(symbol.Parameters))
		for i, param := range symbol.Parameters {
			if param.Type != "" {
				params[i] = fmt.Sprintf("%s %s", param.Name, param.Type)
			} else {
				params[i] = param.Name
			}
		}
		signature.WriteString("(")
		signature.WriteString(strings.Join(params, ", "))
		signature.WriteString(")")
	}

	// Add return type for functions/methods
	if (symbol.Type == treesitter.SymbolTypeFunction || symbol.Type == treesitter.SymbolTypeMethod) && symbol.ReturnType != "" {
		signature.WriteString(" ")
		signature.WriteString(symbol.ReturnType)
	}

	return signature.String()
}

func (g *goToDefTool) getSymbolContext(source string, symbol treesitter.Symbol) string {
	lines := strings.Split(source, "\n")
	symbolLine := int(symbol.Position.Line) - 1 // Convert to 0-based

	if symbolLine < 0 || symbolLine >= len(lines) {
		return "Symbol context not available"
	}

	// Get context around the symbol
	start := max(0, symbolLine-2)
	end := min(len(lines)-1, symbolLine+3)

	contextLines := lines[start : end+1]

	// Highlight the symbol line
	if symbolLine >= start && symbolLine <= end {
		lineIndex := symbolLine - start
		parts := strings.Split(contextLines[lineIndex], "")
		symbolCol := int(symbol.Position.Column) - 1
		symbolEnd := symbolCol + len(symbol.Name)

		if symbolCol >= 0 && symbolEnd < len(parts) {
			// Add highlighting (simple text markers)
			parts[symbolCol] = "▶" + parts[symbolCol]
			parts[symbolEnd-1] = parts[symbolEnd-1] + "◀"
			contextLines[lineIndex] = strings.Join(parts, "")
		}
	}

	return strings.Join(contextLines, "\n")
}

func (g *goToDefTool) formatDefinitionResult(result *DefinitionResult, params GoToDefParams) string {
	var output strings.Builder

	if !result.Found {
		output.WriteString(fmt.Sprintf("❌ No symbol found at position %s:%d:%d\n", params.Path, params.Line, params.Column))
		output.WriteString(result.Context)
		return output.String()
	}

	output.WriteString(fmt.Sprintf("🎯 Found definition for '%s'\n", result.Symbol))
	output.WriteString(fmt.Sprintf("📁 File: %s\n", result.File))
	output.WriteString(fmt.Sprintf("📍 Location: Line %d, Column %d\n", result.Line, result.Column))
	output.WriteString(fmt.Sprintf("🏷️  Type: %s\n", result.Type))

	if result.Signature != "" {
		output.WriteString(fmt.Sprintf("📝 Signature: %s\n", result.Signature))
	}

	output.WriteString("\n💻 Context:\n")
	output.WriteString("```\n")
	output.WriteString(result.Context)
	output.WriteString("\n```\n")

	return output.String()
}
