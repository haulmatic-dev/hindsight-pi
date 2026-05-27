package core

import (
	"errors"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestDynamicUnblocking_InDegreeReduction tests that completing a bead reduces in-degree of dependents
func TestDynamicUnblocking_InDegreeReduction(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads with dependencies: A <- B, A <- C
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Verify initial state
	nodeB, _ := test.overseer.dag.GetNode("test-project-B")
	nodeC, _ := test.overseer.dag.GetNode("test-project-C")
	if nodeB.InDegree != 1 {
		t.Errorf("Expected B InDegree=1, got %d", nodeB.InDegree)
	}
	if nodeC.InDegree != 1 {
		t.Errorf("Expected C InDegree=1, got %d", nodeC.InDegree)
	}

	// Mark A as completed
	newlyReady, err := test.overseer.dag.MarkCompleted("test-project-A")
	if err != nil {
		t.Fatalf("MarkCompleted(A) error = %v", err)
	}

	// Verify in-degree reduction
	nodeB, _ = test.overseer.dag.GetNode("test-project-B")
	nodeC, _ = test.overseer.dag.GetNode("test-project-C")
	if nodeB.InDegree != 0 {
		t.Errorf("Expected B InDegree=0 after A completed, got %d", nodeB.InDegree)
	}
	if nodeC.InDegree != 0 {
		t.Errorf("Expected C InDegree=0 after A completed, got %d", nodeC.InDegree)
	}

	// Verify newly ready list
	if len(newlyReady) != 2 {
		t.Errorf("Expected 2 newly ready beads, got %d: %v", len(newlyReady), newlyReady)
	}
}

// TestDynamicUnblocking_ChainUnblocking tests chain dependency unblocking (A -> B -> C)
func TestDynamicUnblocking_ChainUnblocking(t *testing.T) {
	test := setupTestOverseer(t)

	// Create linear chain: A <- B <- C
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-B"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Mark A as completed - only B should become ready
	newlyReady, err := test.overseer.dag.MarkCompleted("test-project-A")
	if err != nil {
		t.Fatalf("MarkCompleted(A) error = %v", err)
	}

	if len(newlyReady) != 1 || newlyReady[0] != "test-project-B" {
		t.Errorf("Expected only B to be newly ready, got %v", newlyReady)
	}

	// Verify C is still blocked
	nodeC, _ := test.overseer.dag.GetNode("test-project-C")
	if nodeC.InDegree != 1 {
		t.Errorf("Expected C InDegree=1 (still blocked by B), got %d", nodeC.InDegree)
	}

	// Mark B as completed - C should now become ready
	newlyReady, err = test.overseer.dag.MarkCompleted("test-project-B")
	if err != nil {
		t.Fatalf("MarkCompleted(B) error = %v", err)
	}

	if len(newlyReady) != 1 || newlyReady[0] != "test-project-C" {
		t.Errorf("Expected only C to be newly ready after B completed, got %v", newlyReady)
	}

	// Verify C is now unblocked
	nodeC, _ = test.overseer.dag.GetNode("test-project-C")
	if nodeC.InDegree != 0 {
		t.Errorf("Expected C InDegree=0 after B completed, got %d", nodeC.InDegree)
	}
}

// TestDynamicUnblocking_DiamondPattern tests diamond dependency pattern unblocking
func TestDynamicUnblocking_DiamondPattern(t *testing.T) {
	test := setupTestOverseer(t)

	// Create diamond: A <- B <- D and A <- C <- D
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-D", Title: "Bead D", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-B", "test-project-C"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Mark A as completed - B and C should become ready, but not D
	newlyReady, err := test.overseer.dag.MarkCompleted("test-project-A")
	if err != nil {
		t.Fatalf("MarkCompleted(A) error = %v", err)
	}

	if len(newlyReady) != 2 {
		t.Errorf("Expected 2 newly ready beads (B and C), got %d: %v", len(newlyReady), newlyReady)
	}

	// Verify D is still blocked (needs both B and C)
	nodeD, _ := test.overseer.dag.GetNode("test-project-D")
	if nodeD.InDegree != 2 {
		t.Errorf("Expected D InDegree=2 (still blocked), got %d", nodeD.InDegree)
	}

	// Mark B as completed - D should still be blocked by C
	newlyReady, err = test.overseer.dag.MarkCompleted("test-project-B")
	if err != nil {
		t.Fatalf("MarkCompleted(B) error = %v", err)
	}

	if len(newlyReady) != 0 {
		t.Errorf("Expected 0 newly ready beads (D still blocked by C), got %v", newlyReady)
	}

	nodeD, _ = test.overseer.dag.GetNode("test-project-D")
	if nodeD.InDegree != 1 {
		t.Errorf("Expected D InDegree=1 (still blocked by C), got %d", nodeD.InDegree)
	}

	// Mark C as completed - D should now become ready
	newlyReady, err = test.overseer.dag.MarkCompleted("test-project-C")
	if err != nil {
		t.Fatalf("MarkCompleted(C) error = %v", err)
	}

	if len(newlyReady) != 1 || newlyReady[0] != "test-project-D" {
		t.Errorf("Expected D to be newly ready after C completed, got %v", newlyReady)
	}
}

// TestDynamicUnblocking_FailureDoesNotUnblock tests that failure doesn't unblock dependents
func TestDynamicUnblocking_FailureDoesNotUnblock(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads: A <- B
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Mark A as failed
	err := test.overseer.dag.MarkFailed("test-project-A")
	if err != nil {
		t.Fatalf("MarkFailed(A) error = %v", err)
	}

	// Verify A is marked as failed
	nodeA, _ := test.overseer.dag.GetNode("test-project-A")
	if nodeA.Status() != types.StatusFailed {
		t.Errorf("Expected A status to be failed, got %v", nodeA.Status())
	}

	// Verify B is still blocked
	nodeB, _ := test.overseer.dag.GetNode("test-project-B")
	if nodeB.InDegree != 1 {
		t.Errorf("Expected B InDegree=1 (still blocked after A failed), got %d", nodeB.InDegree)
	}

	// Verify B is not in ready list
	ready := test.overseer.GetReadyBeadsFromDAG()
	for _, bead := range ready {
		if bead.ID == "test-project-B" {
			t.Error("B should not be ready after A failed")
		}
	}
}

// TestDynamicUnblocking_ImmediateSchedulingTrigger tests that completion triggers immediate scheduling
func TestDynamicUnblocking_ImmediateSchedulingTrigger(t *testing.T) {
	test := setupTestOverseer(t)
	test.overseer.SetMaxWorkers(5)

	// Create beads with dependencies: A <- B
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project", Type: "implement"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Type: "implement", Dependencies: []types.BeadID{"test-project-A"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Verify only A is ready initially
	ready := test.overseer.GetReadyBeadsFromDAG()
	if len(ready) != 1 || ready[0].ID != "test-project-A" {
		t.Errorf("Expected only A to be ready initially, got %v", ready)
	}

	// Mark A as completed
	_, err := test.overseer.dag.MarkCompleted("test-project-A")
	if err != nil {
		t.Fatalf("MarkCompleted(A) error = %v", err)
	}

	// Verify B is now ready (unblocked)
	ready = test.overseer.GetReadyBeadsFromDAG()
	if len(ready) != 1 || ready[0].ID != "test-project-B" {
		t.Errorf("Expected B to be ready after A completed, got %v", ready)
	}
}

// TestDynamicUnblocking_RetryFailedBead tests retry logic for failed beads
func TestDynamicUnblocking_RetryFailedBead(t *testing.T) {
	test := setupTestOverseer(t)

	beadID := "test-project-retry"

	// Before recording any retries, shouldRetry should return true
	shouldRetry := test.overseer.retryManager.ShouldRetry(beadID, test.overseer.config.Overseer.DefaultRetries)
	if !shouldRetry {
		t.Error("Expected shouldRetry=true when no retries recorded")
	}

	// Record a failure
	test.overseer.retryManager.RecordRetry(beadID, nil)

	// Check retry count
	count := test.overseer.retryManager.GetRetryCount(beadID)
	if count != 1 {
		t.Errorf("Expected retry count=1, got %d", count)
	}

	// After recording, shouldRetry should be false (cooldown)
	shouldRetry = test.overseer.retryManager.ShouldRetry(beadID, test.overseer.config.Overseer.DefaultRetries)
	if shouldRetry {
		t.Error("Expected shouldRetry=false due to cooldown after recording")
	}

	// Record more failures up to max retries
	for i := 0; i < 2; i++ {
		test.overseer.retryManager.RecordRetry(beadID, nil)
	}

	// After exceeding max retries, shouldRetry should be false
	shouldRetry = test.overseer.retryManager.ShouldRetry(beadID, test.overseer.config.Overseer.DefaultRetries)
	if shouldRetry {
		t.Error("Expected shouldRetry=false after max retries exhausted")
	}
}

// TestDynamicUnblocking_RetryCooldown tests retry cooldown period
func TestDynamicUnblocking_RetryCooldown(t *testing.T) {
	test := setupTestOverseer(t)

	beadID := "test-project-cooldown"

	// Record a failure with recent timestamp
	test.overseer.retryManager.RecordRetry(beadID, nil)

	// Immediately check - should be in cooldown
	shouldRetry := test.overseer.retryManager.ShouldRetry(beadID, test.overseer.config.Overseer.DefaultRetries)
	if shouldRetry {
		t.Error("Expected shouldRetry=false during cooldown period")
	}
}

// TestDynamicUnblocking_AtomicStateUpdate tests that DAG updates are atomic
func TestDynamicUnblocking_AtomicStateUpdate(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads with multiple dependencies
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A", "test-project-B"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Verify C is blocked by both A and B
	nodeC, _ := test.overseer.dag.GetNode("test-project-C")
	if nodeC.InDegree != 2 {
		t.Errorf("Expected C InDegree=2, got %d", nodeC.InDegree)
	}

	// Complete A - C should still be blocked by B
	newlyReady, _ := test.overseer.dag.MarkCompleted("test-project-A")
	if len(newlyReady) != 0 {
		t.Errorf("Expected 0 newly ready (C still blocked by B), got %v", newlyReady)
	}

	nodeC, _ = test.overseer.dag.GetNode("test-project-C")
	if nodeC.InDegree != 1 {
		t.Errorf("Expected C InDegree=1 after A completed, got %d", nodeC.InDegree)
	}

	// Complete B - C should now be ready
	newlyReady, _ = test.overseer.dag.MarkCompleted("test-project-B")
	if len(newlyReady) != 1 || newlyReady[0] != "test-project-C" {
		t.Errorf("Expected C to be newly ready after B completed, got %v", newlyReady)
	}

	nodeC, _ = test.overseer.dag.GetNode("test-project-C")
	if nodeC.InDegree != 0 {
		t.Errorf("Expected C InDegree=0 after B completed, got %d", nodeC.InDegree)
	}
}

// TestDynamicUnblocking_MultipleDependents tests unblocking multiple dependents simultaneously
func TestDynamicUnblocking_MultipleDependents(t *testing.T) {
	test := setupTestOverseer(t)

	// Create structure where A is dependency of B, C, D, E
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-D", Title: "Bead D", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-E", Title: "Bead E", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Mark A as completed
	newlyReady, err := test.overseer.dag.MarkCompleted("test-project-A")
	if err != nil {
		t.Fatalf("MarkCompleted(A) error = %v", err)
	}

	// All 4 dependents should be unblocked
	if len(newlyReady) != 4 {
		t.Errorf("Expected 4 newly ready beads, got %d: %v", len(newlyReady), newlyReady)
	}

	// Verify all have InDegree=0
	for _, id := range []string{"test-project-B", "test-project-C", "test-project-D", "test-project-E"} {
		node, _ := test.overseer.dag.GetNode(id)
		if node.InDegree != 0 {
			t.Errorf("Expected %s InDegree=0, got %d", id, node.InDegree)
		}
	}
}

// TestDynamicUnblocking_ComplexGraph tests a complex dependency graph
func TestDynamicUnblocking_ComplexGraph(t *testing.T) {
	test := setupTestOverseer(t)

	// Create complex graph:
	// A (root)
	// B depends on A
	// C depends on A
	// D depends on B and C
	// E depends on C
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-D", Title: "Bead D", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-B", "test-project-C"}},
		{ID: "test-project-E", Title: "Bead E", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-C"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Mark A as completed - B and C should be ready
	newlyReady, _ := test.overseer.dag.MarkCompleted("test-project-A")
	if len(newlyReady) != 2 {
		t.Errorf("Expected 2 newly ready beads (B, C), got %d: %v", len(newlyReady), newlyReady)
	}

	// D and E should still be blocked
	nodeD, _ := test.overseer.dag.GetNode("test-project-D")
	nodeE, _ := test.overseer.dag.GetNode("test-project-E")
	if nodeD.InDegree != 2 {
		t.Errorf("Expected D InDegree=2, got %d", nodeD.InDegree)
	}
	if nodeE.InDegree != 1 {
		t.Errorf("Expected E InDegree=1, got %d", nodeE.InDegree)
	}

	// Mark B as completed - D should still be blocked by C
	newlyReady, _ = test.overseer.dag.MarkCompleted("test-project-B")
	if len(newlyReady) != 0 {
		t.Errorf("Expected 0 newly ready beads (D still blocked by C), got %v", newlyReady)
	}

	// Mark C as completed - D and E should both be ready
	newlyReady, _ = test.overseer.dag.MarkCompleted("test-project-C")
	if len(newlyReady) != 2 {
		t.Errorf("Expected 2 newly ready beads (D, E), got %d: %v", len(newlyReady), newlyReady)
	}
}

// TestDynamicUnblocking_ClearRetryAfterSuccess tests that retry state is cleared on success
func TestDynamicUnblocking_ClearRetryAfterSuccess(t *testing.T) {
	test := setupTestOverseer(t)

	beadID := "test-project-clear"

	// Record some retries
	test.overseer.retryManager.RecordRetry(beadID, nil)
	test.overseer.retryManager.RecordRetry(beadID, nil)

	// Verify retry count
	count := test.overseer.retryManager.GetRetryCount(beadID)
	if count != 2 {
		t.Errorf("Expected retry count=2, got %d", count)
	}

	// Clear retry state (simulating success)
	test.overseer.retryManager.ClearRetry(beadID)

	// Verify retry state is cleared
	count = test.overseer.retryManager.GetRetryCount(beadID)
	if count != 0 {
		t.Errorf("Expected retry count=0 after clear, got %d", count)
	}

	// Should be able to retry again
	shouldRetry := test.overseer.retryManager.ShouldRetry(beadID, test.overseer.config.Overseer.DefaultRetries)
	if !shouldRetry {
		t.Error("Expected shouldRetry=true after clearing retry state")
	}
}

// TestDynamicUnblocking_GetRetryEligibleBeads tests the retry eligible beads filtering
func TestDynamicUnblocking_GetRetryEligibleBeads(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads
	beads := []types.Bead{
		{ID: "test-project-ready", Title: "Ready Bead", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-blocked", Title: "Blocked Bead", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-ready"}},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Get initial ready beads
	readyBeads := test.overseer.GetReadyBeadsFromDAG()
	if len(readyBeads) != 1 {
		t.Fatalf("Expected 1 ready bead, got %d", len(readyBeads))
	}

	// Add a failed bead that's eligible for retry (but wait for cooldown)
	failedBeadID := "test-project-failed"
	test.overseer.retryManager.RecordRetry(failedBeadID, errors.New("test failure"))
	// Manually set lastError to bypass cooldown (hack for test)
	test.overseer.retryManager.retries[failedBeadID].lastError = time.Now().Add(-1 * time.Hour)

	// Get retry eligible beads
	retryEligible := test.overseer.getRetryEligibleBeads(readyBeads)

	// Should include the original ready bead
	foundReady := false
	for _, bead := range retryEligible {
		if bead.ID == "test-project-ready" {
			foundReady = true
			break
		}
	}
	if !foundReady {
		t.Error("Expected ready bead to be in retry eligible list")
	}
}

// TestDynamicUnblocking_StatusTransitions tests valid and invalid status transitions
func TestDynamicUnblocking_StatusTransitions(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Test: Cannot mark already completed bead as completed again
	test.overseer.dag.MarkCompleted("test-project-A")
	_, err := test.overseer.dag.MarkCompleted("test-project-A")
	if err == nil {
		t.Error("Expected error when marking already completed bead as completed")
	}

	// Reset for next test
	test.overseer.dag = NewDAG()
	test.overseer.buildDAG()

	// Test: Cannot mark failed bead as completed
	test.overseer.dag.MarkFailed("test-project-A")
	_, err = test.overseer.dag.MarkCompleted("test-project-A")
	if err == nil {
		t.Error("Expected error when marking failed bead as completed")
	}

	// Reset for next test
	test.overseer.dag = NewDAG()
	test.overseer.buildDAG()

	// Test: Cannot mark completed bead as failed
	test.overseer.dag.MarkCompleted("test-project-A")
	err = test.overseer.dag.MarkFailed("test-project-A")
	if err == nil {
		t.Error("Expected error when marking completed bead as failed")
	}
}

// TestDynamicUnblocking_ConcurrentCompletion tests concurrent bead completions
func TestDynamicUnblocking_ConcurrentCompletion(t *testing.T) {
	test := setupTestOverseer(t)

	// Create independent beads
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project"},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Concurrently complete all beads
	done := make(chan bool, 3)
	for _, id := range []string{"test-project-A", "test-project-B", "test-project-C"} {
		go func(beadID string) {
			test.overseer.dag.MarkCompleted(beadID)
			done <- true
		}(id)
	}

	// Wait for all completions
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify all are completed
	for _, id := range []string{"test-project-A", "test-project-B", "test-project-C"} {
		node, _ := test.overseer.dag.GetNode(id)
		if node.Status() != types.StatusCompleted {
			t.Errorf("Expected %s status to be completed, got %v", id, node.Status())
		}
	}
}
