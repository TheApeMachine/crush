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

func TestGoToDefTool(t *testing.T) {
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
	result := processUser(user)
	fmt.Println(result)
}

func processUser(u User) string {
	return fmt.Sprintf("User: %s", u.Name)
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
	tool := NewGoToDefTool(registry, tempDir)

	// Test tool info
	info := tool.Info()
	if info.Name != GoToDefToolName {
		t.Errorf("Expected tool name %s, got %s", GoToDefToolName, info.Name)
	}

	// Test successful execution with function
	params := GoToDefParams{
		Path:   "test.go",
		Line:   13, // Line with "result := processUser(user)"
		Column: 18, // Position of "processUser"
	}
	paramsJSON, _ := json.Marshal(params)

	call := ToolCall{
		ID:    "test-call",
		Name:  GoToDefToolName,
		Input: string(paramsJSON),
	}

	response, err := tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if response.IsError {
		t.Errorf("Tool returned error: %s", response.Content)
	}

	// Check that response contains expected definition information
	content := response.Content
	if !strings.Contains(content, "processUser") {
		t.Errorf("Expected function name not found in response")
	}
	if !strings.Contains(content, "Found definition") {
		t.Errorf("Expected definition header not found")
	}
	if !strings.Contains(content, "Context") {
		t.Errorf("Expected context section not found")
	}

	// Test with type definition
	params = GoToDefParams{
		Path:   "test.go",
		Line:   11, // Line with "user := User{...}"
		Column: 10, // Position of "User"
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

	// Should find the User type definition
	if !strings.Contains(response.Content, "type User") {
		t.Errorf("Expected User type definition not found")
	}

	// Test with variable
	params = GoToDefParams{
		Path:   "test.go",
		Line:   25, // Line with "var globalVar = "test""
		Column: 5,  // Position of "globalVar"
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

	// Should find the globalVar definition
	if !strings.Contains(response.Content, "globalVar") {
		t.Errorf("Expected globalVar definition not found")
	}

	// Test with function call in different context
	params = GoToDefParams{
		Path:   "test.go",
		Line:   14, // Line with "fmt.Println(result)"
		Column: 2,  // Position of "fmt"
	}
	paramsJSON, _ = json.Marshal(params)

	call.Input = string(paramsJSON)
	response, err = tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	// This might not find a definition (since fmt is imported), but shouldn't error
	if response.IsError {
		t.Errorf("Unexpected error for imported symbol: %s", response.Content)
	}

	// Test error cases
	t.Run("MissingPath", func(t *testing.T) {
		params := GoToDefParams{
			Line:   1,
			Column: 1,
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  GoToDefToolName,
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

	t.Run("InvalidPosition", func(t *testing.T) {
		params := GoToDefParams{
			Path:   "test.go",
			Line:   0,
			Column: 1,
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  GoToDefToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		if err != nil {
			t.Fatalf("Tool execution failed: %v", err)
		}

		if !response.IsError || !strings.Contains(response.Content, "must be positive") {
			t.Errorf("Expected error for invalid position")
		}
	})

	t.Run("FileNotFound", func(t *testing.T) {
		params := GoToDefParams{
			Path:   "nonexistent.go",
			Line:   1,
			Column: 1,
		}
		paramsJSON, _ = json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  GoToDefToolName,
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

	t.Run("NoSymbolAtPosition", func(t *testing.T) {
		params := GoToDefParams{
			Path:   "test.go",
			Line:   2, // Empty line
			Column: 1,
		}
		paramsJSON, _ = json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  GoToDefToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		if err != nil {
			t.Fatalf("Tool execution failed: %v", err)
		}

		// Should handle gracefully - either find closest symbol or indicate no symbol found
		if response.IsError && !strings.Contains(response.Content, "not found") {
			t.Errorf("Unexpected error for empty position: %s", response.Content)
		}
	})
}

func TestSignatureCreation(t *testing.T) {
	tool := &goToDefTool{}

	// Test function signature
	symbol := treesitter.Symbol{
		Name: "processUser",
		Type: treesitter.SymbolTypeFunction,
		Parameters: []treesitter.Parameter{
			{Name: "u", Type: "User"},
		},
		ReturnType: "string",
	}

	signature := tool.createSymbolSignature(symbol)
	expected := "func processUser(u User) string"
	if signature != expected {
		t.Errorf("Expected signature '%s', got '%s'", expected, signature)
	}

	// Test type signature
	symbol = treesitter.Symbol{
		Name: "User",
		Type: treesitter.SymbolTypeType,
	}

	signature = tool.createSymbolSignature(symbol)
	expected = "type User"
	if signature != expected {
		t.Errorf("Expected signature '%s', got '%s'", expected, signature)
	}

	// Test variable signature
	symbol = treesitter.Symbol{
		Name:       "globalVar",
		Type:       treesitter.SymbolTypeVariable,
		ReturnType: "string",
	}

	signature = tool.createSymbolSignature(symbol)
	expected = "var globalVar string"
	if signature != expected {
		t.Errorf("Expected signature '%s', got '%s'", expected, signature)
	}
}
