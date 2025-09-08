package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/llm/tools"
	"github.com/charmbracelet/crush/internal/treesitter"
)

// Middleware represents a middleware that can intercept and modify tool calls
type Middleware interface {
	Name() string
	PreExecute(ctx context.Context, toolCall tools.ToolCall, sessionID string) (*MiddlewareResult, error)
	PostExecute(ctx context.Context, toolCall tools.ToolCall, result tools.ToolResponse, sessionID string) (*MiddlewareResult, error)
}

// MiddlewareResult represents the result of middleware execution
type MiddlewareResult struct {
	AllowExecution bool
	Response       *tools.ToolResponse
	Error          error
	Message        string
}

// EditVerificationMiddleware provides preflight and postflight verification for edit operations
type EditVerificationMiddleware struct {
	registry   *treesitter.Registry
	workingDir string
	config     EditVerificationConfig
	impactTool tools.BaseTool
}

// EditVerificationConfig configures the verification middleware behavior
type EditVerificationConfig struct {
	Enabled                bool          `json:"enabled"`
	BlastRadiusThreshold   int           `json:"blast_radius_threshold"`
	RequireApprovalForHigh bool          `json:"require_approval_for_high"`
	AutoRollbackOnError    bool          `json:"auto_rollback_on_error"`
	AnalysisTimeout        time.Duration `json:"analysis_timeout"`
}

// DefaultEditVerificationConfig returns default configuration
func DefaultEditVerificationConfig() EditVerificationConfig {
	return EditVerificationConfig{
		Enabled:                true,
		BlastRadiusThreshold:   10,
		RequireApprovalForHigh: true,
		AutoRollbackOnError:    true,
		AnalysisTimeout:        30 * time.Second,
	}
}

// NewEditVerificationMiddleware creates a new edit verification middleware
func NewEditVerificationMiddleware(registry *treesitter.Registry, workingDir string, cfg EditVerificationConfig) Middleware {
	impactTool := tools.NewImpactTool(registry, workingDir)
	return &EditVerificationMiddleware{
		registry:   registry,
		workingDir: workingDir,
		config:     cfg,
		impactTool: impactTool,
	}
}

func (m *EditVerificationMiddleware) Name() string {
	return "edit_verification"
}

// PreExecute runs preflight verification before edit operations
func (m *EditVerificationMiddleware) PreExecute(ctx context.Context, toolCall tools.ToolCall, sessionID string) (*MiddlewareResult, error) {
	// Only intercept edit-related tools
	if !m.isEditTool(toolCall.Name) {
		return &MiddlewareResult{AllowExecution: true}, nil
	}

	slog.Info("Running preflight verification", "tool", toolCall.Name, "session", sessionID)

	// Extract file path from tool call
	filePath, err := m.extractFilePath(toolCall)
	if err != nil {
		return &MiddlewareResult{
			AllowExecution: false,
			Error:          fmt.Errorf("failed to extract file path: %w", err),
		}, nil
	}

	// Run impact analysis
	analysisCtx, cancel := context.WithTimeout(ctx, m.config.AnalysisTimeout)
	defer cancel()

	impactCall := tools.ToolCall{
		ID:    toolCall.ID + "-preflight",
		Name:  "impact",
		Input: fmt.Sprintf(`{"path": "%s", "max_depth": 3}`, filePath),
	}

	impactResponse, err := m.impactTool.Run(analysisCtx, impactCall)
	if err != nil {
		slog.Warn("Preflight impact analysis failed", "error", err)
		// Allow execution but log the failure
		return &MiddlewareResult{
			AllowExecution: true,
			Message:        "Preflight analysis failed, proceeding with caution",
		}, nil
	}

	// Parse impact analysis result
	analysis, err := m.parseImpactAnalysis(impactResponse.Content)
	if err != nil {
		slog.Warn("Failed to parse impact analysis", "error", err)
		return &MiddlewareResult{
			AllowExecution: true,
			Message:        "Could not parse impact analysis, proceeding",
		}, nil
	}

	// Check blast radius threshold
	if analysis.BlastRadius > m.config.BlastRadiusThreshold {
		if m.config.RequireApprovalForHigh {
			recommendation := "High impact edit detected - proceed with caution"
			if len(analysis.Recommendations) > 0 {
				recommendation = analysis.Recommendations[0]
			}
			return &MiddlewareResult{
				AllowExecution: false,
				Response: &tools.ToolResponse{
					Content: fmt.Sprintf("🚨 High impact edit detected!\n\n%s\n\nThis edit affects %d files with risk level: %s\n\nPlease confirm you want to proceed with this change.",
						recommendation, analysis.BlastRadius, analysis.RiskLevel),
					IsError: false,
				},
			}, nil
		}
		slog.Warn("High impact edit proceeding without approval",
			"blast_radius", analysis.BlastRadius,
			"risk_level", analysis.RiskLevel)
	}

	return &MiddlewareResult{
		AllowExecution: true,
		Message:        fmt.Sprintf("Preflight check passed. Blast radius: %d, Risk: %s", analysis.BlastRadius, analysis.RiskLevel),
	}, nil
}

// PostExecute runs postflight verification after edit operations
func (m *EditVerificationMiddleware) PostExecute(ctx context.Context, toolCall tools.ToolCall, result tools.ToolResponse, sessionID string) (*MiddlewareResult, error) {
	// Only process edit-related tools
	if !m.isEditTool(toolCall.Name) {
		return &MiddlewareResult{AllowExecution: true}, nil
	}

	slog.Info("Running postflight verification", "tool", toolCall.Name, "session", sessionID)

	// Extract file path from tool call
	filePath, err := m.extractFilePath(toolCall)
	if err != nil {
		slog.Warn("Failed to extract file path for postflight", "error", err)
		return &MiddlewareResult{AllowExecution: true}, nil
	}

	// Run post-edit impact analysis
	analysisCtx, cancel := context.WithTimeout(ctx, m.config.AnalysisTimeout)
	defer cancel()

	impactCall := tools.ToolCall{
		ID:    toolCall.ID + "-postflight",
		Name:  "impact",
		Input: fmt.Sprintf(`{"path": "%s", "max_depth": 3}`, filePath),
	}

	postImpactResponse, err := m.impactTool.Run(analysisCtx, impactCall)
	if err != nil {
		slog.Warn("Postflight impact analysis failed", "error", err)
		return &MiddlewareResult{AllowExecution: true}, nil
	}

	// Parse post-edit analysis
	postAnalysis, err := m.parseImpactAnalysis(postImpactResponse.Content)
	if err != nil {
		slog.Warn("Failed to parse postflight impact analysis", "error", err)
		return &MiddlewareResult{AllowExecution: true}, nil
	}

	// Check for new unresolved references or regressions
	if m.hasRegression(postAnalysis) {
		if m.config.AutoRollbackOnError {
			return &MiddlewareResult{
				AllowExecution: false,
				Response: &tools.ToolResponse{
					Content: "🚨 Postflight verification detected potential regression. Auto-rollback recommended.\n\n" +
						"New unresolved references or structural issues detected after the edit.",
					IsError: true,
				},
			}, nil
		}
		slog.Warn("Postflight detected potential regression but auto-rollback disabled")
	}

	return &MiddlewareResult{
		AllowExecution: true,
		Message:        "Postflight verification passed",
	}, nil
}

// Helper methods

func (m *EditVerificationMiddleware) isEditTool(toolName string) bool {
	return toolName == "edit" || toolName == "multiedit"
}

func (m *EditVerificationMiddleware) extractFilePath(toolCall tools.ToolCall) (string, error) {
	switch toolCall.Name {
	case "edit":
		var params tools.EditParams
		if err := json.Unmarshal([]byte(toolCall.Input), &params); err != nil {
			return "", err
		}
		return params.FilePath, nil
	case "multiedit":
		var params tools.MultiEditParams
		if err := json.Unmarshal([]byte(toolCall.Input), &params); err != nil {
			return "", err
		}
		return params.FilePath, nil
	default:
		return "", fmt.Errorf("unsupported tool: %s", toolCall.Name)
	}
}

func (m *EditVerificationMiddleware) parseImpactAnalysis(content string) (*tools.ImpactAnalysis, error) {
	// Simple parsing of impact analysis output
	// This is a basic implementation - could be enhanced with better parsing
	analysis := &tools.ImpactAnalysis{}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Blast Radius:") {
			// Extract blast radius number - handle different formats
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				radiusPart := strings.TrimSpace(parts[1])
				// Extract number from "5 files affected" format
				if idx := strings.Index(radiusPart, " "); idx > 0 {
					if blastRadius, err := strconv.Atoi(radiusPart[:idx]); err == nil {
						analysis.BlastRadius = blastRadius
					}
				}
			}
		}
		if strings.Contains(line, "Risk Level:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				analysis.RiskLevel = strings.TrimSpace(parts[1])
			}
		}
	}

	return analysis, nil
}

func (m *EditVerificationMiddleware) hasRegression(analysis *tools.ImpactAnalysis) bool {
	// Check for indicators of regression
	// This is a simplified check - could be enhanced with more sophisticated analysis
	return analysis.BlastRadius > m.config.BlastRadiusThreshold*2 || analysis.RiskLevel == "Critical"
}

// MiddlewareManager manages a chain of middleware
type MiddlewareManager struct {
	middleware []Middleware
}

// NewMiddlewareManager creates a new middleware manager
func NewMiddlewareManager() *MiddlewareManager {
	return &MiddlewareManager{
		middleware: make([]Middleware, 0),
	}
}

// AddMiddleware adds a middleware to the chain
func (mm *MiddlewareManager) AddMiddleware(m Middleware) {
	mm.middleware = append(mm.middleware, m)
}

// ExecutePre runs all pre-execute middleware
func (mm *MiddlewareManager) ExecutePre(ctx context.Context, toolCall tools.ToolCall, sessionID string) (*MiddlewareResult, error) {
	for _, m := range mm.middleware {
		result, err := m.PreExecute(ctx, toolCall, sessionID)
		if err != nil {
			return nil, err
		}
		if !result.AllowExecution {
			return result, nil
		}
	}
	return &MiddlewareResult{AllowExecution: true}, nil
}

// ExecutePost runs all post-execute middleware
func (mm *MiddlewareManager) ExecutePost(ctx context.Context, toolCall tools.ToolCall, result tools.ToolResponse, sessionID string) (*MiddlewareResult, error) {
	for _, m := range mm.middleware {
		postResult, err := m.PostExecute(ctx, toolCall, result, sessionID)
		if err != nil {
			return nil, err
		}
		if !postResult.AllowExecution {
			return postResult, nil
		}
	}
	return &MiddlewareResult{AllowExecution: true}, nil
}
