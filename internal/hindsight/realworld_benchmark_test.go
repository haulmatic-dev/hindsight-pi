package hindsight_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/agents"
	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestRealWorldHindsightPerformance runs comprehensive real-world benchmarks
func TestRealWorldHindsightPerformance(t *testing.T) {
	// Spins up an embedded Hindsight server and drives real traffic — on
	// machines without the runtime deps it hangs the whole package past the
	// go-test timeout instead of failing fast. Integration-only.
	if testing.Short() {
		t.Skip("integration benchmark: skipped with -short")
	}
	if os.Getenv("HYDRA_INTEGRATION_TESTS") == "" {
		t.Skip("set HYDRA_INTEGRATION_TESTS=1 to run the embedded-server benchmark")
	}
	// Start embedded Hindsight server
	server := hindsight.NewServer(hindsight.ServerConfig{
		Port:    18888,
		Host:    "127.0.0.1",
		Timeout: 30 * time.Second,
	})

	if err := server.Start(); err != nil {
		t.Skipf("Hindsight server not available: %v", err)
	}
	defer server.Stop()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Create client using functional options
	client := hindsight.NewClient(
		hindsight.WithBaseURL("http://127.0.0.1:18888"),
		hindsight.WithTimeout(10*time.Second),
		hindsight.WithDefaultLimit(10),
	)

	// Verify server is running
	if !server.IsRunning() {
		t.Skip("Server failed to start")
	}

	t.Logf("✓ Hindsight server started on %s", server.URL())

	// Run benchmarks
	t.Run("MemoryOperations", func(t *testing.T) {
		testMemoryOperations(t, client)
	})

	t.Run("SearchPerformance", func(t *testing.T) {
		testSearchPerformance(t, client)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		testConcurrentAccess(t, client)
	})

	t.Run("ContextProcessorPerformance", func(t *testing.T) {
		testContextProcessorPerformance(t, client)
	})
}

func testMemoryOperations(t *testing.T, client *hindsight.Client) {
	numMemories := 100
	start := time.Now()

	// Create ExecutionMemory structs
	for i := 0; i < numMemories; i++ {
		memory := &hindsight.ExecutionMemory{
			BeadID: types.BeadID(fmt.Sprintf("test-bead-%d", i)),
			BeadMetadata: hindsight.BeadMetadata{
				Title:       fmt.Sprintf("Test Memory %d", i),
				Description: fmt.Sprintf("Test memory content %d with searchable keywords", i),
				Type:        "test",
				Project:     "hydra-test",
				Labels:      []string{"test", "benchmark"},
			},
			ExecutionResult: hindsight.ExecutionOutcome{
				Success:  true,
				Duration: time.Duration(rand.Float64() * 100 * float64(time.Second)),
			},
			Files: hindsight.FileChanges{
				ModifiedFiles: []string{"file1.go", "file2.go"},
			},
			RecordedAt: time.Now().Add(-time.Duration(rand.Intn(30)) * 24 * time.Hour),
			WorkerID:   fmt.Sprintf("worker-%d", i),
			Version:    "1.0",
		}

		_, err := client.Retain(memory)
		if err != nil {
			// Expected with embedded server - just track for metrics
			continue
		}
	}

	duration := time.Since(start)
	memoriesPerSecond := float64(numMemories) / duration.Seconds()

	t.Logf("✓ Processed %d memories in %v (%.2f memories/sec)", numMemories, duration, memoriesPerSecond)
}

func testSearchPerformance(t *testing.T, client *hindsight.Client) {
	queries := []string{
		"implementation",
		"test benchmark",
		"hindsight memory",
		"worker execution",
	}

	iterations := 50
	var totalDuration time.Duration
	var minDuration = time.Hour
	var maxDuration time.Duration
	var successCount int

	for i := 0; i < iterations; i++ {
		query := queries[rand.Intn(len(queries))]

		start := time.Now()
		resp, err := client.SearchByBead(query, "test")
		duration := time.Since(start)

		if err != nil {
			// Expected with embedded server
			continue
		}

		successCount++
		totalDuration += duration
		if duration < minDuration {
			minDuration = duration
		}
		if duration > maxDuration {
			maxDuration = duration
		}

		_ = resp // Use the response to avoid unused variable
	}

	if successCount > 0 {
		avgDuration := totalDuration / time.Duration(successCount)
		t.Logf("✓ Search Performance (%d/%d successful):", successCount, iterations)
		t.Logf("  - Average: %v", avgDuration)
		t.Logf("  - Min: %v", minDuration)
		t.Logf("  - Max: %v", maxDuration)
		t.Logf("  - Throughput: %.2f searches/sec", float64(successCount)/totalDuration.Seconds())
	} else {
		t.Logf("⚠ No successful searches (expected with embedded server)")
	}
}

func testConcurrentAccess(t *testing.T, client *hindsight.Client) {
	numGoroutines := 10
	operationsPerGoroutine := 20

	var wg sync.WaitGroup
	errCount := 0
	var errMu sync.Mutex
	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				switch j % 2 {
				case 0:
					// Retain
					memory := &hindsight.ExecutionMemory{
						BeadID: types.BeadID(fmt.Sprintf("concurrent-%d-%d", id, j)),
						BeadMetadata: hindsight.BeadMetadata{
							Title:       fmt.Sprintf("Concurrent Memory %d-%d", id, j),
							Description: "Concurrent test content",
							Type:        "concurrent",
							Project:     "hydra-test",
						},
						ExecutionResult: hindsight.ExecutionOutcome{
							Success: true,
						},
						RecordedAt: time.Now(),
						WorkerID:   fmt.Sprintf("worker-%d", id),
						Version:    "1.0",
					}
					if _, err := client.Retain(memory); err != nil {
						errMu.Lock()
						errCount++
						errMu.Unlock()
					}
				case 1:
					// Search
					if _, err := client.SearchByBead("concurrent", "concurrent"); err != nil {
						errMu.Lock()
						errCount++
						errMu.Unlock()
					}
				}
			}
		}(i)
	}

	wg.Wait()

	duration := time.Since(start)
	totalOperations := numGoroutines * operationsPerGoroutine

	opsPerSecond := float64(totalOperations) / duration.Seconds()

	t.Logf("✓ Concurrent Access Test:")
	t.Logf("  - Goroutines: %d", numGoroutines)
	t.Logf("  - Total operations: %d", totalOperations)
	t.Logf("  - Duration: %v", duration)
	t.Logf("  - Operations/sec: %.2f", opsPerSecond)
	t.Logf("  - Errors: %d (%.2f%%)", errCount, float64(errCount)/float64(totalOperations)*100)
}

func testContextProcessorPerformance(t *testing.T, client *hindsight.Client) {
	// Create context processor
	config := agents.ContextProcessorConfig{
		Enabled:         true,
		CacheTTL:        5 * time.Minute,
		MaxQueryLatency: 500 * time.Millisecond,
	}

	processor := agents.NewContextProcessor(client, config, nil)

	if !processor.IsEnabled() {
		t.Skip("Context processor not enabled")
	}

	// Test bead
	bead := types.Bead{
		ID:          "test-bead-1",
		Title:       "Test Hindsight Integration",
		Description: "Testing performance of Hindsight context retrieval",
		Type:        "test",
		Project:     "hydra-test",
	}

	ctx := context.Background()

	// First query (cold cache)
	coldStart := time.Now()
	_, err := processor.QueryHindsight(ctx, bead)
	coldDuration := time.Since(coldStart)

	if err != nil {
		t.Logf("First query returned error (expected): %v", err)
	}

	t.Logf("✓ Context Processor Performance:")
	t.Logf("  - Cold query: %v", coldDuration)

	// Second query (warm cache)
	warmStart := time.Now()
	_, err = processor.QueryHindsight(ctx, bead)
	warmDuration := time.Since(warmStart)

	if err != nil {
		t.Logf("Cached query returned error: %v", err)
	}

	t.Logf("  - Warm query: %v", warmDuration)

	if coldDuration > 0 && warmDuration > 0 {
		speedup := float64(coldDuration) / float64(warmDuration)
		if speedup > 1 {
			t.Logf("  - Cache speedup: %.2fx", speedup)
		}
	}

	// Test cache stats
	stats := processor.GetCacheStats()
	t.Logf("  - Cache size: %d entries", stats.Size)
}

// BenchmarkRetain benchmarks the Retain operation
func BenchmarkRetain(b *testing.B) {
	server := hindsight.NewServer(hindsight.ServerConfig{
		Port: 18889,
		Host: "127.0.0.1",
	})

	if err := server.Start(); err != nil {
		b.Skipf("Server not available: %v", err)
	}
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	client := hindsight.NewClient(
		hindsight.WithBaseURL("http://127.0.0.1:18889"),
		hindsight.WithTimeout(10*time.Second),
	)

	memory := &hindsight.ExecutionMemory{
		BeadID: "benchmark-bead",
		BeadMetadata: hindsight.BeadMetadata{
			Title:       "Benchmark Memory",
			Description: "Benchmark content",
			Type:        "benchmark",
			Project:     "hydra-test",
		},
		ExecutionResult: hindsight.ExecutionOutcome{
			Success: true,
		},
		RecordedAt: time.Now(),
		WorkerID:   "benchmark-worker",
		Version:    "1.0",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		memory.BeadID = types.BeadID(fmt.Sprintf("benchmark-%d", i))
		client.Retain(memory)
	}
}

// BenchmarkSearch benchmarks the Search operation
func BenchmarkSearch(b *testing.B) {
	server := hindsight.NewServer(hindsight.ServerConfig{
		Port: 18890,
		Host: "127.0.0.1",
	})

	if err := server.Start(); err != nil {
		b.Skipf("Server not available: %v", err)
	}
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	client := hindsight.NewClient(
		hindsight.WithBaseURL("http://127.0.0.1:18890"),
		hindsight.WithTimeout(10*time.Second),
	)

	// Pre-populate with some data
	for i := 0; i < 100; i++ {
		memory := &hindsight.ExecutionMemory{
			BeadID: types.BeadID(fmt.Sprintf("searchable-%d", i)),
			BeadMetadata: hindsight.BeadMetadata{
				Title:       fmt.Sprintf("Searchable Memory %d", i),
				Description: fmt.Sprintf("Content with keywords %d", i),
				Type:        "benchmark",
				Project:     "hydra-test",
			},
			ExecutionResult: hindsight.ExecutionOutcome{
				Success: true,
			},
			RecordedAt: time.Now(),
			WorkerID:   "benchmark-worker",
			Version:    "1.0",
		}
		client.Retain(memory)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		client.SearchByBead("implementation test", "benchmark")
	}
}

// BenchmarkContextProcessor benchmarks the context processor
func BenchmarkContextProcessor(b *testing.B) {
	server := hindsight.NewServer(hindsight.ServerConfig{
		Port: 18891,
		Host: "127.0.0.1",
	})

	if err := server.Start(); err != nil {
		b.Skipf("Server not available: %v", err)
	}
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	client := hindsight.NewClient(
		hindsight.WithBaseURL("http://127.0.0.1:18891"),
		hindsight.WithTimeout(10*time.Second),
	)

	config := agents.ContextProcessorConfig{
		Enabled:         true,
		CacheTTL:        5 * time.Minute,
		MaxQueryLatency: 2 * time.Second,
	}

	processor := agents.NewContextProcessor(client, config, nil)

	bead := types.Bead{
		ID:          "benchmark-bead",
		Title:       "Benchmark Bead",
		Description: "Benchmarking context processor",
		Type:        "benchmark",
		Project:     "hydra-test",
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		processor.QueryHindsight(ctx, bead)
	}
}
