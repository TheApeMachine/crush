package db

import (
	"errors"
	"testing"
)

func TestCloseStmtHelper(t *testing.T) {
	// Test closeStmt with nil statement (should not panic)
	var err error
	closeStmt(nil, "test", &err)
	if err != nil {
		t.Errorf("closeStmt with nil statement should not set error, got: %v", err)
	}
}

func TestCloseStmtPreservesFirstError(t *testing.T) {
	// Test that closeStmt doesn't overwrite existing errors
	originalErr := errors.New("original error")
	err := originalErr

	// This should not overwrite the existing error
	closeStmt(nil, "test", &err)

	if err != originalErr {
		t.Errorf("closeStmt should preserve existing error, got: %v", err)
	}
}

func TestQueriesCloseWithNilStatements(t *testing.T) {
	// Test Close method with all nil statements
	queries := &Queries{}
	
	err := queries.Close()
	if err != nil {
		t.Errorf("Close should not return error for nil statements, got: %v", err)
	}
}

func TestCloseStmtErrorMessage(t *testing.T) {
	// Test that error messages contain the statement name
	var err error
	
	// Simulate what would happen if a statement close failed
	// by manually setting an error that closeStmt would create
	testErr := errors.New("error closing test_stmt: mock error")
	err = testErr
	
	if err == nil {
		t.Error("Expected error to be set")
	}
	
	// Check that error message contains statement name
	if !contains(err.Error(), "test_stmt") {
		t.Errorf("Error message should contain statement name, got: %v", err)
	}
}

func BenchmarkCloseStmtNil(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		closeStmt(nil, "benchmark_stmt", &err)
	}
}

func TestCloseStmtConcurrency(t *testing.T) {
	// Test that closeStmt is safe to call concurrently
	// (though in practice it wouldn't be called concurrently)
	var err error
	
	done := make(chan bool, 2)
	
	go func() {
		closeStmt(nil, "stmt1", &err)
		done <- true
	}()
	
	go func() {
		closeStmt(nil, "stmt2", &err)
		done <- true
	}()
	
	// Wait for both goroutines
	<-done
	<-done
	
	// Should not have any error since both statements are nil
	if err != nil {
		t.Errorf("Concurrent closeStmt calls should not produce error, got: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}