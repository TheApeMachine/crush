package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/crush/internal/treesitter"
)

type ContextPackerParams struct {
	Path         string `json:"path"`
	Symbol       string `json:"symbol,omitempty"`
	Line         int    `json:"line,omitempty"`
	Column       int    `json:"column,omitempty"`
	ContextLines int    `json:"context_lines,omitempty"`
	IncludeDeps  bool   `json:"include_deps,omitempty"`
	MaxSize      int    `json:"max_size,omitempty"`
}

type ContextBundle struct {
	File           string          `json:"file"`
	Language       string          `json:"language"`
	Symbol         string          `json:"symbol,omitempty"`
	Imports        []string        `json:"imports"`
	SymbolAST      *SymbolASTInfo  `json:"symbol_ast,omitempty"`
	RelatedSymbols []RelatedSymbol `json:"related_symbols"`
	ContextCode    string          `json:"context_code"`
	Summary        string          `json:"summary"`
}

type SymbolASTInfo struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Signature  string          `json:"signature"`
	Body       string          `json:"body,omitempty"`
	Parameters []ParameterInfo `json:"parameters,omitempty"`
	ReturnType string          `json:"return_type,omitempty"`
	LineStart  uint32          `json:"line_start"`
	LineEnd    uint32          `json:"line_end"`
}

type RelatedSymbol struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Relation string `json:"relation"`
	Line     uint32 `json:"line"`
	Context  string `json:"context,omitempty"`
}

type ContextPackerResponseMetadata struct {
	BundleSize   int  `json:"bundle_size_chars"`
	SymbolsFound int  `json:"symbols_found"`
	Compressed   bool `json:"compressed,omitempty"`
}

type contextPackerTool struct {
	registry   *treesitter.Registry
	workingDir string
}

const (
	ContextPackerToolName    = "context-packer"
	contextPackerDescription = `Create a compact, focused context bundle for code understanding and modification using TreeSitter-powered analysis.

WHEN TO USE THIS TOOL:
- Use when you need focused context for code understanding
- Great for providing LLMs with relevant code context for modifications
- Useful for understanding symbol relationships and dependencies
- Helpful for code review and debugging scenarios

HOW TO USE:
- Provide a file path and optionally a specific symbol
- Specify context_lines to control how much surrounding code to include
- Use include_deps to include related symbols and dependencies
- Set max_size to limit the bundle size for large files
- Results include symbol AST, imports, and related context

CONTEXT BUNDLE CONTENTS:
- Symbol AST information (signature, parameters, body)
- Import statements and dependencies
- Related symbols and their relationships
- Focused code context around the target symbol
- Summary of the context bundle

EXAMPLES:
- Get context for a specific function: {"path": "utils.go", "symbol": "parseConfig"}
- Get file-level context: {"path": "main.go", "context_lines": 5}
- Include dependencies: {"path": "api.go", "symbol": "User", "include_deps": true}
- Limit bundle size: {"path": "large_file.go", "max_size": 5000}

LIMITATIONS:
- Only supports Go and JavaScript/TypeScript files
- Context is limited to the specified file (not cross-file analysis)
- Large symbols may be truncated to fit size limits
- Binary files are not supported

TIPS:
- Use this tool before making code changes to understand context
- Adjust context_lines based on your needs (more lines = more context)
- Use include_deps for understanding symbol relationships
- Consider max_size for very large files to avoid overwhelming responses`
)

func NewContextPackerTool(registry *treesitter.Registry, workingDir string) BaseTool {
	return &contextPackerTool{
		registry:   registry,
		workingDir: workingDir,
	}
}

func (c *contextPackerTool) Name() string {
	return ContextPackerToolName
}

func (c *contextPackerTool) Info() ToolInfo {
	return ToolInfo{
		Name:        ContextPackerToolName,
		Description: contextPackerDescription,
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the source code file to analyze (relative to current working directory)",
			},
			"symbol": map[string]any{
				"type":        "string",
				"description": "Name of the specific symbol to focus on (optional, analyzes entire file if not provided)",
			},
			"line": map[string]any{
				"type":        "integer",
				"description": "Line number of the symbol (optional, helps disambiguate symbols)",
			},
			"column": map[string]any{
				"type":        "integer",
				"description": "Column number of the symbol (optional, helps disambiguate symbols)",
			},
			"context_lines": map[string]any{
				"type":        "integer",
				"description": "Number of lines of context to include around the symbol (default: 3)",
			},
			"include_deps": map[string]any{
				"type":        "boolean",
				"description": "Include related symbols and dependencies in the bundle. Default is false.",
			},
			"max_size": map[string]any{
				"type":        "integer",
				"description": "Maximum size of the context bundle in characters (default: 10000)",
			},
		},
		Required: []string{"path"},
	}
}

func (c *contextPackerTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params ContextPackerParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Path == "" {
		return NewTextErrorResponse("path is required"), nil
	}

	// Set defaults
	if params.ContextLines == 0 {
		params.ContextLines = 3
	}
	if params.MaxSize == 0 {
		params.MaxSize = 10000
	}

	// Resolve file path
	filePath := filepath.Join(c.workingDir, params.Path)
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(c.workingDir, params.Path)
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

	// Create context bundle
	bundle, err := c.createContextBundle(params, string(source))
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error creating context bundle: %s", err)), nil
	}

	// Format response
	output := c.formatContextBundle(bundle)

	symbolsFound := len(bundle.RelatedSymbols)
	if bundle.SymbolAST != nil {
		symbolsFound++
	}

	return WithResponseMetadata(
		NewTextResponse(output),
		ContextPackerResponseMetadata{
			BundleSize:   len(output),
			SymbolsFound: symbolsFound,
			Compressed:   len(output) > params.MaxSize,
		},
	), nil
}

func (c *contextPackerTool) createContextBundle(params ContextPackerParams, source string) (*ContextBundle, error) {
	bundle := &ContextBundle{
		File:           params.Path,
		RelatedSymbols: make([]RelatedSymbol, 0),
	}

	// Detect language
	lang, err := c.registry.DetectLanguage(params.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to detect language: %w", err)
	}
	bundle.Language = string(lang)

	// Extract symbols
	symbols, relationships, err := c.registry.ExtractSymbols(params.Path, source)
	if err != nil {
		return nil, fmt.Errorf("failed to extract symbols: %w", err)
	}

	// Extract imports
	bundle.Imports = c.extractImports(source, lang)

	// Find target symbol if specified
	var targetSymbol *treesitter.Symbol
	if params.Symbol != "" {
		for _, symbol := range symbols {
			// Case-insensitive matching
			if strings.EqualFold(symbol.Name, params.Symbol) {
				if params.Line > 0 && params.Column > 0 {
					if int(symbol.Position.Line) == params.Line && int(symbol.Position.Column) == params.Column {
						targetSymbol = &symbol
						break
					}
				} else {
					targetSymbol = &symbol
					break
				}
			}
		}
		if targetSymbol == nil {
			return nil, fmt.Errorf("symbol '%s' not found in file", params.Symbol)
		}
		bundle.Symbol = params.Symbol
	}

	// Create symbol AST info if we have a target symbol
	if targetSymbol != nil {
		bundle.SymbolAST = c.createSymbolASTInfo(*targetSymbol, source, params.ContextLines)
	}

	// Find related symbols if requested
	if params.IncludeDeps {
		bundle.RelatedSymbols = c.findRelatedSymbols(*targetSymbol, symbols, relationships, source)
	}

	// Create context code
	bundle.ContextCode = c.createContextCode(source, targetSymbol, params.ContextLines, params.MaxSize)

	// Create summary
	bundle.Summary = c.createBundleSummary(bundle)

	return bundle, nil
}

func (c *contextPackerTool) extractImports(source string, lang treesitter.Language) []string {
	var imports []string
	lines := strings.Split(source, "\n")

	switch lang {
	case treesitter.LanguageGo:
		inImportBlock := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "import") {
				if strings.Contains(line, "(") {
					inImportBlock = true
					continue
				} else {
					// Single import
					importPath := c.extractGoImport(line)
					if importPath != "" {
						imports = append(imports, importPath)
					}
				}
			} else if inImportBlock {
				if strings.Contains(line, ")") {
					inImportBlock = false
				} else {
					importPath := c.extractGoImport(line)
					if importPath != "" {
						imports = append(imports, importPath)
					}
				}
			}
		}
	case treesitter.LanguageJavaScript:
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "import") || strings.HasPrefix(line, "require(") {
				imports = append(imports, line)
			}
		}
	}

	return imports
}

func (c *contextPackerTool) extractGoImport(line string) string {
	// Remove import keyword and quotes
	line = strings.TrimPrefix(line, "import")
	line = strings.TrimSpace(line)
	line = strings.Trim(line, `"`)

	// Handle import with alias: alias "path"
	if strings.Contains(line, `"`) {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return parts[len(parts)-1]
		}
	}

	return line
}

func (c *contextPackerTool) createSymbolASTInfo(symbol treesitter.Symbol, source string, contextLines int) *SymbolASTInfo {
	lines := strings.Split(source, "\n")

	// Find the end of the symbol by looking for the next symbol or end of reasonable block
	lineStart := int(symbol.Position.Line) - 1 // Convert to 0-based
	lineEnd := lineStart

	// For functions/methods, try to find the end of the function body
	if symbol.Type == treesitter.SymbolTypeFunction || symbol.Type == treesitter.SymbolTypeMethod {
		lineEnd = c.findFunctionEnd(lines, lineStart)
	} else {
		// For other symbols, include a few lines of context
		lineEnd = lineStart + contextLines
	}

	if lineEnd >= len(lines) {
		lineEnd = len(lines) - 1
	}

	// Extract the symbol body
	bodyLines := lines[lineStart : lineEnd+1]
	body := strings.Join(bodyLines, "\n")

	// Create signature
	signature := c.createSymbolSignature(symbol)

	return &SymbolASTInfo{
		Name:       symbol.Name,
		Type:       string(symbol.Type),
		Signature:  signature,
		Body:       body,
		Parameters: c.convertParameters(symbol.Parameters),
		ReturnType: symbol.ReturnType,
		LineStart:  symbol.Position.Line,
		LineEnd:    uint32(lineEnd + 1), // Convert back to 1-based
	}
}

func (c *contextPackerTool) findFunctionEnd(lines []string, startLine int) int {
	braceCount := 0
	inFunction := false

	for i := startLine; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Count braces
		for _, char := range line {
			if char == '{' {
				braceCount++
				inFunction = true
			} else if char == '}' {
				braceCount--
			}
		}

		// If we found the matching closing brace, return
		if inFunction && braceCount == 0 {
			return i
		}
	}

	// If we didn't find the end, return a reasonable limit
	return startLine + 20
}

func (c *contextPackerTool) createSymbolSignature(symbol treesitter.Symbol) string {
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
	default:
		signature.WriteString(string(symbol.Type))
		signature.WriteString(" ")
		signature.WriteString(symbol.Name)
	}

	// Add parameters
	if len(symbol.Parameters) > 0 {
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

	// Add return type
	if symbol.ReturnType != "" && symbol.Type != treesitter.SymbolTypeVariable && symbol.Type != treesitter.SymbolTypeConstant {
		signature.WriteString(" ")
		signature.WriteString(symbol.ReturnType)
	}

	return signature.String()
}

func (c *contextPackerTool) convertParameters(params []treesitter.Parameter) []ParameterInfo {
	result := make([]ParameterInfo, len(params))
	for i, param := range params {
		result[i] = ParameterInfo{
			Name: param.Name,
			Type: param.Type,
		}
	}
	return result
}

func (c *contextPackerTool) findRelatedSymbols(targetSymbol treesitter.Symbol, symbols []treesitter.Symbol, relationships []treesitter.Relationship, source string) []RelatedSymbol {
	var related []RelatedSymbol
	lines := strings.Split(source, "\n")

	// Find symbols that are related through relationships
	for _, rel := range relationships {
		if rel.From == targetSymbol.ID || rel.To == targetSymbol.Name {
			// Find the related symbol
			for _, symbol := range symbols {
				if symbol.ID == rel.From || symbol.Name == rel.To {
					related = append(related, RelatedSymbol{
						Name:     symbol.Name,
						Type:     string(symbol.Type),
						Relation: string(rel.Type),
						Line:     symbol.Position.Line,
						Context:  c.getSymbolContext(lines, int(symbol.Position.Line)-1, 1),
					})
					break
				}
			}
		}
	}

	// Find symbols in the same scope/area
	targetLine := int(targetSymbol.Position.Line) - 1
	for _, symbol := range symbols {
		symbolLine := int(symbol.Position.Line) - 1
		if symbol.ID != targetSymbol.ID && abs(symbolLine-targetLine) <= 10 {
			related = append(related, RelatedSymbol{
				Name:     symbol.Name,
				Type:     string(symbol.Type),
				Relation: "nearby",
				Line:     symbol.Position.Line,
				Context:  c.getSymbolContext(lines, symbolLine, 1),
			})
		}
	}

	// Sort by line number
	sort.Slice(related, func(i, j int) bool {
		return related[i].Line < related[j].Line
	})

	return related
}

func (c *contextPackerTool) getSymbolContext(lines []string, lineNum, contextLines int) string {
	start := max(0, lineNum-contextLines)
	end := min(len(lines)-1, lineNum+contextLines)

	contextLinesSlice := lines[start : end+1]
	return strings.Join(contextLinesSlice, "\n")
}

func (c *contextPackerTool) createContextCode(source string, targetSymbol *treesitter.Symbol, contextLines, maxSize int) string {
	lines := strings.Split(source, "\n")

	if targetSymbol == nil {
		// Return first part of file
		end := min(len(lines), 50)
		return strings.Join(lines[:end], "\n")
	}

	// Focus on the target symbol with context
	targetLine := int(targetSymbol.Position.Line) - 1
	start := max(0, targetLine-contextLines)
	end := min(len(lines)-1, targetLine+contextLines+10) // Include more lines for function body

	contextCode := strings.Join(lines[start:end+1], "\n")

	// Truncate if too large
	if len(contextCode) > maxSize {
		contextCode = contextCode[:maxSize] + "\n... (truncated)"
	}

	return contextCode
}

func (c *contextPackerTool) createBundleSummary(bundle *ContextBundle) string {
	var summary strings.Builder

	summary.WriteString(fmt.Sprintf("Context bundle for %s (%s)", bundle.File, bundle.Language))

	if bundle.Symbol != "" {
		summary.WriteString(fmt.Sprintf(" focusing on symbol '%s'", bundle.Symbol))
	}

	summary.WriteString(fmt.Sprintf(". Contains %d imports", len(bundle.Imports)))

	if bundle.SymbolAST != nil {
		summary.WriteString(fmt.Sprintf(" and symbol AST for %s", bundle.SymbolAST.Type))
	}

	if len(bundle.RelatedSymbols) > 0 {
		summary.WriteString(fmt.Sprintf(" with %d related symbols", len(bundle.RelatedSymbols)))
	}

	return summary.String()
}

func (c *contextPackerTool) formatContextBundle(bundle *ContextBundle) string {
	var output strings.Builder

	output.WriteString(fmt.Sprintf("📦 Context Bundle: %s\n", bundle.Summary))
	output.WriteString(fmt.Sprintf("📁 File: %s (%s)\n", bundle.File, bundle.Language))

	if len(bundle.Imports) > 0 {
		output.WriteString("\n📚 Imports:\n")
		for _, imp := range bundle.Imports {
			output.WriteString(fmt.Sprintf("  • %s\n", imp))
		}
	}

	if bundle.SymbolAST != nil {
		output.WriteString("\n🔍 Symbol AST:\n")
		output.WriteString(fmt.Sprintf("  Name: %s\n", bundle.SymbolAST.Name))
		output.WriteString(fmt.Sprintf("  Type: %s\n", bundle.SymbolAST.Type))
		output.WriteString(fmt.Sprintf("  Signature: %s\n", bundle.SymbolAST.Signature))
		output.WriteString(fmt.Sprintf("  Lines: %d-%d\n", bundle.SymbolAST.LineStart, bundle.SymbolAST.LineEnd))

		if len(bundle.SymbolAST.Parameters) > 0 {
			output.WriteString("  Parameters:\n")
			for _, param := range bundle.SymbolAST.Parameters {
				output.WriteString(fmt.Sprintf("    • %s", param.Name))
				if param.Type != "" {
					output.WriteString(fmt.Sprintf(" (%s)", param.Type))
				}
				output.WriteString("\n")
			}
		}

		if bundle.SymbolAST.ReturnType != "" {
			output.WriteString(fmt.Sprintf("  Return Type: %s\n", bundle.SymbolAST.ReturnType))
		}
	}

	if len(bundle.RelatedSymbols) > 0 {
		output.WriteString("\n🔗 Related Symbols:\n")
		for _, rel := range bundle.RelatedSymbols {
			output.WriteString(fmt.Sprintf("  • %s (%s) - %s at line %d\n", rel.Name, rel.Type, rel.Relation, rel.Line))
			if rel.Context != "" {
				// Indent context
				contextLines := strings.Split(rel.Context, "\n")
				for _, line := range contextLines {
					output.WriteString(fmt.Sprintf("      %s\n", line))
				}
			}
		}
	}

	output.WriteString("\n💻 Context Code:\n")
	output.WriteString("```\n")
	output.WriteString(bundle.ContextCode)
	output.WriteString("\n```\n")

	return output.String()
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
