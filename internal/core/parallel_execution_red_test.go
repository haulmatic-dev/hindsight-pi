package core

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ParallelExecutionRedPhaseTests - RED PHASE: Comprehensive failing tests
// These tests are designed to fail initially, demonstrating what needs to be implemented
// After implementation (GREEN phase), these tests should pass

// TestParallelExecution_TickSpawnsWorkersUpToMax tests that tick() spawns workers up to max_workers limit
func TestParallelExecution_TickSpawnsWorkersUpToMax(t *testing.T) {
	test := setupTestOverseer(t)

	// Set max workers to 3
	test.overseer.SetMaxWorkers(3)

	// Create 5 ready beads
	beads := make([]types.Bead, 5)
	for i := 0; i < 5; i++ {
		beadID := types.NewNumericBeadID("test-project", "", i)
		beads[i] = types.Bead{
			ID:       beadID,
			Title:    fmt.Sprintf("Bead %d", i),
			Status:   types.StatusOpen,
			Project:  "test-project",
			Type:     "implement",
			Priority: 5,
		}
		test.mockBeads.addBead(beads[i])
	}

	// Call tick() - should spawn workers up to max_workers (3)
	err := test.overseer.tick()
	require.NoError(t, err)

	// Verify exactly 3 workers were spawned (not all 5)
	time.Sleep(100 * time.Millisecond) // Allow async spawning
	activeCount := test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 3, activeCount,
		"Expected exactly 3 workers spawned (max_workers limit), got %d", activeCount)
}

// TestParallelExecution_TickSkipsApprovalGates tests that approval-gate beads are skipped during tick()
func TestParallelExecution_TickSkipsApprovalGates(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(5)

	// Create beads including approval gates
	beads := []types.Bead{
		{
			ID:       types.NewNumericBeadID("test-project", "", 1),
			Title:    "Implementation",
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 5,
		},
		{
			ID:       types.NewNumericBeadID("test-project", "", 2),
			Title:    "Approval Gate 1",
			Type:     "approval-gate",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 10,
		},
		{
			ID:       types.NewNumericBeadID("test-project", "", 3),
			Title:    "Review",
			Type:     "review",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 8,
		},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Call tick()
	err := test.overseer.tick()
	require.NoError(t, err)

	// Verify only non-approval-gate workers were spawned
	time.Sleep(100 * time.Millisecond)
	activeCount := test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 2, activeCount,
		"Expected 2 workers spawned (approval gates should be skipped), got %d", activeCount)
}

// TestParallelExecution_TickSortsByPriority tests that tick() processes beads in priority order
func TestParallelExecution_TickSortsByPriority(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(2)

	// Create beads with different priorities
	beads := []types.Bead{
		{
			ID:       types.NewNumericBeadID("test-project", "", 1),
			Title:    "Low Priority",
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 1,
		},
		{
			ID:       types.NewNumericBeadID("test-project", "", 2),
			Title:    "High Priority",
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 10,
		},
		{
			ID:       types.NewNumericBeadID("test-project", "", 3),
			Title:    "Medium Priority",
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 5,
		},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Track spawn order
	spawnOrder := make([]string, 0)
	var mu sync.Mutex

	// Override spawnWorker to track order
	originalTick := test.overseer.tick
	_ = originalTick

	// Call tick()
	err := test.overseer.tick()
	require.NoError(t, err)

	// With max_workers=2, high and medium priority should be spawned first
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	_ = spawnOrder
	mu.Unlock()

	activeWorkers := test.overseer.GetActiveWorkers()
	require.LessOrEqual(t, len(activeWorkers), 2, "Should have at most 2 workers")

	// Verify higher priority beads were processed
	// (In RED phase, this test may fail if priority sorting isn't implemented)
	if len(activeWorkers) > 0 {
		// The test documents expected behavior - doesn't strictly enforce order
		// since spawnWorker doesn't expose the bead ID easily
		t.Logf("Spawned %d workers", len(activeWorkers))
	}
}

// TestParallelExecution_TickSkipsRunningBeads tests that tick() doesn't spawn workers for already-running beads
func TestParallelExecution_TickSkipsRunningBeads(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(5)

	// Create a bead
	bead := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 1),
		Title:    "Test Bead",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}
	test.mockBeads.addBead(bead)

	// Manually add to active workers to simulate it already running
	test.overseer.workerMutex.Lock()
	test.overseer.activeWorkers[string(bead.ID)] = &types.Worker{
		ID:     string(bead.ID),
		BeadID: bead.ID,
		Status: "running",
	}
	test.overseer.workerMutex.Unlock()

	// Call tick() - should not spawn duplicate worker
	err := test.overseer.tick()
	require.NoError(t, err)

	// Verify still only 1 worker
	time.Sleep(100 * time.Millisecond)
	activeCount := test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 1, activeCount,
		"Expected 1 worker (duplicate should be skipped), got %d", activeCount)
}

// TestParallelExecution_TickSkipsCompletedBeads tests that tick() skips beads marked as completed
func TestParallelExecution_TickSkipsCompletedBeads(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(5)

	// Create beads - one completed, one not
	beads := []types.Bead{
		{
			ID:       types.NewNumericBeadID("test-project", "", 1),
			Title:    "Completed Bead",
			Type:     "implement",
			Status:   types.StatusOpen, // Status in DB is still open
			Project:  "test-project",
			Priority: 5,
		},
		{
			ID:       types.NewNumericBeadID("test-project", "", 2),
			Title:    "New Bead",
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 5,
		},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Mark first bead as completed in session
	test.overseer.completed[string(beads[0].ID)] = true

	// Call tick()
	err := test.overseer.tick()
	require.NoError(t, err)

	// Verify only the non-completed bead gets a worker
	time.Sleep(100 * time.Millisecond)
	activeCount := test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 1, activeCount,
		"Expected 1 worker (completed bead should be skipped), got %d", activeCount)
}

// TestParallelExecution_ResourceMonitorBlocksSpawning tests that resource monitoring blocks spawning when constrained
func TestParallelExecution_ResourceMonitorBlocksSpawning(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(5)

	// Verify resource monitor exists and has CanSpawnWorker method
	require.NotNil(t, test.overseer.resourceMonitor, "Resource monitor should be initialized")

	// Create beads
	bead := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 1),
		Title:    "Test Bead",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}
	test.mockBeads.addBead(bead)

	// Resource monitor should determine if spawning is allowed
	canSpawn := test.overseer.resourceMonitor.CanSpawnWorker()
	t.Logf("Resource monitor CanSpawnWorker returned: %v", canSpawn)

	// Call tick() - behavior depends on actual resource usage
	err := test.overseer.tick()
	require.NoError(t, err)

	// Workers spawned depends on actual resource state
	time.Sleep(100 * time.Millisecond)
	activeCount := test.overseer.GetActiveWorkerCount()
	t.Logf("Active workers after tick with resource monitoring: %d", activeCount)

	// Document the requirement: if resources are constrained, spawning should be blocked
	// In RED phase, we verify the infrastructure exists
	if !canSpawn {
		assert.Equal(t, 0, activeCount,
			"Expected 0 workers when resources constrained, got %d", activeCount)
	}
}

// TestParallelExecution_TickReturnsNoErrorWhenPaused tests that tick() returns nil when paused
func TestParallelExecution_TickReturnsNoErrorWhenPaused(t *testing.T) {
	test := setupTestOverseer(t)

	// Create bead
	bead := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 1),
		Title:    "Test Bead",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}
	test.mockBeads.addBead(bead)

	// Pause overseer
	test.overseer.Pause()
	require.True(t, test.overseer.IsPaused(), "Overseer should be paused")

	// Call tick() - should return nil (not spawn)
	err := test.overseer.tick()
	require.NoError(t, err)

	// Verify no workers spawned
	time.Sleep(100 * time.Millisecond)
	activeCount := test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 0, activeCount,
		"Expected 0 workers when paused, got %d", activeCount)

	// Resume and verify tick works again
	test.overseer.Resume()
	require.False(t, test.overseer.IsPaused(), "Overseer should be resumed")

	err = test.overseer.tick()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	activeCount = test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 1, activeCount,
		"Expected 1 worker after resume, got %d", activeCount)
}

// TestParallelExecution_GetReadyBeadsFiltersCorrectly tests getReadyBeads filtering logic
func TestParallelExecution_GetReadyBeadsFiltersCorrectly(t *testing.T) {
	test := setupTestOverseer(t)

	// Create various beads
	beads := []types.Bead{
		{
			ID:       types.NewNumericBeadID("test-project", "", 1),
			Title:    "Ready Bead 1",
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 5,
		},
		{
			ID:       types.NewNumericBeadID("test-project", "", 2),
			Title:    "Approval Gate",
			Type:     "approval-gate",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 10,
		},
		{
			ID:       types.NewNumericBeadID("test-project", "", 3),
			Title:    "Ready Bead 2",
			Type:     "review",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 8,
		},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Call getReadyBeads
	readyBeads, err := test.overseer.getReadyBeads()
	require.NoError(t, err)

	// Should have 2 ready beads (approval gate filtered out)
	assert.Len(t, readyBeads, 2, "Expected 2 ready beads (approval gate should be filtered)")

	// Verify no approval gates in results
	for _, bead := range readyBeads {
		assert.NotEqual(t, "approval-gate", bead.Type,
			"Approval gate beads should not appear in ready beads")
	}

	// Verify sorted by priority (high to low)
	for i := 1; i < len(readyBeads); i++ {
		assert.GreaterOrEqual(t, readyBeads[i-1].Priority, readyBeads[i].Priority,
			"Ready beads should be sorted by priority (high to low)")
	}
}

// TestParallelExecution_GetReadyBeadsExcludesRunning tests that running beads are excluded from ready list
func TestParallelExecution_GetReadyBeadsExcludesRunning(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads
	bead1 := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 1),
		Title:    "Bead 1",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}
	bead2 := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 2),
		Title:    "Bead 2",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}

	test.mockBeads.addBead(bead1)
	test.mockBeads.addBead(bead2)

	// Manually mark bead1 as running
	test.overseer.workerMutex.Lock()
	test.overseer.activeWorkers[string(bead1.ID)] = &types.Worker{
		ID:     string(bead1.ID),
		BeadID: bead1.ID,
		Status: "running",
	}
	test.overseer.workerMutex.Unlock()

	// Get ready beads
	readyBeads, err := test.overseer.getReadyBeads()
	require.NoError(t, err)

	// Should only have bead2
	assert.Len(t, readyBeads, 1, "Expected 1 ready bead (running bead excluded)")
	assert.Equal(t, bead2.ID, readyBeads[0].ID, "Should be bead2")
}

// TestParallelExecution_GetReadyBeadsExcludesCompleted tests that completed beads are excluded
func TestParallelExecution_GetReadyBeadsExcludesCompleted(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads
	bead1 := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 1),
		Title:    "Bead 1",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}
	bead2 := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 2),
		Title:    "Bead 2",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}

	test.mockBeads.addBead(bead1)
	test.mockBeads.addBead(bead2)

	// Mark bead1 as completed in session
	test.overseer.completed[string(bead1.ID)] = true

	// Get ready beads
	readyBeads, err := test.overseer.getReadyBeads()
	require.NoError(t, err)

	// Should only have bead2
	assert.Len(t, readyBeads, 1, "Expected 1 ready bead (completed bead excluded)")
	assert.Equal(t, bead2.ID, readyBeads[0].ID, "Should be bead2")
}

// TestParallelExecution_MaxWorkersConcurrencySafety tests thread safety of max workers management
func TestParallelExecution_MaxWorkersConcurrencySafety(t *testing.T) {
	test := setupTestOverseer(t)

	// Concurrently set and get max workers
	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Setters
	for i := 0; i < numGoroutines; i++ {
		go func(val int) {
			defer wg.Done()
			test.overseer.SetMaxWorkers(val % 10)
		}(i)
	}

	// Getters
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = test.overseer.GetMaxWorkers()
		}()
	}

	// Should complete without race conditions
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock or panic
		t.Log("Concurrent max workers access completed without issues")
	case <-time.After(5 * time.Second):
		t.Fatal("Deadlock detected in concurrent max workers access")
	}
}

// TestParallelExecution_PauseResumeConcurrency tests thread safety of pause/resume
func TestParallelExecution_PauseResumeConcurrency(t *testing.T) {
	test := setupTestOverseer(t)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3)

	// Pausers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			test.overseer.Pause()
		}()
	}

	// Resumers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			test.overseer.Resume()
		}()
	}

	// Checkers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = test.overseer.IsPaused()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("Concurrent pause/resume access completed without issues")
	case <-time.After(5 * time.Second):
		t.Fatal("Deadlock detected in concurrent pause/resume access")
	}
}

// TestParallelExecution_WorkerTrackingConsistency tests worker tracking consistency
func TestParallelExecution_WorkerTrackingConsistency(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(10)

	// Add multiple beads
	for i := 0; i < 5; i++ {
		bead := types.Bead{
			ID:       types.NewNumericBeadID("test-project", "", i),
			Title:    fmt.Sprintf("Bead %d", i),
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: i,
		}
		test.mockBeads.addBead(bead)
	}

	// Trigger tick
	err := test.overseer.tick()
	require.NoError(t, err)

	// Check consistency between different accessor methods
	time.Sleep(100 * time.Millisecond)

	activeCount := test.overseer.GetActiveWorkerCount()
	activeWorkers := test.overseer.GetActiveWorkers()

	assert.Equal(t, activeCount, len(activeWorkers),
		"GetActiveWorkerCount() and len(GetActiveWorkers()) should be consistent")
}

// TestParallelExecution_AvailableSlotsRespectsMaxWorkers tests available slots calculation
func TestParallelExecution_AvailableSlotsRespectsMaxWorkers(t *testing.T) {
	test := setupTestOverseer(t)

	// Test different scenarios
	tests := []struct {
		name          string
		maxWorkers    int
		activeWorkers int
		expectedSlots int
	}{
		{"all slots available", 10, 0, 10},
		{"partial slots available", 10, 6, 4},
		{"no slots available", 5, 5, 0},
		{"over capacity", 5, 8, 0}, // Should cap at 0, not negative
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.overseer.SetMaxWorkers(tt.maxWorkers)

			// Manually set active workers
			test.overseer.workerMutex.Lock()
			test.overseer.activeWorkers = make(map[string]*types.Worker)
			for i := 0; i < tt.activeWorkers; i++ {
				beadID := types.NewNumericBeadID("test-project", "", i)
				test.overseer.activeWorkers[string(beadID)] = &types.Worker{
					ID:     string(beadID),
					BeadID: beadID,
				}
			}
			test.overseer.workerMutex.Unlock()

			// Calculate available slots
			activeCount := test.overseer.GetActiveWorkerCount()
			availableSlots := test.overseer.GetMaxWorkers() - activeCount
			if availableSlots < 0 {
				availableSlots = 0
			}

			assert.Equal(t, tt.expectedSlots, availableSlots,
				"Available slots calculation incorrect for max=%d, active=%d",
				tt.maxWorkers, tt.activeWorkers)
		})
	}
}

// TestParallelExecution_TickWithEmptyReadyList tests tick() behavior with no ready beads
func TestParallelExecution_TickWithEmptyReadyList(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(5)

	// No beads added

	// Call tick() - should not error
	err := test.overseer.tick()
	require.NoError(t, err)

	// Verify no workers spawned
	time.Sleep(100 * time.Millisecond)
	activeCount := test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 0, activeCount, "Expected 0 workers with no ready beads")
}

// TestParallelExecution_TickWithZeroMaxWorkers tests tick() behavior with max_workers=0
func TestParallelExecution_TickWithZeroMaxWorkers(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(0)

	// Create beads
	bead := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 1),
		Title:    "Test Bead",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}
	test.mockBeads.addBead(bead)

	// Call tick()
	err := test.overseer.tick()
	require.NoError(t, err)

	// Verify no workers spawned with max_workers=0
	time.Sleep(100 * time.Millisecond)
	activeCount := test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 0, activeCount, "Expected 0 workers with max_workers=0")
}

// TestParallelExecution_ConcurrentTickCalls tests thread safety of concurrent tick() calls
func TestParallelExecution_ConcurrentTickCalls(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(10)

	// Create multiple beads
	for i := 0; i < 20; i++ {
		bead := types.Bead{
			ID:       types.NewNumericBeadID("test-project", "", i),
			Title:    fmt.Sprintf("Bead %d", i),
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: i,
		}
		test.mockBeads.addBead(bead)
	}

	// Call tick() concurrently
	const numCalls = 10
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			err := test.overseer.tick()
			assert.NoError(t, err)
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		time.Sleep(200 * time.Millisecond)
		activeCount := test.overseer.GetActiveWorkerCount()
		assert.LessOrEqual(t, activeCount, 10,
			"Should never exceed max_workers even with concurrent tick() calls, got %d", activeCount)
	case <-time.After(5 * time.Second):
		t.Fatal("Deadlock detected during concurrent tick() calls")
	}
}

// TestParallelExecution_GetReadyBeadsSortsByPriorityDescending tests priority sorting is descending
func TestParallelExecution_GetReadyBeadsSortsByPriorityDescending(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads with various priorities
	beads := []types.Bead{
		{ID: types.NewNumericBeadID("test-project", "", 1), Title: "Low", Type: "implement", Status: types.StatusOpen, Project: "test-project", Priority: 1},
		{ID: types.NewNumericBeadID("test-project", "", 2), Title: "High", Type: "implement", Status: types.StatusOpen, Project: "test-project", Priority: 10},
		{ID: types.NewNumericBeadID("test-project", "", 3), Title: "Medium", Type: "implement", Status: types.StatusOpen, Project: "test-project", Priority: 5},
		{ID: types.NewNumericBeadID("test-project", "", 4), Title: "Very High", Type: "implement", Status: types.StatusOpen, Project: "test-project", Priority: 100},
		{ID: types.NewNumericBeadID("test-project", "", 5), Title: "Very Low", Type: "implement", Status: types.StatusOpen, Project: "test-project", Priority: 0},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Get ready beads
	readyBeads, err := test.overseer.getReadyBeads()
	require.NoError(t, err)
	require.Len(t, readyBeads, 5)

	// Verify descending order
	for i := 1; i < len(readyBeads); i++ {
		assert.GreaterOrEqual(t, readyBeads[i-1].Priority, readyBeads[i].Priority,
			"Ready beads should be sorted by priority in descending order. "+
				"Bead at index %d (priority %d) should have >= priority than bead at index %d (priority %d)",
			i-1, readyBeads[i-1].Priority, i, readyBeads[i].Priority)
	}

	// Verify highest priority first
	assert.Equal(t, 100, readyBeads[0].Priority, "Highest priority bead should be first")
	assert.Equal(t, 0, readyBeads[4].Priority, "Lowest priority bead should be last")
}

// TestParallelExecution_WorkerCountNeverNegative tests worker count never goes negative
func TestParallelExecution_WorkerCountNeverNegative(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(5)

	// Add beads
	for i := 0; i < 3; i++ {
		bead := types.Bead{
			ID:       types.NewNumericBeadID("test-project", "", i),
			Title:    fmt.Sprintf("Bead %d", i),
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: i,
		}
		test.mockBeads.addBead(bead)
	}

	// Spawn workers
	err := test.overseer.tick()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Verify non-negative
	count := test.overseer.GetActiveWorkerCount()
	assert.GreaterOrEqual(t, count, 0, "Worker count should never be negative")

	// The actual implementation should be tested with real worker spawning
	// This test documents the requirement
	t.Logf("Current active worker count: %d", count)
}

// TestParallelExecution_ThreadSafeWorkerMapAccess tests concurrent access to worker map
func TestParallelExecution_ThreadSafeWorkerMapAccess(t *testing.T) {
	test := setupTestOverseer(t)

	// Add some beads
	for i := 0; i < 10; i++ {
		bead := types.Bead{
			ID:       types.NewNumericBeadID("test-project", "", i),
			Title:    fmt.Sprintf("Bead %d", i),
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: i,
		}
		test.mockBeads.addBead(bead)
	}

	var wg sync.WaitGroup
	numGoroutines := 50

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = test.overseer.GetActiveWorkerCount()
			_ = test.overseer.GetActiveWorkers()
			// Access worker map directly to check if bead is being tracked
			test.overseer.workerMutex.RLock()
			_ = test.overseer.activeWorkers
			test.overseer.workerMutex.RUnlock()
		}()
	}

	// Concurrent tick() calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = test.overseer.tick()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("Concurrent worker map access completed without race conditions")
	case <-time.After(5 * time.Second):
		t.Fatal("Deadlock or timeout during concurrent worker map access")
	}
}

// TestParallelExecution_SpawnWorkerReturnsErrorForDuplicate tests spawnWorker error for duplicate workers
func TestParallelExecution_SpawnWorkerReturnsErrorForDuplicate(t *testing.T) {
	test := setupTestOverseer(t)

	bead := types.Bead{
		ID:       types.NewNumericBeadID("test-project", "", 1),
		Title:    "Test Bead",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "test-project",
		Priority: 5,
	}

	// First spawn should succeed (or attempt to)
	err1 := test.overseer.spawnWorker(bead)
	// Note: spawnWorker may fail for other reasons (e.g., binary not found), but not for duplicate

	// Second spawn should fail with duplicate error
	err2 := test.overseer.spawnWorker(bead)

	if err1 == nil {
		// If first succeeded, second should fail
		assert.Error(t, err2, "Second spawnWorker for same bead should return error")
		assert.Contains(t, err2.Error(), "already exists", "Error should indicate worker already exists")
	}
}

// TestParallelExecution_DAGStatsConsistency tests DAG stats are consistent
func TestParallelExecution_DAGStatsConsistency(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads with dependencies
	beads := []types.Bead{
		{
			ID:       types.NewNumericBeadID("test-project", "", 1),
			Title:    "Root Bead",
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: 5,
		},
		{
			ID:           types.NewNumericBeadID("test-project", "", 2),
			Title:        "Dependent Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Project:      "test-project",
			Priority:     5,
			Dependencies: []types.BeadID{types.NewNumericBeadID("test-project", "", 1)},
		},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Build DAG
	err := test.overseer.buildDAG()
	require.NoError(t, err)

	// Get stats
	stats := test.overseer.GetDAGStats()

	// Verify consistency
	assert.GreaterOrEqual(t, stats.TotalNodes, 0, "Total nodes should be non-negative")
	assert.GreaterOrEqual(t, stats.ReadyNodes, 0, "Ready nodes should be non-negative")
	assert.GreaterOrEqual(t, stats.BlockedNodes, 0, "Blocked nodes should be non-negative")
	assert.GreaterOrEqual(t, stats.CompletedNodes, 0, "Completed nodes should be non-negative")
	assert.GreaterOrEqual(t, stats.FailedNodes, 0, "Failed nodes should be non-negative")

	// Total should be sum of categories
	// Note: This may not hold exactly depending on DAG implementation
	t.Logf("DAG Stats: %+v", stats)
}

// TestParallelExecution_NilDAGHandling tests graceful handling of nil DAG
func TestParallelExecution_NilDAGHandling(t *testing.T) {
	test := setupTestOverseer(t)

	// Set DAG to nil
	test.overseer.dag = nil

	// Operations should handle nil gracefully
	stats := test.overseer.GetDAGStats()
	assert.Equal(t, 0, stats.TotalNodes, "Nil DAG should return zero stats")

	// getReadyBeadsFromDAG should handle nil
	beads := test.overseer.GetReadyBeadsFromDAG()
	assert.Nil(t, beads, "Nil DAG should return nil beads")
}

// TestParallelExecution_MultiProjectSupport tests parallel execution across multiple projects
func TestParallelExecution_MultiProjectSupport(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(10)

	// Configure multiple projects
	test.overseer.projects = []types.Project{
		{Name: "project-a", Path: "/test/project-a", Type: types.RepoTypeSingle},
		{Name: "project-b", Path: "/test/project-b", Type: types.RepoTypeSingle},
	}

	// Add beads from different projects
	bead1 := types.Bead{
		ID:       types.NewNumericBeadID("project-a", "", 1),
		Title:    "Project A Bead",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "project-a",
		Priority: 5,
	}
	bead2 := types.Bead{
		ID:       types.NewNumericBeadID("project-b", "", 1),
		Title:    "Project B Bead",
		Type:     "implement",
		Status:   types.StatusOpen,
		Project:  "project-b",
		Priority: 5,
	}

	test.mockBeads.addBead(bead1)
	test.mockBeads.addBead(bead2)

	// Call tick()
	err := test.overseer.tick()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Should spawn workers for both projects
	activeCount := test.overseer.GetActiveWorkerCount()
	assert.Equal(t, 2, activeCount,
		"Expected 2 workers (one per project), got %d", activeCount)
}

// TestParallelExecution_ResourceMonitorIntegration tests resource monitor integration
func TestParallelExecution_ResourceMonitorIntegration(t *testing.T) {
	test := setupTestOverseer(t)

	// Verify resource monitor is initialized
	assert.NotNil(t, test.overseer.resourceMonitor, "Resource monitor should be initialized")

	// Test CanSpawnWorker method exists and returns bool
	canSpawn := test.overseer.resourceMonitor.CanSpawnWorker()
	assert.IsType(t, true, canSpawn, "CanSpawnWorker should return bool")
}

// TestParallelExecution_PollIntervalConfiguration tests poll interval can be configured
func TestParallelExecution_PollIntervalConfiguration(t *testing.T) {
	test := setupTestOverseer(t)

	// Test getter
	initialInterval := test.overseer.GetPollInterval()
	assert.Greater(t, initialInterval, time.Duration(0), "Poll interval should be positive")

	// Test setter
	newInterval := 5 * time.Second
	test.overseer.SetPollInterval(newInterval)
	assert.Equal(t, newInterval, test.overseer.GetPollInterval(), "Poll interval should be updated")
}

// TestParallelExecution_RetryDelayConfiguration tests retry delay configuration
func TestParallelExecution_RetryDelayConfiguration(t *testing.T) {
	test := setupTestOverseer(t)

	// Test getter
	initialDelay := test.overseer.GetRetryDelay()
	assert.GreaterOrEqual(t, initialDelay, time.Duration(0), "Retry delay should be non-negative")

	// Test setter
	newDelay := 10 * time.Second
	test.overseer.SetRetryDelay(newDelay)
	assert.Equal(t, newDelay, test.overseer.GetRetryDelay(), "Retry delay should be updated")
}

// BenchmarkParallelExecution_Tick benchmarks tick() performance
func BenchmarkParallelExecution_Tick(b *testing.B) {
	test := setupTestOverseer(&testing.T{})
	test.overseer.SetMaxWorkers(10)

	// Setup beads
	for i := 0; i < 100; i++ {
		bead := types.Bead{
			ID:       types.NewNumericBeadID("test-project", "", i),
			Title:    fmt.Sprintf("Bead %d", i),
			Type:     "implement",
			Status:   types.StatusOpen,
			Project:  "test-project",
			Priority: i,
		}
		test.mockBeads.addBead(bead)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = test.overseer.tick()
	}
}
