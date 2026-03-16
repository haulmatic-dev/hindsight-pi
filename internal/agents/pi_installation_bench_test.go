package agents

import (
	"bytes"
	"log"
	"os/exec"
	"testing"
	"time"
)

// BenchmarkCheckInstallation_Cached verifies cached installation check performance
// Target: <1ms per call when cached
func BenchmarkCheckInstallation_Cached(b *testing.B) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	connector := NewPiConnector(PiConfig{}, logger)

	// Pre-populate cache
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			return "/usr/bin/pi", nil
		},
	}
	connector.executor = mockExec

	// Warm up the cache
	err := connector.CheckInstallation()
	if err != nil {
		b.Fatalf("Failed to warm up cache: %v", err)
	}

	// Reset timer to exclude setup
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		err := connector.CheckInstallation()
		if err != nil {
			b.Fatalf("CheckInstallation failed: %v", err)
		}
	}
}

// BenchmarkEnsureInstalled_Cached verifies cached ensureInstalled performance
// Target: <1ms per call when cached
func BenchmarkEnsureInstalled_Cached(b *testing.B) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "pi" {
				return "/usr/bin/pi", nil
			}
			return "", exec.ErrNotFound
		},
	}
	connector.executor = mockExec

	// Warm up the cache
	err := connector.ensureInstalled()
	if err != nil {
		b.Fatalf("Failed to warm up cache: %v", err)
	}

	// Reset timer to exclude setup
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		err := connector.ensureInstalled()
		if err != nil {
			b.Fatalf("ensureInstalled failed: %v", err)
		}
	}
}

// BenchmarkCheckInstallation_PathLookup compares cached vs uncached performance
func BenchmarkCheckInstallation_PathLookup(b *testing.B) {
	b.Run("cached", func(b *testing.B) {
		var buf bytes.Buffer
		logger := log.New(&buf, "", 0)
		connector := NewPiConnector(PiConfig{}, logger)

		callCount := 0
		mockExec := &MockExecutor{
			LookPathFunc: func(file string) (string, error) {
				callCount++
				return "/usr/bin/pi", nil
			},
		}
		connector.executor = mockExec

		// Warm up cache
		connector.CheckInstallation()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			connector.CheckInstallation()
		}
		b.ReportMetric(float64(callCount), "path_lookups")
	})

	b.Run("uncached", func(b *testing.B) {
		var buf bytes.Buffer
		logger := log.New(&buf, "", 0)

		callCount := 0
		mockExec := &MockExecutor{
			LookPathFunc: func(file string) (string, error) {
				callCount++
				return "/usr/bin/pi", nil
			},
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Create new connector each time to avoid caching
			connector := NewPiConnector(PiConfig{}, logger)
			connector.executor = mockExec
			connector.CheckInstallation()
		}
		b.ReportMetric(float64(callCount), "path_lookups")
	})
}

// BenchmarkInstallationCacheTTL verifies that cache expires after TTL
func BenchmarkInstallationCacheTTL(b *testing.B) {
	// This test verifies cache behavior - actual TTL is 5 minutes
	// For benchmarking, we just verify the logic path
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	connector := NewPiConnector(PiConfig{}, logger)

	callCount := 0
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			callCount++
			return "/usr/bin/pi", nil
		},
	}
	connector.executor = mockExec

	// First call - should hit LookPath
	connector.CheckInstallation()
	if callCount != 1 {
		b.Fatalf("Expected 1 LookPath call, got %d", callCount)
	}

	// Second call - should use cache
	connector.CheckInstallation()
	if callCount != 1 {
		b.Fatalf("Expected 1 LookPath call (cached), got %d", callCount)
	}

	// Manually expire cache for testing
	connector.mu.Lock()
	connector.pathsCacheTime = time.Now().Add(-installationCacheTTL - time.Second)
	connector.mu.Unlock()

	// Third call - should hit LookPath again (cache expired)
	connector.CheckInstallation()
	if callCount != 2 {
		b.Fatalf("Expected 2 LookPath calls (cache expired), got %d", callCount)
	}
}
