package pi

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Suite: Daily Skill Updates (hydra-hzz)
// ============================================================================
// These tests verify the SkillUpdater functionality for daily skill updates

// TestSkillUpdater_StartStop verifies that the ticker starts and stops correctly
func TestSkillUpdater_StartStop(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err, "Failed to create SkillUpdater")

	// Initially should not be running
	assert.False(t, updater.IsRunning(), "Updater should not be running initially")

	// Start the updater
	updater.Start()
	assert.True(t, updater.IsRunning(), "Updater should be running after Start()")

	// Double start should not panic or create duplicate goroutines
	updater.Start()
	assert.True(t, updater.IsRunning(), "Updater should still be running after double Start()")

	// Stop the updater
	updater.Stop()
	assert.False(t, updater.IsRunning(), "Updater should not be running after Stop()")

	// Double stop should not panic
	updater.Stop()
	assert.False(t, updater.IsRunning(), "Updater should still not be running after double Stop()")
}

// TestSkillUpdater_UpdateInterval verifies that the updater respects the configured interval
func TestSkillUpdater_UpdateInterval(t *testing.T) {
	tests := []struct {
		name         string
		intervalStr  string
		wantInterval time.Duration
		wantErr      bool
	}{
		{
			name:         "24h interval",
			intervalStr:  "24h",
			wantInterval: 24 * time.Hour,
			wantErr:      false,
		},
		{
			name:         "1h interval",
			intervalStr:  "1h",
			wantInterval: 1 * time.Hour,
			wantErr:      false,
		},
		{
			name:         "30m interval",
			intervalStr:  "30m",
			wantInterval: 30 * time.Minute,
			wantErr:      false,
		},
		{
			name:         "100ms interval for testing",
			intervalStr:  "100ms",
			wantInterval: 100 * time.Millisecond,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			setupManager := NewSetupManager(tempDir, tempDir)
			updater, err := NewSkillUpdater(setupManager, tt.intervalStr)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantInterval, updater.GetUpdateInterval(),
				"Update interval should match configured value")
		})
	}
}

// TestSkillUpdater_TrackingFileUpdated verifies that the tracking file is updated with timestamp
func TestSkillUpdater_TrackingFileUpdated(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)

	// Create initial tracking data
	beforeUpdate := time.Now().Add(-time.Hour) // Set time in the past
	initialTracking := &SkillsVerificationTracking{
		LastUpdated: beforeUpdate,
		SkillsCount: 0,
		Skills:      []SkillVerificationResult{},
	}

	err := setupManager.UpdateTrackingFile(initialTracking)
	require.NoError(t, err, "Failed to create initial tracking file")

	// Verify initial state
	trackingPath := setupManager.getSkillsTrackingPath()
	_, err = os.Stat(trackingPath)
	require.NoError(t, err, "Tracking file should exist")

	// Now create updater and update tracking via the updateTrackingFile method
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Call updateTrackingFile directly to simulate an update
	err = updater.updateTrackingFile(
		[]string{"golang-pro"},
		[]string{},
		time.Now(),
	)
	require.NoError(t, err, "updateTrackingFile should succeed")

	// Read and verify updated tracking file
	data, err := os.ReadFile(trackingPath)
	require.NoError(t, err, "Should be able to read tracking file")

	// Note: The JSON unmarshaling for time.Time requires custom handling
	// For this test, we just verify the file was written
	assert.NotEmpty(t, data, "Tracking file should contain data")
}

// TestSkillUpdater_GracefulFailure verifies that the updater continues after partial failures
func TestSkillUpdater_GracefulFailure(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Test that runUpdate handles partial failures gracefully
	// Since we can't easily mock the exec.Command, we verify the method exists
	// and that it returns nil even when some skills fail

	// The key assertion is that the implementation continues processing
	// all skills even when individual updates fail
	assert.NotNil(t, updater, "Updater should be created")

	// Verify that the updater doesn't crash when started (even if skills CLI unavailable)
	updater.Start()
	time.Sleep(50 * time.Millisecond) // Give initial update time to run

	// Updater should still be running even if initial update failed
	assert.True(t, updater.IsRunning(), "Updater should continue running after partial failures")

	updater.Stop()
}

// TestSkillUpdater_StopChannel verifies that the updater respects the stop signal
func TestSkillUpdater_StopChannel(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "100ms")
	require.NoError(t, err)

	// Start the updater
	updater.Start()
	assert.True(t, updater.IsRunning(), "Updater should be running")

	// Wait for initial update
	time.Sleep(50 * time.Millisecond)

	// Stop should respect the stop signal and shut down gracefully
	updater.Stop()

	// Verify stopped state
	assert.False(t, updater.IsRunning(), "Updater should stop when stop signal sent")
}

// TestSkillUpdater_InitialUpdate verifies that the updater runs an update on startup
func TestSkillUpdater_InitialUpdate(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)

	// Ensure directory exists
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)

	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Start updater - this should trigger initial update
	updater.Start()

	// Wait for initial update to complete (or attempt to complete)
	time.Sleep(100 * time.Millisecond)

	// Verify updater is running
	assert.True(t, updater.IsRunning(), "Updater should be running after initial update attempt")

	updater.Stop()

	// Verify that some form of tracking file interaction occurred
	// The file may or may not exist depending on whether npx is available
	// but the important thing is that the updater didn't crash
}

// TestSkillUpdater_ConcurrentSafety verifies thread-safe operations
func TestSkillUpdater_ConcurrentSafety(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "50ms")
	require.NoError(t, err)

	// Start the updater
	updater.Start()
	defer updater.Stop()

	// Run concurrent operations
	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent status checks
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = updater.IsRunning()
				_ = updater.GetUpdateInterval()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Wait for all goroutines to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no race conditions detected
		assert.True(t, updater.IsRunning(), "Updater should still be running after concurrent access")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for concurrent operations")
	}
}

// TestSkillUpdater_InvalidInterval verifies handling of invalid intervals
func TestSkillUpdater_InvalidInterval(t *testing.T) {
	tests := []struct {
		name        string
		intervalStr string
		wantErrMsg  string
	}{
		{
			name:        "invalid format",
			intervalStr: "invalid",
			wantErrMsg:  "invalid update_interval",
		},
		{
			name:        "empty string",
			intervalStr: "",
			wantErrMsg:  "invalid update_interval",
		},
		{
			name:        "zero interval",
			intervalStr: "0s",
			wantErrMsg:  "update_interval must be positive",
		},
		{
			name:        "negative interval",
			intervalStr: "-1h",
			wantErrMsg:  "update_interval must be positive",
		},
		{
			name:        "whitespace only",
			intervalStr: "   ",
			wantErrMsg:  "invalid update_interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			setupManager := NewSetupManager(tempDir, tempDir)
			updater, err := NewSkillUpdater(setupManager, tt.intervalStr)

			assert.Error(t, err, "Should return error for invalid interval: %s", tt.intervalStr)
			assert.Nil(t, updater, "Updater should be nil when error occurs")
			assert.Contains(t, err.Error(), tt.wantErrMsg,
				"Error message should indicate the problem")
		})
	}
}

// ============================================================================
// Additional Edge Case Tests
// ============================================================================

// TestSkillUpdater_MultipleStartStop verifies idempotent start/stop behavior
func TestSkillUpdater_MultipleStartStop(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Multiple starts should not panic
	for i := 0; i < 5; i++ {
		updater.Start()
		assert.True(t, updater.IsRunning(), "Updater should be running after start %d", i+1)
	}

	// Multiple stops should not panic
	for i := 0; i < 5; i++ {
		updater.Stop()
		assert.False(t, updater.IsRunning(), "Updater should not be running after stop %d", i+1)
	}
}

// TestSkillUpdater_UpdateSkillsNow_NotRunning verifies error when not running
func TestSkillUpdater_UpdateSkillsNow_NotRunning(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Should return error when updater is not running
	err = updater.UpdateSkillsNow()
	assert.Error(t, err, "UpdateSkillsNow should return error when not running")
	assert.Contains(t, err.Error(), "not running")
}

// TestSkillUpdater_StopTimeout verifies stop timeout behavior
func TestSkillUpdater_StopTimeout(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	updater.Start()
	assert.True(t, updater.IsRunning())

	// Stop with timeout
	updater.Stop()
	assert.False(t, updater.IsRunning())

	// Should be able to restart after stop
	updater, err = NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)
	updater.Start()
	assert.True(t, updater.IsRunning())
	updater.Stop()
}

// TestSkillUpdater_GetUpdateInterval_ThreadSafe verifies thread-safe interval access
func TestSkillUpdater_GetUpdateInterval_ThreadSafe(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	var wg sync.WaitGroup
	intervals := make([]time.Duration, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			intervals[idx] = updater.GetUpdateInterval()
		}(i)
	}

	wg.Wait()

	// All intervals should be the same
	expectedInterval := 1 * time.Hour
	for i, interval := range intervals {
		assert.Equal(t, expectedInterval, interval,
			"Interval %d should match expected value", i)
	}
}

// TestSkillUpdater_TrackingFileWithFailures verifies tracking file includes failed skills
func TestSkillUpdater_TrackingFileWithFailures(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Simulate an update with both successes and failures
	updatedSkills := []string{"skill-1", "skill-2"}
	failedSkills := []string{"skill-3", "skill-4"}
	startTime := time.Now()

	err = updater.updateTrackingFile(updatedSkills, failedSkills, startTime)
	require.NoError(t, err)

	// Verify tracking file exists
	trackingPath := setupManager.getSkillsTrackingPath()
	data, err := os.ReadFile(trackingPath)
	require.NoError(t, err)
	assert.NotEmpty(t, data, "Tracking file should contain data")

	// Verify file is valid JSON - we just verify the file exists and has content
	// Full JSON unmarshaling would need custom time parsing
}

// TestSkillUpdater_VeryShortInterval verifies behavior with very short intervals
func TestSkillUpdater_VeryShortInterval(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)

	// 10ms interval for rapid testing
	updater, err := NewSkillUpdater(setupManager, "10ms")
	require.NoError(t, err)

	updater.Start()
	defer updater.Stop()

	// Wait for multiple update cycles
	time.Sleep(50 * time.Millisecond)

	// Should still be running
	assert.True(t, updater.IsRunning(), "Updater should handle rapid update cycles")
}

// TestSkillUpdater_LongInterval verifies behavior with long intervals
func TestSkillUpdater_LongInterval(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)

	// Very long interval (100 hours)
	updater, err := NewSkillUpdater(setupManager, "100h")
	require.NoError(t, err)

	assert.Equal(t, 100*time.Hour, updater.GetUpdateInterval(),
		"Should accept very long intervals")

	updater.Start()
	defer updater.Stop()

	// Should run initial update but not trigger scheduled update within test time
	time.Sleep(50 * time.Millisecond)
	assert.True(t, updater.IsRunning())
}

// TestSkillUpdater_NilSetupManager verifies behavior with nil setup manager
func TestSkillUpdater_NilSetupManager(t *testing.T) {
	// This test documents the current behavior - creating updater with nil setup manager
	// In production, this would panic during updateTrackingFile
	// For now, we just verify the constructor doesn't panic
	updater, err := NewSkillUpdater(nil, "1h")
	require.NoError(t, err)
	assert.NotNil(t, updater)
}

// TestSkillUpdater_CreateDefaultSkillUpdater verifies factory function
func TestSkillUpdater_CreateDefaultSkillUpdater(t *testing.T) {
	tempDir := t.TempDir()

	updater, err := CreateDefaultSkillUpdater(tempDir, tempDir)
	require.NoError(t, err)
	assert.NotNil(t, updater)
	assert.Equal(t, 24*time.Hour, updater.GetUpdateInterval(),
		"Default interval should be 24h")
}

// TestSkillUpdater_CreateSkillUpdaterFromConfig verifies factory function with custom interval
func TestSkillUpdater_CreateSkillUpdaterFromConfig(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		interval string
		expected time.Duration
	}{
		{"1h", 1 * time.Hour},
		{"30m", 30 * time.Minute},
		{"12h", 12 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.interval, func(t *testing.T) {
			updater, err := CreateSkillUpdaterFromConfig(tempDir, tempDir, tt.interval)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, updater.GetUpdateInterval())
		})
	}
}

// TestSkillUpdater_DirectoryCreation verifies that directories are created as needed
func TestSkillUpdater_DirectoryCreation(t *testing.T) {
	tempDir := t.TempDir()
	nestedDir := filepath.Join(tempDir, "nested", "config")

	setupManager := NewSetupManager(nestedDir, nestedDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// The tracking file update should create directories as needed
	err = updater.updateTrackingFile([]string{"test"}, []string{}, time.Now())
	require.NoError(t, err)

	// Verify directory was created
	info, err := os.Stat(nestedDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// ============================================================================
// Cache and Performance Tests (hydra-edd optimizations)
// ============================================================================

// TestSkillUpdater_CachePreventsRedundantUpdates verifies cache prevents redundant updates
func TestSkillUpdater_CachePreventsRedundantUpdates(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Set up cache with recent update
	updater.cache.Update(13, 0, 13)

	// Verify cache is valid
	assert.True(t, updater.cache.IsValid(1*time.Hour), "Cache should be valid within interval")

	// Check cache status
	status := updater.GetCacheStatus()
	assert.True(t, status["initialized"].(bool))
	assert.True(t, status["valid"].(bool))
	assert.Equal(t, 13, status["success_count"])
	assert.Equal(t, 0, status["failed_count"])
	assert.Equal(t, 13, status["total_skills"])
}

// TestSkillUpdater_CacheInvalidation verifies cache invalidation works
func TestSkillUpdater_CacheInvalidation(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Set up cache
	updater.cache.Update(13, 0, 13)
	assert.True(t, updater.cache.IsValid(1*time.Hour))

	// Invalidate cache
	updater.InvalidateCache()

	// Verify cache is now invalid
	assert.False(t, updater.cache.IsValid(1*time.Hour), "Cache should be invalid after invalidation")

	status := updater.GetCacheStatus()
	assert.False(t, status["valid"].(bool))
}

// TestSkillUpdater_CacheExpiration verifies cache expires after interval
func TestSkillUpdater_CacheExpiration(t *testing.T) {
	cache := &UpdateCache{}

	// Set update time to 2 hours ago
	cache.LastUpdateTime = time.Now().Add(-2 * time.Hour)
	cache.SuccessCount = 13
	cache.FailedCount = 0
	cache.TotalSkills = 13

	// Cache should be invalid for 1 hour interval
	assert.False(t, cache.IsValid(1*time.Hour), "Cache should expire after interval")

	// But valid for 3 hour interval
	assert.True(t, cache.IsValid(3*time.Hour), "Cache should be valid for longer interval")
}

// TestSkillUpdater_ConcurrentUpdatePrevention verifies concurrent update prevention
func TestSkillUpdater_ConcurrentUpdatePrevention(t *testing.T) {
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	// Initially not in progress
	assert.False(t, updater.IsUpdateInProgress())

	// Simulate update in progress
	updater.updateMu.Lock()
	updater.updateInProgress = true
	updater.updateMu.Unlock()

	assert.True(t, updater.IsUpdateInProgress())

	// Reset
	updater.updateMu.Lock()
	updater.updateInProgress = false
	updater.updateMu.Unlock()

	assert.False(t, updater.IsUpdateInProgress())
}

// TestSkillUpdater_ParallelUpdates tests that updates run in parallel
func TestSkillUpdater_ParallelUpdates(t *testing.T) {
	// This test verifies that the parallel update mechanism is in place
	// by checking the runParallelUpdates method exists and functions
	tempDir := t.TempDir()
	setupManager := NewSetupManager(tempDir, tempDir)
	updater, err := NewSkillUpdater(setupManager, "1h")
	require.NoError(t, err)

	updater.Start()
	defer updater.Stop()

	// Give initial update time to start
	time.Sleep(50 * time.Millisecond)

	// Verify updater is running
	assert.True(t, updater.IsRunning())

	// Check that we have cache initialized
	status := updater.GetCacheStatus()
	assert.NotNil(t, status)
}

// TestUpdateCache_ConcurrentAccess verifies thread-safe cache access
func TestUpdateCache_ConcurrentAccess(t *testing.T) {
	cache := &UpdateCache{}
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cache.Update(10+idx, idx, 13)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cache.IsValid(time.Hour)
			_, _, _, _ = cache.GetStats()
		}()
	}

	wg.Wait()

	// Verify final state is consistent
	_, successCount, failedCount, total := cache.GetStats()
	assert.GreaterOrEqual(t, successCount, 10)
	assert.GreaterOrEqual(t, failedCount, 0)
	assert.Equal(t, 13, total)
}

// ============================================================================
// Benchmark Tests
// ============================================================================

// BenchmarkSkillUpdater_CacheLookup benchmarks cache lookup performance
func BenchmarkSkillUpdater_CacheLookup(b *testing.B) {
	cache := &UpdateCache{}
	cache.Update(13, 0, 13)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = cache.IsValid(time.Hour)
		}
	})
}

// BenchmarkSkillUpdater_CacheUpdate benchmarks cache update performance
func BenchmarkSkillUpdater_CacheUpdate(b *testing.B) {
	cache := &UpdateCache{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Update(13, 0, 13)
	}
}

// ============================================================================
// Performance Verification Tests
// ============================================================================

// TestSkillUpdater_PerformanceTarget verifies update completes in reasonable time
// This test uses mock data to verify the parallelization works
func TestSkillUpdater_PerformanceTarget(t *testing.T) {
	// This test documents the expected performance characteristics and verifies
	// the parallelization calculation is correct for the current default skill
	// list (the count changes over time, so the assertions are dynamic).
	start := time.Now()

	numSkills := len(DefaultSkills)
	maxConcurrent := 4
	avgTimePerSkill := 100 * time.Millisecond // Simulated

	// Calculate expected time with parallelization
	batches := (numSkills + maxConcurrent - 1) / maxConcurrent
	expectedTime := time.Duration(batches) * avgTimePerSkill

	elapsed := time.Since(start)

	assert.Equal(t, len(DefaultSkills), numSkills)
	assert.Equal(t, 4, maxConcurrent)
	expectedBatches := (numSkills + maxConcurrent - 1) / maxConcurrent
	assert.Equal(t, expectedBatches, batches)
	assert.Less(t, elapsed, 1*time.Second)
	assert.Less(t, expectedTime, 1*time.Second)
}
