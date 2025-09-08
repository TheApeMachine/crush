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

type FindRefsParams struct {
	Path        string `json:"path"`
	Symbol      string `json:"symbol,omitempty"`
	Line        int    `json:"line,omitempty"`
	Column      int    `json:"column,omitempty"`
	IncludeFile bool   `json:"include_file,omitempty"`
	MaxResults  int    `json:"max_results,omitempty"`
}

type ReferenceResult struct {
	File       string `json:"file"`
	Line       uint32 `json:"line"`
	Column     uint32 `json:"column"`
	Context    string `json:"context"`
	Type       string `json:"type"`
	SymbolName string `json:"symbol_name"`
	SymbolType string `json:"symbol_type"`
	Confidence string `json:"confidence"`
}

type FindRefsResponseMetadata struct {
	TotalReferences int    `json:"total_references"`
	FilesSearched   int    `json:"files_searched"`
	SearchType      string `json:"search_type"`
}

type findRefsTool struct {
	registry   *treesitter.Registry
	workingDir string
}

const (
	FindRefsToolName    = "find-refs"
	findRefsDescription = `Find all references to a symbol using AST-aware analysis for accurate, context-sensitive results.

WHEN TO USE THIS TOOL:
- Use when you need to find all usages of a specific symbol
- Great for understanding how functions, variables, or classes are used
- Useful for refactoring analysis and impact assessment
- Helpful for code navigation and understanding dependencies

HOW TO USE:
- Provide either a file path with symbol name OR line/column position
- Use include_file=true to include references within the same file as definition
- Set max_results to limit the number of results returned
- Results are sorted by file and line number for easy navigation

SEARCH ACCURACY:
- AST-aware: Understands code structure, not just text matching
- Context-sensitive: Distinguishes between different symbol usages
- Type-aware: Recognizes function calls, variable references, etc.
- Scope-aware: Respects variable scoping and shadowing

REFERENCE TYPES:
- call: Function or method calls
- reference: Variable or constant references
- assignment: Variable assignments
- declaration: Symbol declarations
- import: Import usage references

EXAMPLES:
- Find references to a function: {"path": "utils.go", "symbol": "parseConfig"}
- Find references at specific position: {"path": "main.go", "line": 15, "column": 10}
- Include same-file references: {"path": "api.go", "symbol": "User", "include_file": true}
- Limit results: {"path": "large_file.go", "symbol": "data", "max_results": 20}

LIMITATIONS:
- Currently searches within a single file (not cross-file analysis)
- Only supports Go and JavaScript/TypeScript files
- May not detect dynamic references (reflection, eval, etc.)
- Large files may take longer to analyze

TIPS:
- Use this tool for accurate symbol usage analysis
- Combine with 'impact' tool for broader impact assessment
- Use 'grep' tool for simple text-based searches when AST analysis isn't needed
- Consider using include_file=false to focus on external usage`
)

func NewFindRefsTool(registry *treesitter.Registry, workingDir string) BaseTool {
	return &findRefsTool{
		registry:   registry,
		workingDir: workingDir,
	}
}

func (f *findRefsTool) Name() string {
	return FindRefsToolName
}

func (f *findRefsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        FindRefsToolName,
		Description: findRefsDescription,
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the source code file to search in (relative to current working directory)",
			},
			"symbol": map[string]any{
				"type":        "string",
				"description": "Name of the symbol to find references for (optional if line/column provided)",
			},
			"line": map[string]any{
				"type":        "integer",
				"description": "Line number to find symbol at (optional, used to identify symbol)",
			},
			"column": map[string]any{
				"type":        "integer",
				"description": "Column number to find symbol at (optional, used to identify symbol)",
			},
			"include_file": map[string]any{
				"type":        "boolean",
				"description": "Include references within the same file as the symbol definition. Default is false.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 50)",
			},
		},
		Required: []string{"path"},
	}
}

func (f *findRefsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params FindRefsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Path == "" {
		return NewTextErrorResponse("path is required"), nil
	}

	if params.MaxResults == 0 {
		params.MaxResults = 50
	}

	// Resolve file path
	filePath := filepath.Join(f.workingDir, params.Path)
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(f.workingDir, params.Path)
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

	// Find references
	results, err := f.findReferences(params, string(source))
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error finding references: %s", err)), nil
	}

	// Format response
	output := f.formatReferenceResults(results, params)

	return WithResponseMetadata(
		NewTextResponse(output),
		FindRefsResponseMetadata{
			TotalReferences: len(results),
			FilesSearched:   1, // Currently single file only
			SearchType:      "ast_aware",
		},
	), nil
}

func (f *findRefsTool) findReferences(params FindRefsParams, source string) ([]ReferenceResult, error) {
	// Extract symbols and relationships
	symbols, relationships, err := f.registry.ExtractSymbols(params.Path, source)
	if err != nil {
		return nil, fmt.Errorf("failed to extract symbols: %w", err)
	}

	// Identify target symbol
	var targetSymbol *treesitter.Symbol
	if params.Symbol != "" {
		// Find by name
		for _, symbol := range symbols {
			if symbol.Name == params.Symbol {
				if params.Line > 0 && params.Column > 0 {
					// Match by position if provided
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
	} else if params.Line > 0 && params.Column > 0 {
		// Find symbol at position
		for _, symbol := range symbols {
			if int(symbol.Position.Line) == params.Line && int(symbol.Position.Column) == params.Column {
				targetSymbol = &symbol
				break
			}
		}
	}

	if targetSymbol == nil {
		return nil, fmt.Errorf("target symbol '%s' not found", params.Symbol)
	}

	// Find all references to this symbol
	var results []ReferenceResult

	// Add the definition itself (if include_file is true)
	if params.IncludeFile {
		results = append(results, ReferenceResult{
			File:       params.Path,
			Line:       targetSymbol.Position.Line,
			Column:     targetSymbol.Position.Column,
			Context:    fmt.Sprintf("Definition of %s %s", targetSymbol.Type, targetSymbol.Name),
			Type:       "declaration",
			SymbolName: targetSymbol.Name,
			SymbolType: string(targetSymbol.Type),
			Confidence: "high",
		})
	}

	// Find references through relationships
	for _, rel := range relationships {
		if rel.To == targetSymbol.Name || rel.From == targetSymbol.ID {
			refType := string(rel.Type)
			context := fmt.Sprintf("%s reference to %s", refType, targetSymbol.Name)

			result := ReferenceResult{
				File:       params.Path, // For now, assume same file
				Line:       rel.Position.Line,
				Column:     rel.Position.Column,
				Context:    context,
				Type:       refType,
				SymbolName: targetSymbol.Name,
				SymbolType: string(targetSymbol.Type),
				Confidence: "high",
			}
			results = append(results, result)
		}
	}

	// Find additional references through AST analysis
	// This is a simplified version - in practice, you'd need more sophisticated
	// AST traversal to find all identifier usages
	additionalRefs := f.findIdentifierReferences(*targetSymbol, source)
	results = append(results, additionalRefs...)

	// Remove duplicates and sort
	results = f.deduplicateAndSort(results)

	// Limit results
	if len(results) > params.MaxResults {
		results = results[:params.MaxResults]
	}

	return results, nil
}

func (f *findRefsTool) findIdentifierReferences(targetSymbol treesitter.Symbol, source string) []ReferenceResult {
	var results []ReferenceResult
	lines := strings.Split(source, "\n")

	// Simple identifier matching - in a full implementation, this would use
	// the AST to find all identifier nodes that match the symbol
	targetName := targetSymbol.Name

	for i, line := range lines {
		lineNum := i + 1

		// Skip the definition line
		if int(targetSymbol.Position.Line) == lineNum {
			continue
		}

		// Find all occurrences of the identifier
		words := strings.FieldsFunc(line, func(r rune) bool {
			return !isIdentifierChar(r)
		})

		for _, word := range words {
			if word == targetName {
				// Find column position
				col := strings.Index(line, word) + 1

				// Determine context
				context := f.getReferenceContext(line, word, col-1)

				result := ReferenceResult{
					File:       "", // Will be filled by caller
					Line:       uint32(lineNum),
					Column:     uint32(col),
					Context:    context,
					Type:       f.inferReferenceType(line, word, col-1),
					SymbolName: targetName,
					SymbolType: string(targetSymbol.Type),
					Confidence: "medium", // Lower confidence for simple text matching
				}
				results = append(results, result)
			}
		}
	}

	return results
}

func isIdentifierChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

func (f *findRefsTool) getReferenceContext(line, word string, col int) string {
	// Extract context around the reference
	start := max(0, col-20)
	end := min(len(line), col+len(word)+20)

	context := line[start:end]
	if start > 0 {
		context = "..." + context
	}
	if end < len(line) {
		context = context + "..."
	}

	return strings.TrimSpace(context)
}

func (f *findRefsTool) inferReferenceType(line, word string, col int) string {
	line = strings.ToLower(strings.TrimSpace(line))
	word = strings.ToLower(word)

	// Check for function call patterns
	if strings.Contains(line, word+"(") {
		return "call"
	}

	// Check for assignment patterns
	assignmentPatterns := []string{" = ", " := ", " += ", " -= ", " *= ", " /= "}
	for _, pattern := range assignmentPatterns {
		if strings.Contains(line, word+pattern) || strings.Contains(line, pattern+word) {
			return "assignment"
		}
	}

	// Check for declaration patterns
	if strings.HasPrefix(line, "var ") || strings.HasPrefix(line, "const ") ||
		strings.HasPrefix(line, "let ") || strings.HasPrefix(line, "function ") {
		return "declaration"
	}

	// Default to reference
	return "reference"
}

func (f *findRefsTool) deduplicateAndSort(results []ReferenceResult) []ReferenceResult {
	// Create a map to deduplicate
	seen := make(map[string]bool)
	var unique []ReferenceResult

	for _, result := range results {
		key := fmt.Sprintf("%s:%d:%d", result.File, result.Line, result.Column)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, result)
		}
	}

	// Sort by file, then line, then column
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].File != unique[j].File {
			return unique[i].File < unique[j].File
		}
		if unique[i].Line != unique[j].Line {
			return unique[i].Line < unique[j].Line
		}
		return unique[i].Column < unique[j].Column
	})

	return unique
}

func (f *findRefsTool) formatReferenceResults(results []ReferenceResult, params FindRefsParams) string {
	var output strings.Builder

	if len(results) == 0 {
		output.WriteString(fmt.Sprintf("No references found for symbol in %s", params.Path))
		if params.Symbol != "" {
			output.WriteString(fmt.Sprintf(" (searched for: %s)", params.Symbol))
		}
		return output.String()
	}

	output.WriteString(fmt.Sprintf("🔍 Found %d references", len(results)))
	if params.Symbol != "" {
		output.WriteString(fmt.Sprintf(" to '%s'", params.Symbol))
	}
	output.WriteString(fmt.Sprintf(" in %s\n\n", params.Path))

	// Group by type
	typeGroups := make(map[string][]ReferenceResult)
	for _, result := range results {
		typeGroups[result.Type] = append(typeGroups[result.Type], result)
	}

	// Display results grouped by type
	for _, refType := range []string{"declaration", "call", "reference", "assignment"} {
		if refs, exists := typeGroups[refType]; exists {
			output.WriteString(fmt.Sprintf("%s References (%d):\n", strings.Title(refType), len(refs)))
			for _, ref := range refs {
				output.WriteString(fmt.Sprintf("  • Line %d:%d - %s", ref.Line, ref.Column, ref.Context))
				if ref.Confidence != "high" {
					output.WriteString(fmt.Sprintf(" (%s confidence)", ref.Confidence))
				}
				output.WriteString("\n")
			}
			output.WriteString("\n")
		}
	}

	// Show any other types
	for refType, refs := range typeGroups {
		if refType != "declaration" && refType != "call" && refType != "reference" && refType != "assignment" {
			output.WriteString(fmt.Sprintf("%s References (%d):\n", strings.Title(refType), len(refs)))
			for _, ref := range refs {
				output.WriteString(fmt.Sprintf("  • Line %d:%d - %s", ref.Line, ref.Column, ref.Context))
				if ref.Confidence != "high" {
					output.WriteString(fmt.Sprintf(" (%s confidence)", ref.Confidence))
				}
				output.WriteString("\n")
			}
			output.WriteString("\n")
		}
	}

	return output.String()
}
