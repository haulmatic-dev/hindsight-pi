package core

import (
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// =============================================================================
// Dependency Resolution Integration Tests (RED Phase)
// These tests verify the complete integration between the beads client,
// DAG resolution, and overseer dependency management.
// =============================================================================

// TestDependencyResolution_BeadsClientToDAGFlow verifies the integration
// between the beads client (br ready) and DAG resolution
func TestDependencyResolution_BeadsClientToDAGFlow(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Setup beads with dependencies
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Base Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-002"),
			Title:        "Dependent Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"},
		},
		{
			ID:           types.BeadID("hydra-003"),
			Title:        "Deep Dependent",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-002"},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	// Initially only hydra-001 should be ready
	readyBeads, err := mockClient.GetReadyBeads("test-project", "/test/project", "test-repo")
	if err != nil {
		t.Fatalf("GetReadyBeads() error = %v", err)
	}

	if len(readyBeads) != 1 {
		t.Errorf("Expected 1 ready bead, got %d", len(readyBeads))
	}

	if len(readyBeads) > 0 && readyBeads[0].ID != "hydra-001" {
		t.Errorf("Expected hydra-001 to be ready, got %v", readyBeads[0].ID)
	}

	// Build DAG and verify
	builder := NewDependencyGraph()
	err = builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	dagReady := builder.GetReadyBeads()
	if len(dagReady) != 1 || dagReady[0].ID != "hydra-001" {
		t.Error("DAG ready beads should match beads client")
	}
}

// TestDependencyResolution_CrossProjectDependencies verifies dependencies
// spanning multiple projects are resolved correctly
func TestDependencyResolution_CrossProjectDependencies(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Setup beads across projects
	beads := []types.Bead{
		{
			ID:           types.BeadID("proj-a-001"),
			Title:        "Project A Base",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
			Project:      "project-a",
		},
		{
			ID:           types.BeadID("proj-a-002"),
			Title:        "Project A Dependent",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"proj-a-001"},
			Project:      "project-a",
		},
		{
			ID:           types.BeadID("proj-b-001"),
			Title:        "Project B Base",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
			Project:      "project-b",
		},
		{
			ID:           types.BeadID("proj-b-002"),
			Title:        "Cross-Project Dependent",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"proj-a-002", "proj-b-001"},
			Project:      "project-b",
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Initial ready beads: proj-a-001 and proj-b-001 (no deps)
	ready := builder.GetReadyBeads()
	if len(ready) != 2 {
		t.Errorf("Expected 2 ready beads, got %d", len(ready))
	}

	// Mark proj-a-001 as completed
	newlyReady, err := builder.MarkCompleted("proj-a-001")
	if err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}

	// proj-a-002 should become ready
	if len(newlyReady) != 1 || newlyReady[0] != "proj-a-002" {
		t.Errorf("Expected proj-a-002 to become ready, got %v", newlyReady)
	}

	// Mark proj-a-002 as completed
	newlyReady, err = builder.MarkCompleted("proj-a-002")
	if err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}

	// proj-b-002 should NOT be ready yet (still waiting for proj-b-001)
	if len(newlyReady) != 0 {
		t.Errorf("Expected 0 newly ready, proj-b-002 still blocked by proj-b-001, got %v", newlyReady)
	}

	// Mark proj-b-001 as completed
	newlyReady, err = builder.MarkCompleted("proj-b-001")
	if err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}

	// NOW proj-b-002 should become ready
	if len(newlyReady) != 1 || newlyReady[0] != "proj-b-002" {
		t.Errorf("Expected proj-b-002 to become ready, got %v", newlyReady)
	}
}

// TestDependencyResolution_DiamondPattern verifies the diamond dependency
// pattern is resolved correctly
func TestDependencyResolution_DiamondPattern(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Diamond pattern:
	//       A
	//      / \
	//     B   C
	//      \ /
	//       D
	beads := []types.Bead{
		{
			ID:           types.BeadID("diamond-a"),
			Title:        "Diamond Base",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("diamond-b"),
			Title:        "Left Branch",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"diamond-a"},
		},
		{
			ID:           types.BeadID("diamond-c"),
			Title:        "Right Branch",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"diamond-a"},
		},
		{
			ID:           types.BeadID("diamond-d"),
			Title:        "Merge Point",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"diamond-b", "diamond-c"},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Only A should be ready initially
	ready := builder.GetReadyBeads()
	if len(ready) != 1 || ready[0].ID != "diamond-a" {
		t.Errorf("Expected only diamond-a to be ready, got %v", ready)
	}

	// Mark A as completed
	newlyReady, _ := builder.MarkCompleted("diamond-a")

	// Both B and C should become ready
	if len(newlyReady) != 2 {
		t.Errorf("Expected 2 newly ready (B and C), got %d", len(newlyReady))
	}

	// Mark B as completed
	newlyReady, _ = builder.MarkCompleted("diamond-b")

	// D should NOT be ready yet (waiting for C)
	if len(newlyReady) != 0 {
		t.Errorf("Expected 0 newly ready, D still waiting for C, got %v", newlyReady)
	}

	// Mark C as completed
	newlyReady, _ = builder.MarkCompleted("diamond-c")

	// NOW D should become ready
	if len(newlyReady) != 1 || newlyReady[0] != "diamond-d" {
		t.Errorf("Expected diamond-d to become ready, got %v", newlyReady)
	}
}

// TestDependencyResolution_MultipleDependencyLevels verifies resolution
// with multiple levels of dependencies
func TestDependencyResolution_MultipleDependencyLevels(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Level 0: beads with no dependencies
	// Level 1: depends on Level 0
	// Level 2: depends on Level 1
	// Level 3: depends on Level 2
	beads := []types.Bead{
		// Level 0
		{ID: types.BeadID("l0-1"), Title: "Level 0 - 1", Status: types.StatusOpen, Dependencies: []types.BeadID{}},
		{ID: types.BeadID("l0-2"), Title: "Level 0 - 2", Status: types.StatusOpen, Dependencies: []types.BeadID{}},
		{ID: types.BeadID("l0-3"), Title: "Level 0 - 3", Status: types.StatusOpen, Dependencies: []types.BeadID{}},
		// Level 1
		{ID: types.BeadID("l1-1"), Title: "Level 1 - 1", Status: types.StatusOpen, Dependencies: []types.BeadID{"l0-1", "l0-2"}},
		{ID: types.BeadID("l1-2"), Title: "Level 1 - 2", Status: types.StatusOpen, Dependencies: []types.BeadID{"l0-2", "l0-3"}},
		// Level 2
		{ID: types.BeadID("l2-1"), Title: "Level 2 - 1", Status: types.StatusOpen, Dependencies: []types.BeadID{"l1-1"}},
		{ID: types.BeadID("l2-2"), Title: "Level 2 - 2", Status: types.StatusOpen, Dependencies: []types.BeadID{"l1-1", "l1-2"}},
		// Level 3
		{ID: types.BeadID("l3-1"), Title: "Level 3 - 1", Status: types.StatusOpen, Dependencies: []types.BeadID{"l2-1", "l2-2"}},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Initially 3 beads at Level 0 should be ready
	ready := builder.GetReadyBeads()
	if len(ready) != 3 {
		t.Errorf("Expected 3 ready beads at Level 0, got %d", len(ready))
	}

	// Complete all Level 0 beads
	builder.MarkCompleted("l0-1")
	builder.MarkCompleted("l0-2")
	newlyReady, _ := builder.MarkCompleted("l0-3")

	// Both Level 1 beads should become ready
	if len(newlyReady) != 2 {
		t.Errorf("Expected 2 Level 1 beads to become ready, got %v", newlyReady)
	}

	// Complete Level 1 beads
	builder.MarkCompleted("l1-1")
	newlyReady, _ = builder.MarkCompleted("l1-2")

	// Both Level 2 beads should become ready
	if len(newlyReady) != 2 {
		t.Errorf("Expected 2 Level 2 beads to become ready, got %v", newlyReady)
	}

	// Complete Level 2 beads
	builder.MarkCompleted("l2-1")
	newlyReady, _ = builder.MarkCompleted("l2-2")

	// Level 3 bead should become ready
	if len(newlyReady) != 1 || newlyReady[0] != "l3-1" {
		t.Errorf("Expected l3-1 to become ready, got %v", newlyReady)
	}
}

// TestDependencyResolution_MixedStatuses verifies resolution with beads
// in different statuses (open, running, completed, failed)
func TestDependencyResolution_MixedStatuses(t *testing.T) {
	mockClient := newMockBeadsClient()

	beads := []types.Bead{
		{
			ID:           types.BeadID("mixed-001"),
			Title:        "Completed Bead",
			Status:       types.StatusCompleted,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("mixed-002"),
			Title:        "Failed Bead",
			Status:       types.StatusFailed,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("mixed-003"),
			Title:        "Open Bead - No Deps",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("mixed-004"),
			Title:        "Depends on Completed",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"mixed-001"},
		},
		{
			ID:           types.BeadID("mixed-005"),
			Title:        "Depends on Failed",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"mixed-002"},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Ready beads should only be open beads with no deps
	// mixed-001 (completed) and mixed-002 (failed) are not "ready"
	// mixed-003 should be ready
	// mixed-004 and mixed-005 are blocked
	ready := builder.GetReadyBeads()
	if len(ready) != 1 || ready[0].ID != "mixed-003" {
		t.Errorf("Expected only mixed-003 to be ready, got %v", ready)
	}

	// Mark completed bead as completed in DAG
	newlyReady, _ := builder.MarkCompleted("mixed-001")

	// mixed-004 should become ready (depends on completed mixed-001)
	if len(newlyReady) != 1 || newlyReady[0] != "mixed-004" {
		t.Errorf("Expected mixed-004 to become ready, got %v", newlyReady)
	}

	// Mark failed bead as failed in DAG
	err = builder.MarkFailed("mixed-002")
	if err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}

	// mixed-005 should remain blocked (depends on failed bead)
	ready = builder.GetReadyBeads()
	for _, bead := range ready {
		if bead.ID == "mixed-005" {
			t.Error("mixed-005 should remain blocked when its dependency failed")
		}
	}
}

// TestDependencyResolution_ComplexGraph tests a complex graph with
// multiple independent chains and merge points
func TestDependencyResolution_ComplexGraph(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Complex graph:
	// A -> B -> C
	// D -> E -> C (C merges both chains)
	// F -> G
	// H (standalone)
	beads := []types.Bead{
		{ID: types.BeadID("c-A"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("c-B"), Dependencies: []types.BeadID{"c-A"}},
		{ID: types.BeadID("c-C"), Dependencies: []types.BeadID{"c-B", "c-E"}},
		{ID: types.BeadID("c-D"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("c-E"), Dependencies: []types.BeadID{"c-D"}},
		{ID: types.BeadID("c-F"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("c-G"), Dependencies: []types.BeadID{"c-F"}},
		{ID: types.BeadID("c-H"), Dependencies: []types.BeadID{}},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Initial ready: A, D, F, H
	ready := builder.GetReadyBeads()
	if len(ready) != 4 {
		t.Errorf("Expected 4 ready beads, got %d", len(ready))
	}

	// Complete A, D, F, H
	builder.MarkCompleted("c-A")
	builder.MarkCompleted("c-D")
	builder.MarkCompleted("c-F")
	newlyReady, _ := builder.MarkCompleted("c-H")

	// B, E, G should become ready (H has no dependents)
	if len(newlyReady) != 3 {
		t.Errorf("Expected 3 newly ready (B, E, G), got %d", len(newlyReady))
	}

	// Complete B
	newlyReady, _ = builder.MarkCompleted("c-B")

	// C should NOT be ready yet (waiting for E)
	if len(newlyReady) != 0 {
		t.Errorf("Expected C to remain blocked, got %v", newlyReady)
	}

	// Complete E
	newlyReady, _ = builder.MarkCompleted("c-E")

	// NOW C should become ready (both B and E completed)
	if len(newlyReady) != 1 || newlyReady[0] != "c-C" {
		t.Errorf("Expected c-C to become ready, got %v", newlyReady)
	}
}

// TestDependencyResolution_ConcurrentMarking verifies thread-safe
// marking of multiple beads as completed concurrently
func TestDependencyResolution_ConcurrentMarking(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Create many independent beads
	beads := make([]types.Bead, 100)
	for i := 0; i < 100; i++ {
		beads[i] = types.Bead{
			ID:           types.NewNumericBeadID("conc", "", i+1),
			Title:        "Concurrent Bead",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		}
	}

	// Add one bead that depends on all 100
	finalBead := types.Bead{
		ID:           types.BeadID("conc-final"),
		Title:        "Final Bead",
		Status:       types.StatusOpen,
		Dependencies: make([]types.BeadID, 100),
	}
	for i := 0; i < 100; i++ {
		finalBead.Dependencies[i] = beads[i].ID
	}
	beads = append(beads, finalBead)

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Mark all 100 beads as completed concurrently
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			beadID := types.NewNumericBeadID("conc", "", idx+1).ID()
			_, err := builder.MarkCompleted(beadID)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent MarkCompleted error: %v", err)
	}

	// Final bead should be ready now
	ready := builder.GetReadyBeads()
	foundFinal := false
	for _, bead := range ready {
		if bead.ID == "conc-final" {
			foundFinal = true
			break
		}
	}

	if !foundFinal {
		t.Error("conc-final should be ready after all dependencies completed")
	}
}

// TestDependencyResolution_BeadsClientIntegration verifies the actual
// integration with the beads client GetReadyBeads method
func TestDependencyResolution_BeadsClientIntegration(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Setup beads where some are ready and some are blocked
	beads := []types.Bead{
		{
			ID:           types.BeadID("bci-001"),
			Title:        "Ready Bead 1",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("bci-002"),
			Title:        "Ready Bead 2",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("bci-003"),
			Title:        "Blocked Bead",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"bci-001"},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	// Test GetReadyBeads through the client
	ready, err := mockClient.GetReadyBeads("test-project", "/test/project", "test-repo")
	if err != nil {
		t.Fatalf("GetReadyBeads() error = %v", err)
	}

	if len(ready) != 2 {
		t.Errorf("Expected 2 ready beads from client, got %d", len(ready))
	}

	// Mark one as completed through client
	err = mockClient.CloseBead("/test/project", "test-repo", types.BeadID("bci-001"), "completed")
	if err != nil {
		t.Fatalf("CloseBead() error = %v", err)
	}

	// Update the bead status in mock
	bead := mockClient.beads["bci-001"]
	bead.Status = types.StatusClosed
	mockClient.beads["bci-001"] = bead

	// bci-003 should now be ready
	ready, err = mockClient.GetReadyBeads("test-project", "/test/project", "test-repo")
	if err != nil {
		t.Fatalf("GetReadyBeads() error = %v", err)
	}

	if len(ready) != 2 {
		t.Errorf("Expected 2 ready beads (bci-002 and bci-003), got %d", len(ready))
	}
}

// TestDependencyResolution_PartialCompletion verifies behavior when
// only some dependencies are completed
func TestDependencyResolution_PartialCompletion(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Bead depends on A, B, and C
	beads := []types.Bead{
		{ID: types.BeadID("p-A"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("p-B"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("p-C"), Dependencies: []types.BeadID{}},
		{
			ID:           types.BeadID("p-target"),
			Dependencies: []types.BeadID{"p-A", "p-B", "p-C"},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Mark only A as completed
	newlyReady, _ := builder.MarkCompleted("p-A")

	// Target should NOT be ready (still needs B and C)
	if len(newlyReady) != 0 {
		t.Errorf("Expected 0 newly ready, target still blocked, got %v", newlyReady)
	}

	// Mark B as completed
	newlyReady, _ = builder.MarkCompleted("p-B")

	// Target should still NOT be ready (still needs C)
	if len(newlyReady) != 0 {
		t.Errorf("Expected 0 newly ready, target still blocked, got %v", newlyReady)
	}

	// Mark C as completed
	newlyReady, _ = builder.MarkCompleted("p-C")

	// NOW target should become ready
	if len(newlyReady) != 1 || newlyReady[0] != "p-target" {
		t.Errorf("Expected p-target to become ready, got %v", newlyReady)
	}
}

// TestDependencyResolution_CycleDetection verifies cycle detection
// works correctly at the integration level
func TestDependencyResolution_CycleDetection(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Create a cycle: A -> B -> C -> A
	beads := []types.Bead{
		{
			ID:           types.BeadID("cyc-A"),
			Dependencies: []types.BeadID{"cyc-C"},
		},
		{
			ID:           types.BeadID("cyc-B"),
			Dependencies: []types.BeadID{"cyc-A"},
		},
		{
			ID:           types.BeadID("cyc-C"),
			Dependencies: []types.BeadID{"cyc-B"},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)

	if err == nil {
		t.Error("Expected error for cyclic dependency, got nil")
	}
}

// TestDependencyResolution_EmptyGraph verifies behavior with empty graph
func TestDependencyResolution_EmptyGraph(t *testing.T) {
	builder := NewDependencyGraph()
	err := builder.BuildFromBeads([]types.Bead{})
	if err != nil {
		t.Errorf("BuildFromBeads([]) error = %v", err)
	}

	ready := builder.GetReadyBeads()
	if len(ready) != 0 {
		t.Errorf("Expected 0 ready beads for empty graph, got %d", len(ready))
	}

	blocked := builder.GetBlockedBeads()
	if len(blocked) != 0 {
		t.Errorf("Expected 0 blocked beads for empty graph, got %d", len(blocked))
	}

	stats := builder.GetStats()
	if stats.TotalNodes != 0 {
		t.Errorf("Expected 0 total nodes, got %d", stats.TotalNodes)
	}
}

// TestDependencyResolution_SingleBead verifies behavior with single bead
func TestDependencyResolution_SingleBead(t *testing.T) {
	mockClient := newMockBeadsClient()

	beads := []types.Bead{
		{
			ID:           types.BeadID("single-001"),
			Title:        "Single Bead",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	ready := builder.GetReadyBeads()
	if len(ready) != 1 {
		t.Errorf("Expected 1 ready bead, got %d", len(ready))
	}

	newlyReady, err := builder.MarkCompleted("single-001")
	if err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}

	if len(newlyReady) != 0 {
		t.Errorf("Expected 0 newly ready after completing single bead, got %v", newlyReady)
	}

	// After completion, no beads should be ready
	ready = builder.GetReadyBeads()
	if len(ready) != 0 {
		t.Errorf("Expected 0 ready beads after completion, got %d", len(ready))
	}
}

// TestDependencyResolution_MissingDependencies verifies behavior when
// dependencies don't exist in the graph
func TestDependencyResolution_MissingDependencies(t *testing.T) {
	mockClient := newMockBeadsClient()

	beads := []types.Bead{
		{
			ID:           types.BeadID("missing-base"),
			Title:        "Base Bead",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("missing-dep"),
			Title:        "Bead with Missing Dependency",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"non-existent"},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() should handle missing dependencies gracefully, got: %v", err)
	}

	// missing-base should be ready
	// missing-dep should be blocked (depends on non-existent bead)
	ready := builder.GetReadyBeads()
	if len(ready) != 1 || ready[0].ID != "missing-base" {
		t.Errorf("Expected only missing-base to be ready, got %v", ready)
	}
}

// TestDependencyResolution_StatsAccuracy verifies that GetStats returns
// accurate counts at each stage
func TestDependencyResolution_StatsAccuracy(t *testing.T) {
	mockClient := newMockBeadsClient()

	beads := []types.Bead{
		{ID: types.BeadID("stat-1"), Status: types.StatusOpen, Dependencies: []types.BeadID{}},
		{ID: types.BeadID("stat-2"), Status: types.StatusOpen, Dependencies: []types.BeadID{}},
		{ID: types.BeadID("stat-3"), Status: types.StatusOpen, Dependencies: []types.BeadID{"stat-1"}},
		{ID: types.BeadID("stat-4"), Status: types.StatusOpen, Dependencies: []types.BeadID{"stat-2"}},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Initial stats
	stats := builder.GetStats()
	if stats.TotalNodes != 4 {
		t.Errorf("Expected TotalNodes=4, got %d", stats.TotalNodes)
	}
	if stats.ReadyNodes != 2 {
		t.Errorf("Expected ReadyNodes=2, got %d", stats.ReadyNodes)
	}
	if stats.BlockedNodes != 2 {
		t.Errorf("Expected BlockedNodes=2, got %d", stats.BlockedNodes)
	}

	// Mark stat-1 as completed
	builder.MarkCompleted("stat-1")

	stats = builder.GetStats()
	if stats.ReadyNodes != 2 {
		t.Errorf("Expected ReadyNodes=2 (stat-2 and stat-3), got %d", stats.ReadyNodes)
	}
	if stats.BlockedNodes != 1 {
		t.Errorf("Expected BlockedNodes=1 (stat-4), got %d", stats.BlockedNodes)
	}
	if stats.CompletedNodes != 1 {
		t.Errorf("Expected CompletedNodes=1, got %d", stats.CompletedNodes)
	}

	// Mark stat-2 as failed
	builder.MarkFailed("stat-2")

	stats = builder.GetStats()
	if stats.FailedNodes != 1 {
		t.Errorf("Expected FailedNodes=1, got %d", stats.FailedNodes)
	}
}

// TestDependencyResolution_TopologicalOrder verifies topological sorting
// works correctly with the dependency graph
func TestDependencyResolution_TopologicalOrder(t *testing.T) {
	mockClient := newMockBeadsClient()

	beads := []types.Bead{
		{ID: types.BeadID("topo-A"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("topo-B"), Dependencies: []types.BeadID{"topo-A"}},
		{ID: types.BeadID("topo-C"), Dependencies: []types.BeadID{"topo-A"}},
		{ID: types.BeadID("topo-D"), Dependencies: []types.BeadID{"topo-B", "topo-C"}},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	order, err := builder.GetTopologicalOrder()
	if err != nil {
		t.Fatalf("GetTopologicalOrder() error = %v", err)
	}

	if len(order) != 4 {
		t.Errorf("Expected 4 nodes in topological order, got %d", len(order))
	}

	// Verify order: A must come before B and C, B and C must come before D
	idxA, idxB, idxC, idxD := -1, -1, -1, -1
	for i, id := range order {
		switch id {
		case "topo-A":
			idxA = i
		case "topo-B":
			idxB = i
		case "topo-C":
			idxC = i
		case "topo-D":
			idxD = i
		}
	}

	if idxA == -1 || idxB == -1 || idxC == -1 || idxD == -1 {
		t.Error("Not all nodes found in topological order")
	}

	if idxA >= idxB || idxA >= idxC {
		t.Error("A should come before B and C")
	}

	if idxB >= idxD || idxC >= idxD {
		t.Error("B and C should come before D")
	}
}

// TestDependencyResolution_PerformanceLargeGraph tests performance
// with a large number of beads
func TestDependencyResolution_PerformanceLargeGraph(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Create 1000 beads in a complex pattern
	beads := make([]types.Bead, 1000)

	// First 500 have no dependencies
	for i := 0; i < 500; i++ {
		beads[i] = types.Bead{
			ID:           types.NewNumericBeadID("perf", "", i+1),
			Title:        "Performance Bead",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		}
	}

	// Next 500 depend on previous ones (fan-in pattern)
	for i := 500; i < 1000; i++ {
		depCount := (i - 500) % 10 // Each depends on up to 10 previous beads
		deps := make([]types.BeadID, 0, depCount)
		for j := 0; j < depCount && j < i-500; j++ {
			deps = append(deps, types.NewNumericBeadID("perf", "", j+1))
		}
		beads[i] = types.Bead{
			ID:           types.NewNumericBeadID("perf", "", i+1),
			Title:        "Performance Bead",
			Status:       types.StatusOpen,
			Dependencies: deps,
		}
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()

	start := time.Now()
	err := builder.BuildFromBeads(beads)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Should complete in reasonable time (less than 5 seconds)
	if duration > 5*time.Second {
		t.Errorf("BuildFromBeads() took too long: %v", duration)
	}

	// Verify all nodes are in the graph
	if builder.GetStats().TotalNodes != 1000 {
		t.Errorf("Expected 1000 nodes, got %d", builder.GetStats().TotalNodes)
	}

	// First 500 should be ready
	ready := builder.GetReadyBeads()
	if len(ready) < 500 {
		t.Errorf("Expected at least 500 ready beads, got %d", len(ready))
	}
}

// TestDependencyResolution_ResetAndRebuild verifies that Reset() clears
// the graph and allows rebuilding
func TestDependencyResolution_ResetAndRebuild(t *testing.T) {
	mockClient := newMockBeadsClient()

	beads1 := []types.Bead{
		{ID: types.BeadID("reset-A"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("reset-B"), Dependencies: []types.BeadID{"reset-A"}},
	}

	for _, bead := range beads1 {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads1)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if builder.GetStats().TotalNodes != 2 {
		t.Fatalf("Expected 2 nodes, got %d", builder.GetStats().TotalNodes)
	}

	// Reset the graph
	builder.Reset()

	if builder.GetStats().TotalNodes != 0 {
		t.Errorf("Expected 0 nodes after reset, got %d", builder.GetStats().TotalNodes)
	}

	// Build with different beads
	beads2 := []types.Bead{
		{ID: types.BeadID("reset-C"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("reset-D"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("reset-E"), Dependencies: []types.BeadID{"reset-C"}},
	}

	for _, bead := range beads2 {
		mockClient.beads[string(bead.ID)] = bead
	}

	err = builder.BuildFromBeads(beads2)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if builder.GetStats().TotalNodes != 3 {
		t.Errorf("Expected 3 nodes after rebuild, got %d", builder.GetStats().TotalNodes)
	}

	ready := builder.GetReadyBeads()
	if len(ready) != 2 {
		t.Errorf("Expected 2 ready beads (C and D), got %d", len(ready))
	}
}

// TestDependencyResolution_ConcurrentReadWrite verifies thread safety
// with concurrent reads and writes
func TestDependencyResolution_ConcurrentReadWrite(t *testing.T) {
	mockClient := newMockBeadsClient()

	beads := []types.Bead{
		{ID: types.BeadID("crw-1"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("crw-2"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("crw-3"), Dependencies: []types.BeadID{"crw-1"}},
		{ID: types.BeadID("crw-4"), Dependencies: []types.BeadID{"crw-2"}},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Run concurrent operations
	var wg sync.WaitGroup
	done := make(chan bool, 3)

	// Reader 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = builder.GetReadyBeads()
			_ = builder.GetBlockedBeads()
			_ = builder.GetStats()
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Reader 2
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = builder.GetReadyBeads()
			_, _ = builder.GetTopologicalOrder()
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		builder.MarkCompleted("crw-1")
		time.Sleep(10 * time.Millisecond)
		builder.MarkCompleted("crw-2")
		time.Sleep(10 * time.Millisecond)
		builder.MarkCompleted("crw-3")
		time.Sleep(10 * time.Millisecond)
		builder.MarkCompleted("crw-4")
		done <- true
	}()

	wg.Wait()
	close(done)

	// Collect done signals
	count := 0
	for range done {
		count++
	}

	if count != 3 {
		t.Errorf("Expected 3 goroutines to complete, got %d", count)
	}
}

// TestDependencyResolution_ValidateAfterBuild verifies Validate() works
// correctly after building the graph
func TestDependencyResolution_ValidateAfterBuild(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Valid graph
	validBeads := []types.Bead{
		{ID: types.BeadID("val-A"), Dependencies: []types.BeadID{}},
		{ID: types.BeadID("val-B"), Dependencies: []types.BeadID{"val-A"}},
	}

	for _, bead := range validBeads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(validBeads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if err := builder.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

// TestDependencyResolution_SelfReference detects self-referencing beads
func TestDependencyResolution_SelfReference(t *testing.T) {
	mockClient := newMockBeadsClient()

	beads := []types.Bead{
		{
			ID:           types.BeadID("self-ref"),
			Dependencies: []types.BeadID{"self-ref"},
		},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)

	if err == nil {
		t.Error("Expected error for self-reference, got nil")
	}
}

// TestDependencyResolution_DuplicateBeads handles duplicate beads gracefully
func TestDependencyResolution_DuplicateBeads(t *testing.T) {
	mockClient := newMockBeadsClient()

	// Same bead ID twice
	beads := []types.Bead{
		{ID: types.BeadID("dup-1"), Title: "First", Dependencies: []types.BeadID{}},
		{ID: types.BeadID("dup-1"), Title: "Second", Dependencies: []types.BeadID{}},
	}

	for _, bead := range beads {
		mockClient.beads[string(bead.ID)] = bead
	}

	builder := NewDependencyGraph()
	err := builder.BuildFromBeads(beads)

	// Should either error or handle gracefully
	if err != nil && builder.GetStats().TotalNodes != 1 {
		t.Errorf("Expected 1 node after handling duplicate, got %d", builder.GetStats().TotalNodes)
	}
}
