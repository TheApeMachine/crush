package shell

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
)

func TestBufferPooling(t *testing.T) {
	// Test that buffers can be retrieved from pool
	buffer1 := stdoutBufferPool.Get().(*bytes.Buffer)
	buffer1.WriteString("test content")
	
	// Return to pool
	stdoutBufferPool.Put(buffer1)
	
	// Get another buffer - might be the same one
	buffer2 := stdoutBufferPool.Get().(*bytes.Buffer)
	
	// The buffer should be usable (this is what matters for pooling)
	buffer2.WriteString("new content")
	if buffer2.Len() == 0 {
		t.Error("Buffer should be usable after getting from pool")
	}
	
	// Reset and return to pool
	buffer2.Reset()
	stdoutBufferPool.Put(buffer2)
}

func TestEnvSlicePooling(t *testing.T) {
	// Test that env slices are properly pooled
	slice1 := envSlicePool.Get().([]string)
	slice1 = append(slice1, "TEST=value")
	
	// Return to pool
	envSlicePool.Put(slice1)
	
	// Get another slice
	slice2 := envSlicePool.Get().([]string)
	
	// Should have non-zero capacity (this is what matters for pooling)
	if cap(slice2) == 0 {
		t.Error("Slice from pool should have non-zero capacity")
	}
	
	// Should be usable
	slice2 = append(slice2, "NEW=value")
	if len(slice2) == 0 {
		t.Error("Slice should be usable after getting from pool")
	}
	
	// Reset and return to pool
	slice2 = slice2[:0]
	envSlicePool.Put(slice2)
}

func TestShellGetEnvOptimized(t *testing.T) {
	shell := NewShell(&Options{
		Env: []string{"TEST1=value1", "TEST2=value2"},
	})
	
	// Test that GetEnv returns a proper copy
	env1 := shell.GetEnv()
	env2 := shell.GetEnv()
	
	// Should have same content
	if len(env1) != len(env2) {
		t.Error("Environment copies should have same length")
	}
	
	for i, v := range env1 {
		if v != env2[i] {
			t.Error("Environment copies should have same content")
		}
	}
	
	// Should be different slices (not same reference)
	if &env1[0] == &env2[0] {
		t.Error("Environment copies should be different slice instances")
	}
	
	// Modifying one shouldn't affect the other
	env1[0] = "MODIFIED=value"
	if env2[0] == "MODIFIED=value" {
		t.Error("Modifying one env copy should not affect another")
	}
}

func TestShellExecPOSIXBufferReuse(t *testing.T) {
	shell := NewShell(nil)
	
	// Execute multiple commands to test buffer reuse
	commands := []string{
		"echo 'test1'",
		"echo 'test2'",
		"echo 'test3'",
	}
	
	for _, cmd := range commands {
		stdout, stderr, err := shell.Exec(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Command failed: %v", err)
		}
		
		if !strings.Contains(stdout, "test") {
			t.Errorf("Expected output to contain 'test', got: %s", stdout)
		}
		
		if stderr != "" {
			t.Errorf("Expected empty stderr, got: %s", stderr)
		}
	}
}

func TestShellConcurrentExecution(t *testing.T) {
	shell := NewShell(nil)
	
	var wg sync.WaitGroup
	numGoroutines := 5
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			cmd := "echo 'concurrent test " + string(rune('0'+id)) + "'"
			stdout, _, err := shell.Exec(context.Background(), cmd)
			if err != nil {
				t.Errorf("Concurrent command failed: %v", err)
				return
			}
			
			if !strings.Contains(stdout, "concurrent test") {
				t.Errorf("Expected output to contain 'concurrent test', got: %s", stdout)
			}
		}(i)
	}
	
	wg.Wait()
}

func TestShellEnvironmentVariableOptimization(t *testing.T) {
	shell := NewShell(&Options{
		Env: []string{"INITIAL=value"},
	})
	
	// Set multiple environment variables
	shell.SetEnv("TEST1", "value1")
	shell.SetEnv("TEST2", "value2")
	shell.SetEnv("TEST3", "value3")
	
	env := shell.GetEnv()
	
	// Should contain all variables
	found := make(map[string]bool)
	for _, envVar := range env {
		if strings.HasPrefix(envVar, "TEST1=") {
			found["TEST1"] = true
		}
		if strings.HasPrefix(envVar, "TEST2=") {
			found["TEST2"] = true
		}
		if strings.HasPrefix(envVar, "TEST3=") {
			found["TEST3"] = true
		}
		if strings.HasPrefix(envVar, "INITIAL=") {
			found["INITIAL"] = true
		}
	}
	
	expectedVars := []string{"TEST1", "TEST2", "TEST3", "INITIAL"}
	for _, varName := range expectedVars {
		if !found[varName] {
			t.Errorf("Expected to find environment variable %s", varName)
		}
	}
}

func BenchmarkShellExecWithPooling(b *testing.B) {
	shell := NewShell(nil)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := shell.Exec(context.Background(), "echo 'benchmark test'")
		if err != nil {
			b.Fatalf("Command failed: %v", err)
		}
	}
}

func BenchmarkGetEnvOptimized(b *testing.B) {
	shell := NewShell(&Options{
		Env: make([]string, 50), // Simulate larger environment
	})
	
	// Fill with test data
	for i := 0; i < 50; i++ {
		shell.SetEnv("VAR"+string(rune('0'+i%10)), "value"+string(rune('0'+i%10)))
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := shell.GetEnv()
		if len(env) == 0 {
			b.Error("Expected non-empty environment")
		}
	}
}

func BenchmarkBufferPoolGet(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := stdoutBufferPool.Get().(*bytes.Buffer)
		buf.Reset()
		stdoutBufferPool.Put(buf)
	}
}

func BenchmarkBufferDirectAllocation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &bytes.Buffer{}
	}
}

func TestBufferPoolConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	numGoroutines := 10
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for j := 0; j < 100; j++ {
				buf := stdoutBufferPool.Get().(*bytes.Buffer)
				buf.WriteString("test data")
				buf.Reset()
				stdoutBufferPool.Put(buf)
			}
		}()
	}
	
	wg.Wait()
}

func TestEnvSlicePoolConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	numGoroutines := 10
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for j := 0; j < 100; j++ {
				slice := envSlicePool.Get().([]string)
				slice = append(slice, "TEST=value")
				slice = slice[:0] // Reset length
				envSlicePool.Put(slice)
			}
		}()
	}
	
	wg.Wait()
}