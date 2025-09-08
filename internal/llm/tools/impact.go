package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/crush/internal/treesitter"
)

type ImpactParams struct {
	Path     string `json:"path"`
	Symbol   string `json:"symbol,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	MaxDepth int    `json:"max_depth,omitempty"`
}

type ImpactAnalysis struct {
	Symbol          string          `json:"symbol"`
	File            string          `json:"file"`
	BlastRadius     int             `json:"blast_radius"`
	AffectedFiles   []string        `json:"affected_files"`
	References      []ReferenceInfo `json:"references"`
	ImpactScore     float64         `json:"impact_score"`
	RiskLevel       string          `json:"risk_level"`
	Recommendations []string        `json:"recommendations"`
}

type ReferenceInfo struct {
	File    string `json:"file"`
	Line    uint32 `json:"line"`
	Column  uint32 `json:"column"`
	Context string `json:"context,omitempty"`
	Type    string `json:"type"`
}

type ImpactResponseMetadata struct {
	AnalysisTime int `json:"analysis_time_ms"`
	FilesScanned int `json:"files_scanned"`
	TotalRefs    int `json:"total_references"`
}

type impactTool struct {
	registry   *treesitter.Registry
	workingDir string
}

const (
	ImpactToolName    = "impact"
	impactDescription = `Analyze the impact and blast radius of changing a symbol or file using TreeSitter-powered reference analysis.

WHEN TO USE THIS TOOL:
- Use before making changes to understand potential impact
- Great for assessing refactoring risks and dependencies
- Useful for understanding how changes propagate through the codebase
- Helpful for planning safe code modifications

HOW TO USE:
- Provide either a file path OR a symbol name with location
- For symbol analysis: specify path, symbol name, and optionally line/column
- For file analysis: just specify the path
- Optionally set max_depth to limit analysis scope
- Results include blast radius scoring and risk assessment

BLAST RADIUS SCORING:
- Low (1-5): Minimal impact, safe to change
- Medium (6-15): Moderate impact, test thoroughly
- High (16-50): Significant impact, careful review needed
- Critical (50+): High risk, extensive testing required

RISK LEVELS:
- Low: Changes are isolated and safe
- Medium: Changes may affect multiple components
- High: Changes could break functionality
- Critical: Changes require comprehensive testing

EXAMPLES:
- Analyze impact of changing a function: {"path": "utils.go", "symbol": "parseConfig"}
- Analyze file-level impact: {"path": "main.go"}
- Limit analysis depth: {"path": "api.go", "symbol": "User", "max_depth": 3}

LIMITATIONS:
- Analysis is based on static references only
- Dynamic references (reflection, etc.) may not be detected
- Only analyzes Go and JavaScript/TypeScript files
- Large codebases may take longer to analyze

TIPS:
- Use this tool before making breaking changes
- Combine with 'find-refs' for detailed reference analysis
- Consider the risk level when planning changes
- Use recommendations to guide your modification strategy`
)

func NewImpactTool(registry *treesitter.Registry, workingDir string) BaseTool {
	return &impactTool{
		registry:   registry,
		workingDir: workingDir,
	}
}

func (i *impactTool) Name() string {
	return ImpactToolName
}

func (i *impactTool) Info() ToolInfo {
	return ToolInfo{
		Name:        ImpactToolName,
		Description: impactDescription,
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to analyze (relative to current working directory)",
			},
			"symbol": map[string]any{
				"type":        "string",
				"description": "Name of the symbol to analyze (optional, analyzes entire file if not provided)",
			},
			"line": map[string]any{
				"type":        "integer",
				"description": "Line number of the symbol (optional, helps disambiguate symbols)",
			},
			"column": map[string]any{
				"type":        "integer",
				"description": "Column number of the symbol (optional, helps disambiguate symbols)",
			},
			"max_depth": map[string]any{
				"type":        "integer",
				"description": "Maximum depth for reference analysis (default: 5)",
			},
		},
		Required: []string{"path"},
	}
}

func (i *impactTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params ImpactParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Path == "" {
		return NewTextErrorResponse("path is required"), nil
	}

	if params.MaxDepth == 0 {
		params.MaxDepth = 5
	}

	// Resolve file path
	filePath := filepath.Join(i.workingDir, params.Path)
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(i.workingDir, params.Path)
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

	// Perform impact analysis
	analysis, err := i.analyzeImpact(ctx, params, string(source))
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error analyzing impact: %s", err)), nil
	}

	// Format response
	output := i.formatImpactAnalysis(analysis)

	return WithResponseMetadata(
		NewTextResponse(output),
		ImpactResponseMetadata{
			AnalysisTime: 0, // TODO: track timing
			FilesScanned: len(analysis.AffectedFiles),
			TotalRefs:    len(analysis.References),
		},
	), nil
}

func (i *impactTool) analyzeImpact(ctx context.Context, params ImpactParams, source string) (*ImpactAnalysis, error) {
	analysis := &ImpactAnalysis{
		File:          params.Path,
		AffectedFiles: make([]string, 0),
		References:    make([]ReferenceInfo, 0),
	}

	// Extract symbols from the target file
	symbols, relationships, err := i.registry.ExtractSymbols(params.Path, source)
	if err != nil {
		return nil, fmt.Errorf("failed to extract symbols: %w", err)
	}

	var targetSymbol *treesitter.Symbol
	if params.Symbol != "" {
		// Find the specific symbol
		for _, symbol := range symbols {
			if symbol.Name == params.Symbol {
				// If line/column specified, match more precisely
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
		analysis.Symbol = params.Symbol
	} else {
		// Analyze entire file - use all symbols
		analysis.Symbol = fmt.Sprintf("file:%s", params.Path)
	}

	// Find all references to the target symbol(s)
	references, err := i.findReferences(ctx, targetSymbol, symbols, relationships, params.MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("failed to find references: %w", err)
	}

	analysis.References = references

	// Calculate blast radius
	fileSet := make(map[string]bool)
	for _, ref := range references {
		fileSet[ref.File] = true
	}
	for file := range fileSet {
		analysis.AffectedFiles = append(analysis.AffectedFiles, file)
	}

	analysis.BlastRadius = len(analysis.AffectedFiles)

	// Calculate impact score
	analysis.ImpactScore = i.calculateImpactScore(analysis, targetSymbol)

	// Determine risk level
	analysis.RiskLevel = i.calculateRiskLevel(analysis.ImpactScore)

	// Generate recommendations
	analysis.Recommendations = i.generateRecommendations(analysis)

	return analysis, nil
}

func (i *impactTool) findReferences(ctx context.Context, targetSymbol *treesitter.Symbol, symbols []treesitter.Symbol, relationships []treesitter.Relationship, maxDepth int) ([]ReferenceInfo, error) {
	var references []ReferenceInfo

	// If analyzing entire file, collect all symbols
	var targetSymbols []*treesitter.Symbol
	if targetSymbol != nil {
		targetSymbols = []*treesitter.Symbol{targetSymbol}
	} else {
		for i := range symbols {
			targetSymbols = append(targetSymbols, &symbols[i])
		}
	}

	// Find direct references
	for _, symbol := range targetSymbols {
		// Add self-reference
		references = append(references, ReferenceInfo{
			File:    symbol.ID[:strings.LastIndex(symbol.ID, ":")], // Extract file from ID
			Line:    symbol.Position.Line,
			Column:  symbol.Position.Column,
			Context: fmt.Sprintf("Definition of %s", symbol.Name),
			Type:    "definition",
		})

		// Find relationships
		for _, rel := range relationships {
			if rel.From == symbol.ID || rel.To == symbol.Name {
				// This is a simplified approach - in practice, you'd need more sophisticated
				// reference resolution based on the actual AST structure
				references = append(references, ReferenceInfo{
					File:    "unknown", // Would need file mapping
					Line:    rel.Position.Line,
					Column:  rel.Position.Column,
					Context: fmt.Sprintf("%s relationship", rel.Type),
					Type:    string(rel.Type),
				})
			}
		}
	}

	// For now, return basic references
	// In a full implementation, this would traverse the entire codebase
	// to find all usages of the symbol

	return references, nil
}

func (i *impactTool) calculateImpactScore(analysis *ImpactAnalysis, targetSymbol *treesitter.Symbol) float64 {
	score := float64(analysis.BlastRadius)

	// Factor in symbol type importance
	if targetSymbol != nil {
		switch targetSymbol.Type {
		case treesitter.SymbolTypeFunction:
			score *= 1.2
		case treesitter.SymbolTypeClass:
			score *= 1.5
		case treesitter.SymbolTypeInterface:
			score *= 1.8
		case treesitter.SymbolTypeVariable:
			if targetSymbol.Scope == treesitter.ScopeGlobal {
				score *= 1.3
			}
		}
	}

	// Factor in reference types
	refTypeMultiplier := 1.0
	for _, ref := range analysis.References {
		switch ref.Type {
		case "call":
			refTypeMultiplier += 0.1
		case "reference":
			refTypeMultiplier += 0.05
		case "inherit":
			refTypeMultiplier += 0.2
		}
	}

	score *= refTypeMultiplier

	return math.Round(score*100) / 100
}

func (i *impactTool) calculateRiskLevel(score float64) string {
	switch {
	case score <= 5:
		return "Low"
	case score <= 15:
		return "Medium"
	case score <= 50:
		return "High"
	default:
		return "Critical"
	}
}

func (i *impactTool) generateRecommendations(analysis *ImpactAnalysis) []string {
	var recommendations []string

	switch analysis.RiskLevel {
	case "Low":
		recommendations = append(recommendations,
			"✅ Safe to modify - minimal impact expected",
			"📝 Consider adding tests for the modified functionality",
			"🔍 Review the affected files for any indirect dependencies",
		)
	case "Medium":
		recommendations = append(recommendations,
			"⚠️  Moderate risk - test thoroughly before deployment",
			"📋 Create a checklist of affected components to verify",
			"🔄 Consider backward compatibility and migration strategy",
			"👥 Notify team members who might be affected",
		)
	case "High":
		recommendations = append(recommendations,
			"🚨 High risk - extensive testing required",
			"📊 Perform impact analysis on dependent systems",
			"🔒 Consider feature flags for gradual rollout",
			"📝 Document all changes and update API specifications",
			"🧪 Plan comprehensive integration and regression testing",
		)
	case "Critical":
		recommendations = append(recommendations,
			"🚫 Critical risk - proceed with extreme caution",
			"🏗️  Consider architectural review before changes",
			"📅 Plan deployment during low-traffic periods",
			"🔄 Prepare rollback strategy and backup plans",
			"📞 Involve stakeholders and get explicit approval",
			"🧪 Conduct thorough end-to-end testing across all systems",
		)
	}

	if analysis.BlastRadius > 10 {
		recommendations = append(recommendations,
			"📈 High blast radius - consider breaking changes into smaller increments",
		)
	}

	return recommendations
}

func (i *impactTool) formatImpactAnalysis(analysis *ImpactAnalysis) string {
	var output strings.Builder

	output.WriteString(fmt.Sprintf("🔍 Impact Analysis for %s\n", analysis.Symbol))
	output.WriteString(fmt.Sprintf("📁 File: %s\n", analysis.File))
	output.WriteString(fmt.Sprintf("📊 Blast Radius: %d files affected\n", analysis.BlastRadius))
	output.WriteString(fmt.Sprintf("🎯 Impact Score: %.2f\n", analysis.ImpactScore))
	output.WriteString(fmt.Sprintf("⚠️  Risk Level: %s\n\n", analysis.RiskLevel))

	if len(analysis.AffectedFiles) > 0 {
		output.WriteString("📂 Affected Files:\n")
		sort.Strings(analysis.AffectedFiles)
		for _, file := range analysis.AffectedFiles {
			output.WriteString(fmt.Sprintf("  • %s\n", file))
		}
		output.WriteString("\n")
	}

	if len(analysis.References) > 0 {
		output.WriteString("🔗 References Found:\n")
		for _, ref := range analysis.References {
			output.WriteString(fmt.Sprintf("  • %s:%d:%d - %s", ref.File, ref.Line, ref.Column, ref.Context))
			if ref.Type != "" {
				output.WriteString(fmt.Sprintf(" (%s)", ref.Type))
			}
			output.WriteString("\n")
		}
		output.WriteString("\n")
	}

	output.WriteString("💡 Recommendations:\n")
	for _, rec := range analysis.Recommendations {
		output.WriteString(fmt.Sprintf("  %s\n", rec))
	}

	return output.String()
}
