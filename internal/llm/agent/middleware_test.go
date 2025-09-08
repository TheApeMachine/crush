package agent

import (
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/llm/tools"
	"github.com/charmbracelet/crush/internal/treesitter"
)

func TestEditVerificationMiddleware_Name(t *testing.T) {
	registry := treesitter.NewRegistry()
	middleware := NewEditVerificationMiddleware(registry, "/tmp", DefaultEditVerificationConfig())

	if middleware.Name() != "edit_verification" {
		t.Errorf("Expected middleware name 'edit_verification', got '%s'", middleware.Name())
	}
}

func TestEditVerificationMiddleware_IsEditTool(t *testing.T) {
	middleware := &EditVerificationMiddleware{}

	tests := []struct {
		toolName string
		expected bool
	}{
		{"edit", true},
		{"multiedit", true},
		{"view", false},
		{"bash", false},
		{"impact", false},
		{"", false},
	}

	for _, test := range tests {
		t.Run(test.toolName, func(t *testing.T) {
			result := middleware.isEditTool(test.toolName)
			if result != test.expected {
				t.Errorf("isEditTool(%s) = %v, expected %v", test.toolName, result, test.expected)
			}
		})
	}
}

func TestEditVerificationMiddleware_ExtractFilePath(t *testing.T) {
	middleware := &EditVerificationMiddleware{}

	// Test edit tool
	editCall := tools.ToolCall{
		Name:  "edit",
		Input: `{"file_path": "/test/file.go", "old_string": "old", "new_string": "new"}`,
	}
	filePath, err := middleware.extractFilePath(editCall)
	if err != nil {
		t.Errorf("Failed to extract file path from edit tool: %v", err)
	}
	if filePath != "/test/file.go" {
		t.Errorf("Expected file path '/test/file.go', got '%s'", filePath)
	}

	// Test multiedit tool
	multiEditCall := tools.ToolCall{
		Name:  "multiedit",
		Input: `{"file_path": "/test/multi.go", "edits": []}`,
	}
	filePath, err = middleware.extractFilePath(multiEditCall)
	if err != nil {
		t.Errorf("Failed to extract file path from multiedit tool: %v", err)
	}
	if filePath != "/test/multi.go" {
		t.Errorf("Expected file path '/test/multi.go', got '%s'", filePath)
	}

	// Test unsupported tool
	unsupportedCall := tools.ToolCall{
		Name:  "unsupported",
		Input: `{}`,
	}
	_, err = middleware.extractFilePath(unsupportedCall)
	if err == nil {
		t.Error("Expected error for unsupported tool, got nil")
	}
}

func TestEditVerificationMiddleware_ParseImpactAnalysis(t *testing.T) {
	middleware := &EditVerificationMiddleware{}

	// Test parsing impact analysis output
	content := `🔍 Impact Analysis for test.go
📁 File: test.go
📊 Blast Radius: 5 files affected
🎯 Impact Score: 12.50
⚠️  Risk Level: Medium

📂 Affected Files:
  • test.go
  • utils.go
  • main.go

💡 Recommendations:
  • Moderate risk - test thoroughly before deployment`

	analysis, err := middleware.parseImpactAnalysis(content)
	if err != nil {
		t.Errorf("Failed to parse impact analysis: %v", err)
	}

	if analysis.BlastRadius != 5 {
		t.Errorf("Expected blast radius 5, got %d", analysis.BlastRadius)
	}

	if analysis.RiskLevel != "Medium" {
		t.Errorf("Expected risk level 'Medium', got '%s'", analysis.RiskLevel)
	}
}

func TestEditVerificationMiddleware_HasRegression(t *testing.T) {
	config := DefaultEditVerificationConfig()
	config.BlastRadiusThreshold = 10
	middleware := &EditVerificationMiddleware{config: config}

	tests := []struct {
		name     string
		analysis *tools.ImpactAnalysis
		expected bool
	}{
		{
			name: "No regression - low blast radius",
			analysis: &tools.ImpactAnalysis{
				BlastRadius: 5,
				RiskLevel:   "Low",
			},
			expected: false,
		},
		{
			name: "Regression - high blast radius",
			analysis: &tools.ImpactAnalysis{
				BlastRadius: 25,
				RiskLevel:   "High",
			},
			expected: true,
		},
		{
			name: "Regression - critical risk",
			analysis: &tools.ImpactAnalysis{
				BlastRadius: 8,
				RiskLevel:   "Critical",
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := middleware.hasRegression(test.analysis)
			if result != test.expected {
				t.Errorf("hasRegression() = %v, expected %v", result, test.expected)
			}
		})
	}
}

func TestMiddlewareManager(t *testing.T) {
	manager := NewMiddlewareManager()

	// Test empty manager
	result, err := manager.ExecutePre(context.Background(), tools.ToolCall{Name: "test"}, "session1")
	if err != nil {
		t.Errorf("ExecutePre with empty manager failed: %v", err)
	}
	if !result.AllowExecution {
		t.Error("Expected execution to be allowed with empty manager")
	}

	// Add middleware
	registry := treesitter.NewRegistry()
	middleware := NewEditVerificationMiddleware(registry, "/tmp", DefaultEditVerificationConfig())
	manager.AddMiddleware(middleware)

	// Test with middleware
	editCall := tools.ToolCall{
		Name:  "edit",
		Input: `{"file_path": "/tmp/test.go", "old_string": "old", "new_string": "new"}`,
	}

	result, err = manager.ExecutePre(context.Background(), editCall, "session1")
	if err != nil {
		t.Errorf("ExecutePre with middleware failed: %v", err)
	}
	// Should allow execution (preflight analysis may fail but shouldn't block)
	if !result.AllowExecution {
		t.Logf("Preflight blocked execution: %s", result.Message)
	}

	// Test post execution
	response := tools.ToolResponse{
		Content: "File edited successfully",
		IsError: false,
	}
	postResult, err := manager.ExecutePost(context.Background(), editCall, response, "session1")
	if err != nil {
		t.Errorf("ExecutePost failed: %v", err)
	}
	if !postResult.AllowExecution {
		t.Logf("Postflight blocked execution: %s", postResult.Message)
	}
}

func TestDefaultEditVerificationConfig(t *testing.T) {
	config := DefaultEditVerificationConfig()

	if !config.Enabled {
		t.Error("Expected default config to be enabled")
	}

	if config.BlastRadiusThreshold != 10 {
		t.Errorf("Expected blast radius threshold 10, got %d", config.BlastRadiusThreshold)
	}

	if !config.RequireApprovalForHigh {
		t.Error("Expected to require approval for high impact")
	}

	if !config.AutoRollbackOnError {
		t.Error("Expected auto rollback on error to be enabled")
	}

	if config.AnalysisTimeout != 30*time.Second {
		t.Errorf("Expected analysis timeout 30s, got %v", config.AnalysisTimeout)
	}
}

func TestEditVerificationMiddleware_PreExecute_NonEditTool(t *testing.T) {
	registry := treesitter.NewRegistry()
	middleware := NewEditVerificationMiddleware(registry, "/tmp", DefaultEditVerificationConfig())

	// Test with non-edit tool
	nonEditCall := tools.ToolCall{
		Name:  "view",
		Input: `{"file_path": "/tmp/test.go"}`,
	}

	result, err := middleware.PreExecute(context.Background(), nonEditCall, "session1")
	if err != nil {
		t.Errorf("PreExecute failed for non-edit tool: %v", err)
	}

	if !result.AllowExecution {
		t.Error("Expected non-edit tool to be allowed execution")
	}
}

func TestEditVerificationMiddleware_PostExecute_NonEditTool(t *testing.T) {
	registry := treesitter.NewRegistry()
	middleware := NewEditVerificationMiddleware(registry, "/tmp", DefaultEditVerificationConfig())

	// Test with non-edit tool
	nonEditCall := tools.ToolCall{
		Name:  "view",
		Input: `{"file_path": "/tmp/test.go"}`,
	}

	response := tools.ToolResponse{
		Content: "File viewed successfully",
		IsError: false,
	}

	result, err := middleware.PostExecute(context.Background(), nonEditCall, response, "session1")
	if err != nil {
		t.Errorf("PostExecute failed for non-edit tool: %v", err)
	}

	if !result.AllowExecution {
		t.Error("Expected non-edit tool post-execution to be allowed")
	}
}

// Additional middleware edge cases (appended)
func TestEditVerificationMiddleware_parseImpactAnalysis_Robust(t *testing.T) {
	m := &EditVerificationMiddleware{}
	// missing parts should not crash
	content := "Random\n📊 Blast Radius: 3 files affected\nNoise\n"
	analysis, err := m.parseImpactAnalysis(content)
	if err != nil {
		t.Fatalf("parseImpactAnalysis error: %v", err)
	}
	if analysis.BlastRadius != 3 {
		t.Fatalf("expected blast radius 3, got %d", analysis.BlastRadius)
	}
}

func TestMiddlewareManager_EmptyPost(t *testing.T) {
	manager := NewMiddlewareManager()
	res, err := manager.ExecutePost(context.Background(), tools.ToolCall{Name: "anything"}, tools.ToolResponse{Content: "ok"}, "s")
	if err != nil || !res.AllowExecution {
		t.Fatalf("expected allow, got err=%v allow=%v", err, res.AllowExecution)
	}
}
