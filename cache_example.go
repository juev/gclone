package main

import (
	"fmt"
	"time"
)

// Example of how to use the improved directory cache with performance monitoring
func ExampleCacheUsage() {
	// Create custom cache configuration
	cacheConfig := &CacheConfig{
		TTL:                  2 * time.Minute,  // Longer TTL for better performance
		CleanupInterval:      1 * time.Minute,  // More frequent cleanup
		MaxEntries:          500,               // Smaller cache for this example
		EnablePeriodicCleanup: true,
	}

	// Create cache with custom configuration
	fs := &DefaultFileSystem{}
	cache := NewDirCache(cacheConfig, fs)
	defer cache.Close() // Important: close to stop cleanup goroutine

	// Test some directory checks
	testDirs := []string{
		"/tmp",
		"/etc",
		"/var",
		"/usr/bin",
		"/nonexistent/dir",
	}

	fmt.Println("Testing directory cache performance...")
	
	// First round - cache misses
	start := time.Now()
	for _, dir := range testDirs {
		exists := cache.IsDirectoryNotEmpty(dir)
		fmt.Printf("Directory %s exists and not empty: %v\n", dir, exists)
	}
	firstRoundTime := time.Since(start)

	// Second round - cache hits (should be much faster)
	start = time.Now()
	for _, dir := range testDirs {
		exists := cache.IsDirectoryNotEmpty(dir)
		fmt.Printf("Directory %s exists and not empty: %v (cached)\n", dir, exists)
	}
	secondRoundTime := time.Since(start)

	// Get and display cache statistics
	stats := cache.GetStats()
	fmt.Printf("\n=== Cache Performance Statistics ===\n")
	fmt.Printf("Cache Hits: %d\n", stats.Hits)
	fmt.Printf("Cache Misses: %d\n", stats.Misses)
	fmt.Printf("Hit Ratio: %.2f%%\n", stats.HitRatio()*100)
	fmt.Printf("Total Entries: %d\n", stats.TotalSize)
	fmt.Printf("Evictions: %d\n", stats.Evictions)
	
	fmt.Printf("\n=== Performance Improvement ===\n")
	fmt.Printf("First round (cache misses): %v\n", firstRoundTime)
	fmt.Printf("Second round (cache hits): %v\n", secondRoundTime)
	if secondRoundTime.Nanoseconds() > 0 {
		speedup := float64(firstRoundTime.Nanoseconds()) / float64(secondRoundTime.Nanoseconds())
		fmt.Printf("Speedup: %.1fx faster with cache\n", speedup)
	}
}

// Uncomment to run the example:
// func main() {
// 	ExampleCacheUsage()
// }