package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/treesitter"
)

func TestImpactTool(t *testing.T) {
	// Create a temporary test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	testCode := `package main

import "fmt"

type User struct {
	Name string
	Age  int
}

func main() {
	user := User{Name: "John", Age: 30}
	fmt.Println(user)
	printUser(user)
}

func printUser(u User) {
	fmt.Printf("User: %s, Age: %d\n", u.Name, u.Age)
}

func helper() string {
	return "helper"
}

var globalVar = "test"
`

	err := os.WriteFile(testFile, []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create registry and register parsers
	registry := treesitter.NewRegistry()
	err = registry.RegisterParser(treesitter.LanguageGo)
	if err != nil {
		t.Fatalf("Failed to register Go parser: %v", err)
	}

	// Create tool
	tool := NewImpactTool(registry, tempDir)

	// Test tool info
	info := tool.Info()
	if info.Name != ImpactToolName {
		t.Errorf("Expected tool name %s, got %s", ImpactToolName, info.Name)
	}

	// Test successful execution with symbol
	params := ImpactParams{
		Path:   "test.go",
		Symbol: "printUser",
	}
	paramsJSON, _ := json.Marshal(params)

	call := ToolCall{
		ID:    "test-call",
		Name:  ImpactToolName,
		Input: string(paramsJSON),
	}

	response, err := tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if response.IsError {
		t.Errorf("Tool returned error: %s", response.Content)
	}

	// Check that response contains expected impact information
	content := response.Content
	if !strings.Contains(content, "printUser") {
		t.Errorf("Expected symbol name not found in response")
	}
	if !strings.Contains(content, "Blast Radius") {
		t.Errorf("Expected blast radius information not found")
	}
	if !strings.Contains(content, "Impact Score") {
		t.Errorf("Expected impact score not found")
	}
	if !strings.Contains(content, "Risk Level") {
		t.Errorf("Expected risk level not found")
	}

	// Test file-level impact analysis
	params = ImpactParams{
		Path: "test.go",
	}
	paramsJSON, _ = json.Marshal(params)

	call.Input = string(paramsJSON)
	response, err = tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if response.IsError {
		t.Errorf("Tool returned error: %s", response.Content)
	}

	// Should analyze entire file
	if !strings.Contains(response.Content, "file:test.go") {
		t.Errorf("Expected file-level analysis not found")
	}

	// Test with max depth
	params = ImpactParams{
		Path:     "test.go",
		Symbol:   "main",
		MaxDepth: 2,
	}
	paramsJSON, _ = json.Marshal(params)

	call.Input = string(paramsJSON)
	response, err = tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if response.IsError {
		t.Errorf("Tool returned error: %s", response.Content)
	}

	// Test error cases
	t.Run("MissingPath", func(t *testing.T) {
		params := ImpactParams{
			Symbol: "test",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  ImpactToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		if err != nil {
			t.Fatalf("Tool execution failed: %v", err)
		}

		if !response.IsError || !strings.Contains(response.Content, "path is required") {
			t.Errorf("Expected error for missing path")
		}
	})

	t.Run("SymbolNotFound", func(t *testing.T) {
		params := ImpactParams{
			Path:   "test.go",
			Symbol: "nonexistent",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  ImpactToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		if err != nil {
			t.Fatalf("Tool execution failed: %v", err)
		}

		if !response.IsError || !strings.Contains(response.Content, "not found") {
			t.Errorf("Expected error for nonexistent symbol")
		}
	})

	t.Run("FileNotFound", func(t *testing.T) {
		params := ImpactParams{
			Path:   "nonexistent.go",
			Symbol: "test",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  ImpactToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		if err != nil {
			t.Fatalf("Tool execution failed: %v", err)
		}

		if !response.IsError || !strings.Contains(response.Content, "does not exist") {
			t.Errorf("Expected error for nonexistent file")
		}
	})
}

func TestImpactAnalysis(t *testing.T) {
	// Test impact score calculation
	tool := &impactTool{}

	// Test low impact
	analysis := &ImpactAnalysis{
		BlastRadius: 2,
		References: []ReferenceInfo{
			{Type: "call"},
		},
	}
	score := tool.calculateImpactScore(analysis, nil)
	if score >= 5 {
		t.Errorf("Expected low impact score, got %.2f", score)
	}

	risk := tool.calculateRiskLevel(score)
	if risk != "Low" {
		t.Errorf("Expected Low risk level, got %s", risk)
	}

	// Test high impact
	analysis = &ImpactAnalysis{
		BlastRadius: 20,
		References: []ReferenceInfo{
			{Type: "call"},
			{Type: "call"},
			{Type: "call"},
		},
	}
	score = tool.calculateImpactScore(analysis, nil)
	if score < 15 {
		t.Errorf("Expected higher impact score, got %.2f", score)
	}

	risk = tool.calculateRiskLevel(score)
	if risk == "Low" {
		t.Errorf("Expected higher risk level than Low, got %s", risk)
	}
}
