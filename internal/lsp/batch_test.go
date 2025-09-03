package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/lsp/protocol"
)

// mockClient wraps Client to allow method overriding for testing
type mockClient struct {
	*Client
	openFileFunc func(ctx context.Context, filePath string) error
}

func (m *mockClient) OpenFile(ctx context.Context, filePath string) error {
	if m.openFileFunc != nil {
		return m.openFileFunc(ctx, filePath)
	}
	return m.Client.OpenFile(ctx, filePath)
}

func newMockClient() *mockClient {
	return &mockClient{
		Client: &Client{
			name:                  "test-client",
			fileTypes:             []string{".ts", ".tsx", ".js", ".jsx"},
			openFiles:             make(map[string]*OpenFileInfo),
			handlers:              make(map[int32]chan *Message),
			notificationHandlers:  make(map[string]NotificationHandler),
			serverRequestHandlers: make(map[string]ServerRequestHandler),
			diagnostics:           make(map[protocol.DocumentURI][]protocol.Diagnostic),
		},
	}
}

func TestOpenFilesBatch(t *testing.T) {
	// Create a temporary directory with test files
	tempDir := t.TempDir()
	
	// Create test TypeScript files
	testFiles := []string{
		"test1.ts",
		"test2.tsx", 
		"test3.js",
		"test4.jsx",
	}
	
	for _, filename := range testFiles {
		filePath := filepath.Join(tempDir, filename)
		content := "// Test file: " + filename + "\nconsole.log('test');\n"
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}
	
	// Create a mock client
	client := newMockClient()
	
	// Track which files were "opened"
	var openedFiles []string
	var mu sync.Mutex
	
	client.openFileFunc = func(ctx context.Context, filePath string) error {
		mu.Lock()
		openedFiles = append(openedFiles, filepath.Base(filePath))
		mu.Unlock()
		return nil
	}
	
	// Test batch opening
	fullPaths := make([]string, len(testFiles))
	for i, filename := range testFiles {
		fullPaths[i] = filepath.Join(tempDir, filename)
	}
	
	client.openFilesBatch(context.Background(), fullPaths)
	
	// Verify all files were opened
	mu.Lock()
	defer mu.Unlock()
	
	if len(openedFiles) != len(testFiles) {
		t.Errorf("Expected %d files to be opened, got %d", len(testFiles), len(openedFiles))
	}
	
	// Verify all expected files were opened
	openedMap := make(map[string]bool)
	for _, filename := range openedFiles {
		openedMap[filename] = true
	}
	
	for _, expectedFile := range testFiles {
		if !openedMap[expectedFile] {
			t.Errorf("Expected file %s to be opened", expectedFile)
		}
	}
}

func TestOpenFilesBatch_WithErrors(t *testing.T) {
	client := newMockClient()
	
	// Track successful and failed opens
	var successCount, errorCount int32
	
	client.openFileFunc = func(ctx context.Context, filePath string) error {
		// Simulate some files failing to open
		if filepath.Base(filePath) == "error.ts" {
			atomic.AddInt32(&errorCount, 1)
			return os.ErrNotExist
		}
		atomic.AddInt32(&successCount, 1)
		return nil
	}
	
	testFiles := []string{
		"/tmp/success1.ts",
		"/tmp/error.ts",
		"/tmp/success2.ts",
	}
	
	client.openFilesBatch(context.Background(), testFiles)
	
	if atomic.LoadInt32(&successCount) != 2 {
		t.Errorf("Expected 2 successful opens, got %d", successCount)
	}
	
	if atomic.LoadInt32(&errorCount) != 1 {
		t.Errorf("Expected 1 failed open, got %d", errorCount)
	}
}

func TestOpenFilesBatch_Concurrency(t *testing.T) {
	client := newMockClient()
	
	// Track concurrent executions
	var activeCount int32
	var maxConcurrent int32
	
	client.openFileFunc = func(ctx context.Context, filePath string) error {
		current := atomic.AddInt32(&activeCount, 1)
		
		// Update max concurrent if needed
		for {
			max := atomic.LoadInt32(&maxConcurrent)
			if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
				break
			}
		}
		
		// Simulate some work
		time.Sleep(10 * time.Millisecond)
		
		atomic.AddInt32(&activeCount, -1)
		return nil
	}
	
	// Create more files than the semaphore limit (3)
	testFiles := []string{
		"/tmp/file1.ts",
		"/tmp/file2.ts", 
		"/tmp/file3.ts",
		"/tmp/file4.ts",
		"/tmp/file5.ts",
	}
	
	client.openFilesBatch(context.Background(), testFiles)
	
	// Should not exceed semaphore limit of 3
	if maxConcurrent > 3 {
		t.Errorf("Expected max concurrent operations <= 3, got %d", maxConcurrent)
	}
	
	// Should have processed all files
	if atomic.LoadInt32(&activeCount) != 0 {
		t.Errorf("Expected all operations to complete, %d still active", activeCount)
	}
}

func TestOpenTypeScriptFiles_Integration(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()
	
	// Create subdirectories
	srcDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create src directory: %v", err)
	}
	
	nodeModulesDir := filepath.Join(tempDir, "node_modules")
	if err := os.MkdirAll(nodeModulesDir, 0755); err != nil {
		t.Fatalf("Failed to create node_modules directory: %v", err)
	}
	
	// Create TypeScript files in src
	tsFiles := []string{
		filepath.Join(srcDir, "index.ts"),
		filepath.Join(srcDir, "component.tsx"),
		filepath.Join(srcDir, "utils.js"),
	}
	
	for _, filePath := range tsFiles {
		content := "// " + filepath.Base(filePath) + "\nexport {};\n"
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", filePath, err)
		}
	}
	
	// Create a file in node_modules (should be skipped)
	nodeModuleFile := filepath.Join(nodeModulesDir, "library.ts")
	if err := os.WriteFile(nodeModuleFile, []byte("// library"), 0644); err != nil {
		t.Fatalf("Failed to create node_modules file: %v", err)
	}
	
	// Create client
	client := newMockClient()
	
	// Track opened files
	var openedFiles []string
	var mu sync.Mutex
	
	client.openFileFunc = func(ctx context.Context, filePath string) error {
		mu.Lock()
		openedFiles = append(openedFiles, filePath)
		mu.Unlock()
		return nil
	}
	
	// Test the function
	client.openTypeScriptFiles(context.Background(), tempDir)
	
	mu.Lock()
	defer mu.Unlock()
	
	// Should have opened files from src but not node_modules
	if len(openedFiles) != 3 {
		t.Errorf("Expected 3 files to be opened, got %d: %v", len(openedFiles), openedFiles)
	}
	
	// Verify node_modules file was not opened
	for _, filePath := range openedFiles {
		if strings.Contains(filePath, "node_modules") {
			t.Errorf("Should not have opened file from node_modules: %s", filePath)
		}
	}
	
	// Verify expected files were opened
	expectedFiles := make(map[string]bool)
	for _, filePath := range tsFiles {
		expectedFiles[filePath] = false
	}
	
	for _, filePath := range openedFiles {
		if _, exists := expectedFiles[filePath]; exists {
			expectedFiles[filePath] = true
		}
	}
	
	for filePath, opened := range expectedFiles {
		if !opened {
			t.Errorf("Expected file %s to be opened", filePath)
		}
	}
}

func TestShouldSkipDir(t *testing.T) {
	testCases := []struct {
		path     string
		expected bool
	}{
		{"/project/src", false},
		{"/project/.git", true},
		{"/project/node_modules", true},
		{"/project/dist", true},
		{"/project/build", true},
		{"/project/coverage", true},
		{"/project/vendor", true},
		{"/project/target", true},
		{"/project/.hidden", true},
		{"/project/normal", false},
	}
	
	for _, tc := range testCases {
		result := shouldSkipDir(tc.path)
		if result != tc.expected {
			t.Errorf("shouldSkipDir(%s) = %v, expected %v", tc.path, result, tc.expected)
		}
	}
}

func BenchmarkOpenFilesBatch(b *testing.B) {
	client := newMockClient()
	
	// Mock OpenFile to do minimal work
	client.openFileFunc = func(ctx context.Context, filePath string) error {
		return nil
	}
	
	testFiles := []string{
		"/tmp/file1.ts",
		"/tmp/file2.ts",
		"/tmp/file3.ts",
		"/tmp/file4.ts",
		"/tmp/file5.ts",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.openFilesBatch(context.Background(), testFiles)
	}
}

func BenchmarkOpenFilesSequential(b *testing.B) {
	client := newMockClient()
	
	// Mock OpenFile to do minimal work
	client.openFileFunc = func(ctx context.Context, filePath string) error {
		return nil
	}
	
	testFiles := []string{
		"/tmp/file1.ts",
		"/tmp/file2.ts", 
		"/tmp/file3.ts",
		"/tmp/file4.ts",
		"/tmp/file5.ts",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Sequential opening (old way)
		for _, filePath := range testFiles {
			client.OpenFile(context.Background(), filePath)
		}
	}
}