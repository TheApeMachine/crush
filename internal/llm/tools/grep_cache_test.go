package tools

import (
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestRegexCache_Basic(t *testing.T) {
	cache := newRegexCache()

	// Test basic compilation and caching
	pattern := "test.*pattern"
	regex1, err := cache.get(pattern)
	if err != nil {
		t.Fatalf("Failed to compile regex: %v", err)
	}

	// Test cache hit
	regex2, err := cache.get(pattern)
	if err != nil {
		t.Fatalf("Failed to get cached regex: %v", err)
	}

	// Should be the same instance (cached)
	if regex1 != regex2 {
		t.Error("Expected cached regex to be the same instance")
	}

	// Test that it actually works
	if !regex1.MatchString("test123pattern") {
		t.Error("Regex should match test string")
	}
}

func TestRegexCache_InvalidPattern(t *testing.T) {
	cache := newRegexCache()

	// Test invalid regex pattern
	_, err := cache.get("[invalid")
	if err == nil {
		t.Error("Expected error for invalid regex pattern")
	}
}

func TestRegexCache_LRUEviction(t *testing.T) {
	cache := newRegexCache()
	cache.maxSize = 3 // Set small size for testing

	patterns := []string{"pattern1", "pattern2", "pattern3", "pattern4"}

	// Fill cache to capacity
	for i := 0; i < 3; i++ {
		_, err := cache.get(patterns[i])
		if err != nil {
			t.Fatalf("Failed to compile pattern %s: %v", patterns[i], err)
		}
	}

	// Verify cache size
	cache.mu.RLock()
	if len(cache.cache) != 3 {
		t.Errorf("Expected cache size 3, got %d", len(cache.cache))
	}
	cache.mu.RUnlock()

	// Add one more pattern to trigger eviction
	time.Sleep(time.Millisecond) // Ensure different access times
	_, err := cache.get(patterns[3])
	if err != nil {
		t.Fatalf("Failed to compile pattern %s: %v", patterns[3], err)
	}

	// Cache should still be at max size
	cache.mu.RLock()
	cacheSize := len(cache.cache)
	cache.mu.RUnlock()

	if cacheSize != 3 {
		t.Errorf("Expected cache size 3 after eviction, got %d", cacheSize)
	}

	// The oldest entry should have been evicted
	cache.mu.RLock()
	_, exists := cache.cache[patterns[0]]
	cache.mu.RUnlock()

	if exists {
		t.Error("Expected oldest entry to be evicted")
	}
}

func TestRegexCache_ConcurrentAccess(t *testing.T) {
	cache := newRegexCache()
	pattern := "concurrent.*test"

	var wg sync.WaitGroup
	numGoroutines := 10

	// Test concurrent access to same pattern
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			regex, err := cache.get(pattern)
			if err != nil {
				t.Errorf("Failed to get regex: %v", err)
				return
			}
			if !regex.MatchString("concurrent123test") {
				t.Error("Regex should match test string")
			}
		}()
	}

	wg.Wait()

	// Should only have one entry in cache
	cache.mu.RLock()
	if len(cache.cache) != 1 {
		t.Errorf("Expected 1 cache entry, got %d", len(cache.cache))
	}
	cache.mu.RUnlock()
}

func TestRegexCache_AccessTimeUpdates(t *testing.T) {
	cache := newRegexCache()
	pattern := "access.*time"

	// Get pattern first time
	_, err := cache.get(pattern)
	if err != nil {
		t.Fatalf("Failed to compile pattern: %v", err)
	}

	cache.mu.RLock()
	firstAccessTime := cache.accessed[pattern]
	cache.mu.RUnlock()

	// Wait a full second to ensure different Unix timestamps
	time.Sleep(time.Second + time.Millisecond*100)
	_, err = cache.get(pattern)
	if err != nil {
		t.Fatalf("Failed to get cached pattern: %v", err)
	}

	cache.mu.RLock()
	secondAccessTime := cache.accessed[pattern]
	cache.mu.RUnlock()

	if secondAccessTime <= firstAccessTime {
		t.Errorf("Access time should be updated on cache hit: first=%d, second=%d", firstAccessTime, secondAccessTime)
	}
}

func TestRegexCache_EvictLRU_EmptyCache(t *testing.T) {
	cache := newRegexCache()

	// Should not panic on empty cache
	cache.evictLRU()

	cache.mu.RLock()
	cacheSize := len(cache.cache)
	cache.mu.RUnlock()

	if cacheSize != 0 {
		t.Errorf("Expected empty cache, got size %d", cacheSize)
	}
}

func BenchmarkRegexCache_Hit(b *testing.B) {
	cache := newRegexCache()
	pattern := "benchmark.*pattern"

	// Pre-compile pattern
	_, err := cache.get(pattern)
	if err != nil {
		b.Fatalf("Failed to compile pattern: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cache.get(pattern)
		if err != nil {
			b.Fatalf("Failed to get cached pattern: %v", err)
		}
	}
}

func BenchmarkRegexCache_Miss(b *testing.B) {
	cache := newRegexCache()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern := "pattern" + string(rune(i))
		_, err := cache.get(pattern)
		if err != nil {
			b.Fatalf("Failed to compile pattern: %v", err)
		}
	}
}

func BenchmarkRegexCompile_Direct(b *testing.B) {
	pattern := "benchmark.*pattern"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := regexp.Compile(pattern)
		if err != nil {
			b.Fatalf("Failed to compile pattern: %v", err)
		}
	}
}