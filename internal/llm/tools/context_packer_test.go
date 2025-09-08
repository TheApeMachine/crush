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

func TestContextPackerTool(t *testing.T) {
	// Create a temporary test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	testCode := `package main

import (
	"fmt"
	"strings"
)

type User struct {
	Name string
	Age  int
}

func main() {
	user := User{Name: "John", Age: 30}
	fmt.Println(user)
	result := processUser(user)
	fmt.Println(result)
}

func processUser(u User) string {
	return strings.ToUpper(u.Name)
}

func helper() string {
	return "helper function"
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
	tool := NewContextPackerTool(registry, tempDir)

	// Test tool info
	info := tool.Info()
	if info.Name != ContextPackerToolName {
		t.Errorf("Expected tool name %s, got %s", ContextPackerToolName, info.Name)
	}

	// Test successful execution with symbol
	params := ContextPackerParams{
		Path:   "test.go",
		Symbol: "processUser",
	}
	paramsJSON, _ := json.Marshal(params)

	call := ToolCall{
		ID:    "test-call",
		Name:  ContextPackerToolName,
		Input: string(paramsJSON),
	}

	response, err := tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if response.IsError {
		t.Errorf("Tool returned error: %s", response.Content)
	}

	// Check that response contains expected context information
	content := response.Content
	if !strings.Contains(content, "processUser") {
		t.Errorf("Expected symbol name not found in response")
	}
	if !strings.Contains(content, "Context Bundle") {
		t.Errorf("Expected context bundle header not found")
	}
	if !strings.Contains(content, "Symbol AST") {
		t.Errorf("Expected symbol AST section not found")
	}
	if !strings.Contains(content, "Context Code") {
		t.Errorf("Expected context code section not found")
	}

	// Test file-level context
	params = ContextPackerParams{
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

	// Should contain imports
	if !strings.Contains(response.Content, "fmt") || !strings.Contains(response.Content, "strings") {
		t.Errorf("Expected imports not found in file-level context")
	}

	// Test with include_deps
	params = ContextPackerParams{
		Path:        "test.go",
		Symbol:      "main",
		IncludeDeps: true,
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

	// Should contain related symbols
	if !strings.Contains(response.Content, "Related Symbols") {
		t.Errorf("Expected related symbols section not found")
	}

	// Test with custom context lines
	params = ContextPackerParams{
		Path:         "test.go",
		Symbol:       "main",
		ContextLines: 1,
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

	// Test with max size limit
	params = ContextPackerParams{
		Path:    "test.go",
		Symbol:  "main",
		MaxSize: 200,
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

	// Response should be truncated or indicate compression
	if len(response.Content) > 600 { // Allow more buffer
		t.Errorf("Response size %d exceeds expected limit", len(response.Content))
	}

	// Test error cases
	t.Run("MissingPath", func(t *testing.T) {
		params := ContextPackerParams{
			Symbol: "test",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  ContextPackerToolName,
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
		params := ContextPackerParams{
			Path: "nonexistent.go",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  ContextPackerToolName,
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
