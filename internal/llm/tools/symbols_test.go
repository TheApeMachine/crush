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

func TestSymbolsTool(t *testing.T) {
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
}

func helper() string {
	return "helper"
}

var globalVar = "test"

const PI = 3.14
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
	tool := NewSymbolsTool(registry, tempDir)

	// Test tool info
	info := tool.Info()
	if info.Name != SymbolsToolName {
		t.Errorf("Expected tool name %s, got %s", SymbolsToolName, info.Name)
	}

	// Test successful execution
	params := SymbolsParams{
		Path: "test.go",
	}
	paramsJSON, _ := json.Marshal(params)

	call := ToolCall{
		ID:    "test-call",
		Name:  SymbolsToolName,
		Input: string(paramsJSON),
	}

	response, err := tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if response.IsError {
		t.Errorf("Tool returned error: %s", response.Content)
	}

	// Check that response contains expected symbols
	content := response.Content
	expectedSymbols := []string{"main", "helper", "User", "globalVar", "PI"}
	for _, symbol := range expectedSymbols {
		if !strings.Contains(content, symbol) {
			t.Errorf("Expected symbol %s not found in response", symbol)
		}
	}

	// Test with symbol type filter
	params = SymbolsParams{
		Path:       "test.go",
		SymbolType: "function",
	}
	paramsJSON, _ = json.Marshal(params)

	call.Input = string(paramsJSON)
	response, err = tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	// Should contain functions but not variables
	if !strings.Contains(response.Content, "main") || !strings.Contains(response.Content, "helper") {
		t.Errorf("Expected functions not found in filtered response")
	}
	if strings.Contains(response.Content, "globalVar") {
		t.Errorf("Variable found in function-only response")
	}

	// Test with include_deps
	params = SymbolsParams{
		Path:        "test.go",
		IncludeDeps: true,
	}
	paramsJSON, _ = json.Marshal(params)

	call.Input = string(paramsJSON)
	response, err = tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	// Should contain imports
	if !strings.Contains(response.Content, "fmt") {
		t.Errorf("Expected import not found in response with deps")
	}

	// Test error cases
	t.Run("MissingPath", func(t *testing.T) {
		params := SymbolsParams{}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  SymbolsToolName,
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

	t.Run("FileNotFound", func(t *testing.T) {
		params := SymbolsParams{
			Path: "nonexistent.go",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  SymbolsToolName,
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

