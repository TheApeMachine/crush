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

func TestFindRefsTool(t *testing.T) {
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
	updateUser(&user)
}

func printUser(u User) {
	fmt.Printf("User: %s\n", u.Name)
}

func updateUser(u *User) {
	u.Age = 31
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
	tool := NewFindRefsTool(registry, tempDir)

	// Test tool info
	info := tool.Info()
	if info.Name != FindRefsToolName {
		t.Errorf("Expected tool name %s, got %s", FindRefsToolName, info.Name)
	}

	// Test successful execution with symbol
	params := FindRefsParams{
		Path:   "test.go",
		Symbol: "printUser",
	}
	paramsJSON, _ := json.Marshal(params)

	call := ToolCall{
		ID:    "test-call",
		Name:  FindRefsToolName,
		Input: string(paramsJSON),
	}

	response, err := tool.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if response.IsError {
		t.Errorf("Tool returned error: %s", response.Content)
	}

	// Check that response contains expected reference information
	content := response.Content
	if !strings.Contains(content, "printUser") {
		t.Errorf("Expected symbol name not found in response")
	}
	if !strings.Contains(content, "Found") && !strings.Contains(content, "references") {
		t.Errorf("Expected reference count not found")
	}

	// Test with include_file
	params = FindRefsParams{
		Path:        "test.go",
		Symbol:      "User",
		IncludeFile: true,
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

	// Should include the type definition
	if !strings.Contains(response.Content, "Definition of type User") {
		t.Errorf("Expected type definition not found when include_file=true")
	}

	// Test with position-based lookup
	params = FindRefsParams{
		Path:   "test.go",
		Line:   5, // Line with "type User struct"
		Column: 6, // Position of "User"
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

	// Should find references to User
	if !strings.Contains(response.Content, "User") {
		t.Errorf("Expected User references not found")
	}

	// Test with max results limit
	params = FindRefsParams{
		Path:       "test.go",
		Symbol:     "User", // Type name - should find type references
		MaxResults: 2,
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

	// Count the number of references found
	content = response.Content
	refCount := strings.Count(content, "Line ")
	if refCount > 2 {
		t.Errorf("Expected at most 2 references, got %d", refCount)
	}

	// Test error cases
	t.Run("MissingPath", func(t *testing.T) {
		params := FindRefsParams{
			Symbol: "test",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  FindRefsToolName,
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
		params := FindRefsParams{
			Path:   "test.go",
			Symbol: "nonexistentFunction",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  FindRefsToolName,
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
		params := FindRefsParams{
			Path:   "nonexistent.go",
			Symbol: "test",
		}
		paramsJSON, _ := json.Marshal(params)

		call := ToolCall{
			ID:    "test-call",
			Name:  FindRefsToolName,
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

func TestReferenceTypeInference(t *testing.T) {
	tool := &findRefsTool{}

	// Test function call detection
	refType := tool.inferReferenceType("result := calculateTotal(items)", "calculateTotal", 9)
	if refType != "call" {
		t.Errorf("Expected 'call' for function call, got '%s'", refType)
	}

	// Test assignment detection
	refType = tool.inferReferenceType("total = calculateTotal(items)", "calculateTotal", 7)
	if refType != "call" {
		t.Errorf("Expected 'call' for assignment with function call, got '%s'", refType)
	}

	// Test variable reference
	refType = tool.inferReferenceType("fmt.Println(total)", "total", 12)
	if refType != "reference" {
		t.Errorf("Expected 'reference' for variable usage, got '%s'", refType)
	}

	// Test declaration
	refType = tool.inferReferenceType("var total int", "total", 4)
	if refType != "declaration" {
		t.Errorf("Expected 'declaration' for variable declaration, got '%s'", refType)
	}
}
