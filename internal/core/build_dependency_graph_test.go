package core

import (
	"errors"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestBuildDependencyGraph_NewBuilder(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	if builder == nil {
		t.Fatal("NewDependencyGraphBuilder() returned nil")
	}
	if builder.dag == nil {
		t.Error("DAG not initialized in builder")
	}
}

func TestBuildDependencyGraph_FromBeads_Empty(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	err := builder.BuildFromBeads([]types.Bead{})
	if err != nil {
		t.Errorf("BuildFromBeads([]) error = %v, want nil", err)
	}
	if builder.dag.GetNodeCount() != 0 {
		t.Errorf("Expected 0 nodes, got %d", builder.dag.GetNodeCount())
	}
}

func TestBuildDependencyGraph_FromBeads_SingleBead(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Single Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if builder.dag.GetNodeCount() != 1 {
		t.Errorf("Expected 1 node, got %d", builder.dag.GetNodeCount())
	}

	ready := builder.GetReadyBeads()
	if len(ready) != 1 {
		t.Errorf("Expected 1 ready bead, got %d", len(ready))
	}

	if len(ready) > 0 && ready[0].ID != "hydra-001" {
		t.Errorf("Expected bead hydra-001, got %v", ready[0].ID)
	}
}

func TestBuildDependencyGraph_FromBeads_WithDependencies(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "First Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-002"),
			Title:        "Second Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"},
		},
		{
			ID:           types.BeadID("hydra-003"),
			Title:        "Third Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-002"},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if builder.dag.GetNodeCount() != 3 {
		t.Errorf("Expected 3 nodes, got %d", builder.dag.GetNodeCount())
	}

	ready := builder.GetReadyBeads()
	if len(ready) != 1 {
		t.Errorf("Expected 1 ready bead, got %d", len(ready))
	}

	if len(ready) > 0 && ready[0].ID != "hydra-001" {
		t.Errorf("Expected bead hydra-001 to be ready, got %v", ready[0].ID)
	}

	// Check in-degree
	node1, _ := builder.dag.GetNode("hydra-001")
	if node1.InDegree != 0 {
		t.Errorf("Expected hydra-001 InDegree = 0, got %d", node1.InDegree)
	}

	node2, _ := builder.dag.GetNode("hydra-002")
	if node2.InDegree != 1 {
		t.Errorf("Expected hydra-002 InDegree = 1, got %d", node2.InDegree)
	}

	node3, _ := builder.dag.GetNode("hydra-003")
	if node3.InDegree != 1 {
		t.Errorf("Expected hydra-003 InDegree = 1, got %d", node3.InDegree)
	}
}

func TestBuildDependencyGraph_FromBeads_DiamondPattern(t *testing.T) {
	builder := NewDependencyGraphBuilder()
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
			Title:        "Left Branch",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"},
		},
		{
			ID:           types.BeadID("hydra-003"),
			Title:        "Right Branch",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"},
		},
		{
			ID:           types.BeadID("hydra-004"),
			Title:        "Merge Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-002", "hydra-003"},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if builder.dag.GetNodeCount() != 4 {
		t.Errorf("Expected 4 nodes, got %d", builder.dag.GetNodeCount())
	}

	ready := builder.GetReadyBeads()
	if len(ready) != 1 {
		t.Errorf("Expected 1 ready bead, got %d", len(ready))
	}

	// Check in-degrees for diamond pattern
	node4, _ := builder.dag.GetNode("hydra-004")
	if node4.InDegree != 2 {
		t.Errorf("Expected hydra-004 InDegree = 2 (both branches), got %d", node4.InDegree)
	}
}

func TestBuildDependencyGraph_FromBeads_WithCycle(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Bead A",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-003"}, // Depends on C
		},
		{
			ID:           types.BeadID("hydra-002"),
			Title:        "Bead B",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"}, // Depends on A
		},
		{
			ID:           types.BeadID("hydra-003"),
			Title:        "Bead C",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-002"}, // Depends on B (creates cycle)
		},
	}

	err := builder.BuildFromBeads(beads)
	if err == nil {
		t.Error("Expected error for cyclic dependency, got nil")
	}

	if !errors.Is(err, ErrCyclicDependency) {
		t.Errorf("Expected ErrCyclicDependency, got %v", err)
	}
}

func TestBuildDependencyGraph_FromBeads_SelfReference(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Self-referencing Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err == nil {
		t.Error("Expected error for self-reference, got nil")
	}
}

func TestBuildDependencyGraph_FromBeads_MissingDependency(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Valid Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-002"),
			Title:        "Bead with Missing Dependency",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-999"}, // Non-existent
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Errorf("BuildFromBeads() should handle missing dependencies gracefully, got error: %v", err)
	}

	// Should still have both nodes
	if builder.dag.GetNodeCount() != 2 {
		t.Errorf("Expected 2 nodes, got %d", builder.dag.GetNodeCount())
	}
}

func TestBuildDependencyGraph_GetBlockedBeads(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Ready Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-002"),
			Title:        "Blocked Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	blocked := builder.GetBlockedBeads()
	if len(blocked) != 1 {
		t.Errorf("Expected 1 blocked bead, got %d", len(blocked))
	}

	if len(blocked) > 0 && blocked[0].ID != "hydra-002" {
		t.Errorf("Expected hydra-002 to be blocked, got %v", blocked[0].ID)
	}
}

func TestBuildDependencyGraph_MarkCompleted(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "First Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-002"),
			Title:        "Second Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Initially only 001 is ready
	ready1 := builder.GetReadyBeads()
	if len(ready1) != 1 || ready1[0].ID != "hydra-001" {
		t.Errorf("Expected only hydra-001 to be ready initially")
	}

	// Mark 001 as completed
	newlyReady, err := builder.MarkCompleted("hydra-001")
	if err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}

	if len(newlyReady) != 1 || newlyReady[0] != "hydra-002" {
		t.Errorf("Expected hydra-002 to be newly ready, got %v", newlyReady)
	}

	// Now only 002 should be ready
	ready2 := builder.GetReadyBeads()
	if len(ready2) != 1 || ready2[0].ID != "hydra-002" {
		t.Errorf("Expected only hydra-002 to be ready after marking 001 complete")
	}
}

func TestBuildDependencyGraph_MarkFailed(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Failing Bead",
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
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Mark 001 as failed
	err = builder.MarkFailed("hydra-001")
	if err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}

	// 002 should remain blocked (not become ready)
	ready := builder.GetReadyBeads()
	for _, bead := range ready {
		if bead.ID == "hydra-002" {
			t.Error("hydra-002 should not be ready when its dependency failed")
		}
	}
}

func TestBuildDependencyGraph_Performance_1000Beads(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := make([]types.Bead, 1000)

	// Create 1000 beads in a linear chain
	for i := 0; i < 1000; i++ {
		beadID := types.NewNumericBeadID("hydra", "", i+1)
		var deps []types.BeadID
		if i > 0 {
			deps = []types.BeadID{types.NewNumericBeadID("hydra", "", i)}
		}
		beads[i] = types.Bead{
			ID:           beadID,
			Title:        "Bead " + beadID.ID(),
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: deps,
		}
	}

	start := time.Now()
	err := builder.BuildFromBeads(beads)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if builder.dag.GetNodeCount() != 1000 {
		t.Errorf("Expected 1000 nodes, got %d", builder.dag.GetNodeCount())
	}

	// Should complete in reasonable time (less than 5 seconds)
	if duration > 5*time.Second {
		t.Errorf("BuildFromBeads() took too long: %v", duration)
	}

	// Only the first bead should be ready
	ready := builder.GetReadyBeads()
	if len(ready) != 1 {
		t.Errorf("Expected 1 ready bead, got %d", len(ready))
	}
}

func TestBuildDependencyGraph_MultipleProjects(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("project1-001"),
			Title:        "Project 1 Bead 1",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
			Project:      "project1",
		},
		{
			ID:           types.BeadID("project1-002"),
			Title:        "Project 1 Bead 2",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"project1-001"},
			Project:      "project1",
		},
		{
			ID:           types.BeadID("project2-001"),
			Title:        "Project 2 Bead 1",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
			Project:      "project2",
		},
		{
			ID:           types.BeadID("project2-002"),
			Title:        "Project 2 Bead 2",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"project2-001", "project1-002"}, // Cross-project dependency
			Project:      "project2",
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if builder.dag.GetNodeCount() != 4 {
		t.Errorf("Expected 4 nodes, got %d", builder.dag.GetNodeCount())
	}

	ready := builder.GetReadyBeads()
	if len(ready) != 2 {
		t.Errorf("Expected 2 ready beads (project1-001 and project2-001), got %d", len(ready))
	}
}

func TestBuildDependencyGraph_Stats(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Completed Bead",
			Type:         "implement",
			Status:       types.StatusCompleted,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-002"),
			Title:        "Failed Bead",
			Type:         "implement",
			Status:       types.StatusFailed,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-003"),
			Title:        "Ready Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-004"),
			Title:        "Blocked Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-003"},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	stats := builder.GetStats()
	if stats.TotalNodes != 4 {
		t.Errorf("Expected TotalNodes = 4, got %d", stats.TotalNodes)
	}
	if stats.ReadyNodes != 1 {
		t.Errorf("Expected ReadyNodes = 1, got %d", stats.ReadyNodes)
	}
	if stats.BlockedNodes != 1 {
		t.Errorf("Expected BlockedNodes = 1, got %d", stats.BlockedNodes)
	}
	if stats.CompletedNodes != 1 {
		t.Errorf("Expected CompletedNodes = 1, got %d", stats.CompletedNodes)
	}
	if stats.FailedNodes != 1 {
		t.Errorf("Expected FailedNodes = 1, got %d", stats.FailedNodes)
	}
}

func TestBuildDependencyGraph_TopologicalOrder(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Base",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-002"),
			Title:        "Middle",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-001"},
		},
		{
			ID:           types.BeadID("hydra-003"),
			Title:        "Top",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{"hydra-002"},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	order, err := builder.GetTopologicalOrder()
	if err != nil {
		t.Fatalf("GetTopologicalOrder() error = %v", err)
	}

	if len(order) != 3 {
		t.Errorf("Expected 3 nodes in order, got %d", len(order))
	}

	// Verify order: 001 before 002, 002 before 003
	idx1, idx2, idx3 := -1, -1, -1
	for i, id := range order {
		switch id {
		case "hydra-001":
			idx1 = i
		case "hydra-002":
			idx2 = i
		case "hydra-003":
			idx3 = i
		}
	}

	if idx1 == -1 || idx2 == -1 || idx3 == -1 {
		t.Error("Not all nodes found in topological order")
	}

	if idx1 >= idx2 {
		t.Error("001 should come before 002")
	}
	if idx2 >= idx3 {
		t.Error("002 should come before 003")
	}
}

func TestBuildDependencyGraph_Validate(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Valid Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if err := builder.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestBuildDependencyGraph_ThreadSafety(t *testing.T) {
	builder := NewDependencyGraphBuilder()
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
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	// Concurrent reads
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_ = builder.GetReadyBeads()
			_ = builder.GetBlockedBeads()
			_ = builder.GetStats()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestBuildDependencyGraph_Reset(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Test Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err != nil {
		t.Fatalf("BuildFromBeads() error = %v", err)
	}

	if builder.dag.GetNodeCount() != 1 {
		t.Fatalf("Expected 1 node before reset")
	}

	builder.Reset()

	if builder.dag.GetNodeCount() != 0 {
		t.Errorf("Expected 0 nodes after reset, got %d", builder.dag.GetNodeCount())
	}
}

func TestBuildDependencyGraph_InvalidBeadID(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID(""),
			Title:        "Empty ID Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
	}

	err := builder.BuildFromBeads(beads)
	if err == nil {
		t.Error("Expected error for empty bead ID")
	}
}

func TestBuildDependencyGraph_DuplicateBeads(t *testing.T) {
	builder := NewDependencyGraphBuilder()
	beads := []types.Bead{
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Original Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
		{
			ID:           types.BeadID("hydra-001"),
			Title:        "Duplicate Bead",
			Type:         "implement",
			Status:       types.StatusOpen,
			Dependencies: []types.BeadID{},
		},
	}

	err := builder.BuildFromBeads(beads)
	// Should handle duplicate gracefully (skip or error, but not panic)
	if err != nil && !errors.Is(err, ErrNodeAlreadyExists) {
		t.Errorf("Expected ErrNodeAlreadyExists or nil, got %v", err)
	}

	if builder.dag.GetNodeCount() != 1 {
		t.Errorf("Expected 1 node after handling duplicate, got %d", builder.dag.GetNodeCount())
	}
}
