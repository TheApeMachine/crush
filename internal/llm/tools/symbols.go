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

type SymbolsParams struct {
	Path        string `json:"path"`
	SymbolType  string `json:"symbol_type,omitempty"`
	IncludeDeps bool   `json:"include_deps,omitempty"`
}

type SymbolInfo struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Line       uint32          `json:"line"`
	Column     uint32          `json:"column"`
	Scope      string          `json:"scope,omitempty"`
	Visibility string          `json:"visibility,omitempty"`
	ReturnType string          `json:"return_type,omitempty"`
	Parameters []ParameterInfo `json:"parameters,omitempty"`
}

type ParameterInfo struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type SymbolsResponseMetadata struct {
	FilePath    string `json:"file_path"`
	Language    string `json:"language"`
	SymbolCount int    `json:"symbol_count"`
	Filtered    bool   `json:"filtered,omitempty"`
}

type symbolsTool struct {
	registry   *treesitter.Registry
	workingDir string
}

const (
	SymbolsToolName    = "symbols"
	symbolsDescription = `Extract and list top-level symbols from source code files using TreeSitter-powered AST analysis.

WHEN TO USE THIS TOOL:
- Use when you need to understand the structure of a source code file
- Great for exploring APIs, finding functions/classes, or understanding code organization
- Useful for getting an overview of what symbols are defined in a file
- Helpful for code navigation and understanding dependencies

HOW TO USE:
- Provide the path to a source code file (relative to current working directory)
- Optionally filter by symbol type (function, variable, class, method, etc.)
- Optionally include dependencies (imports) in the results
- Results are sorted by line number for easy navigation

SUPPORTED SYMBOL TYPES:
- function - Functions and methods
- variable - Variables and constants
- class - Classes and interfaces
- type - Type definitions
- import - Import statements
- constant - Constants

SUPPORTED LANGUAGES:
- Go (.go files)
- JavaScript/TypeScript (.js, .jsx, .ts, .tsx files)

EXAMPLES:
- List all symbols in main.go: {"path": "main.go"}
- List only functions in utils.js: {"path": "utils.js", "symbol_type": "function"}
- Include imports in results: {"path": "app.go", "include_deps": true}

LIMITATIONS:
- Only supports Go and JavaScript/TypeScript files
- Results are limited to top-level symbols (not nested/local symbols)
- Large files may take longer to process
- Binary files are not supported

TIPS:
- Use this tool first to understand file structure before making changes
- Combine with other tools like 'grep' for more detailed analysis
- Use 'impact' tool to understand how symbols are used across the codebase`
)

func NewSymbolsTool(registry *treesitter.Registry, workingDir string) BaseTool {
	return &symbolsTool{
		registry:   registry,
		workingDir: workingDir,
	}
}

func (s *symbolsTool) Name() string {
	return SymbolsToolName
}

func (s *symbolsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        SymbolsToolName,
		Description: symbolsDescription,
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the source code file to analyze (relative to current working directory)",
			},
			"symbol_type": map[string]any{
				"type":        "string",
				"description": "Optional filter for symbol type (function, variable, class, method, type, import, constant)",
			},
			"include_deps": map[string]any{
				"type":        "boolean",
				"description": "Include import/dependency symbols in results. Default is false.",
			},
		},
		Required: []string{"path"},
	}
}

func (s *symbolsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params SymbolsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Path == "" {
		return NewTextErrorResponse("path is required"), nil
	}

	// Resolve the file path
	filePath := filepath.Join(s.workingDir, params.Path)
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.workingDir, params.Path)
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

	// Extract symbols using TreeSitter
	symbols, _, err := s.registry.ExtractSymbols(params.Path, string(source))
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error extracting symbols: %s", err)), nil
	}

	// Filter symbols based on parameters
	var filteredSymbols []treesitter.Symbol
	for _, symbol := range symbols {
		// Filter by symbol type if specified
		if params.SymbolType != "" && string(symbol.Type) != params.SymbolType {
			continue
		}

		// Filter out imports unless explicitly requested
		if !params.IncludeDeps && symbol.Type == treesitter.SymbolTypeImport {
			continue
		}

		filteredSymbols = append(filteredSymbols, symbol)
	}

	// Sort symbols by line number
	sort.Slice(filteredSymbols, func(i, j int) bool {
		if filteredSymbols[i].Position.Line == filteredSymbols[j].Position.Line {
			return filteredSymbols[i].Position.Column < filteredSymbols[j].Position.Column
		}
		return filteredSymbols[i].Position.Line < filteredSymbols[j].Position.Line
	})

	// Convert to response format
	var symbolInfos []SymbolInfo
	for _, symbol := range filteredSymbols {
		info := SymbolInfo{
			Name:       symbol.Name,
			Type:       string(symbol.Type),
			Line:       symbol.Position.Line,
			Column:     symbol.Position.Column,
			Scope:      string(symbol.Scope),
			Visibility: string(symbol.Visibility),
			ReturnType: symbol.ReturnType,
		}

		// Convert parameters
		for _, param := range symbol.Parameters {
			info.Parameters = append(info.Parameters, ParameterInfo{
				Name: param.Name,
				Type: param.Type,
			})
		}

		symbolInfos = append(symbolInfos, info)
	}

	// Build response
	var output strings.Builder
	if len(symbolInfos) == 0 {
		output.WriteString(fmt.Sprintf("No symbols found in %s", params.Path))
		if params.SymbolType != "" {
			output.WriteString(fmt.Sprintf(" (filtered by type: %s)", params.SymbolType))
		}
	} else {
		output.WriteString(fmt.Sprintf("Symbols in %s:\n", params.Path))
		if params.SymbolType != "" {
			output.WriteString(fmt.Sprintf(" (filtered by type: %s)\n", params.SymbolType))
		}
		output.WriteString("\n")

		for _, info := range symbolInfos {
			output.WriteString(fmt.Sprintf("• %s (%s) at line %d", info.Name, info.Type, info.Line))

			if info.ReturnType != "" {
				output.WriteString(fmt.Sprintf(" -> %s", info.ReturnType))
			}

			if len(info.Parameters) > 0 {
				params := make([]string, len(info.Parameters))
				for i, param := range info.Parameters {
					if param.Type != "" {
						params[i] = fmt.Sprintf("%s %s", param.Name, param.Type)
					} else {
						params[i] = param.Name
					}
				}
				output.WriteString(fmt.Sprintf(" (%s)", strings.Join(params, ", ")))
			}

			output.WriteString("\n")
		}
	}

	// Get language info
	lang, err := s.registry.DetectLanguage(params.Path)
	language := "unknown"
	if err == nil {
		language = string(lang)
	}

	return WithResponseMetadata(
		NewTextResponse(output.String()),
		SymbolsResponseMetadata{
			FilePath:    params.Path,
			Language:    language,
			SymbolCount: len(symbolInfos),
			Filtered:    params.SymbolType != "" || !params.IncludeDeps,
		},
	), nil
}
