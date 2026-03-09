package core

import (
	"errors"
	"fmt"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestDAG_NewDAG(t *testing.T) {
	dag := NewDAG()
	if dag == nil {
		t.Fatal("NewDAG() returned nil")
	}
	if dag.nodes == nil {
		t.Error("DAG nodes map not initialized")
	}
}

func TestDAG_AddNode(t *testing.T) {
	dag := NewDAG()

	bead := types.Bead{
		ID:     types.BeadID("hydra-001"),
		Title:  "Test Bead",
		Status: types.StatusOpen,
	}

	err := dag.AddNode(bead)
	if err != nil {
		t.Errorf("AddNode() error = %v", err)
	}

	// Verify node was added
	node, exists := dag.GetNode("hydra-001")
	if !exists {
		t.Error("Node not found after AddNode")
	}
	if node.Bead.ID != bead.ID {
		t.Errorf("Node ID mismatch: got %v, want %v", node.Bead.ID, bead.ID)
	}

	// Test duplicate node
	err = dag.AddNode(bead)
	if !errors.Is(err, ErrNodeAlreadyExists) {
		t.Errorf("AddNode() duplicate error = %v, want ErrNodeAlreadyExists", err)
	}
}

func TestDAG_AddEdge(t *testing.T) {
	dag := NewDAG()

	// Add two nodes
	bead1 := types.Bead{ID: types.BeadID("hydra-001"), Status: types.StatusOpen}
	bead2 := types.Bead{ID: types.BeadID("hydra-002"), Status: types.StatusOpen}

	dag.AddNode(bead1)
	dag.AddNode(bead2)

	// Add edge: 002 depends on 001 (002 -> 001)
	err := dag.AddEdge("hydra-002", "hydra-001")
	if err != nil {
		t.Errorf("AddEdge() error = %v", err)
	}

	// Verify edge was added
	node2, _ := dag.GetNode("hydra-002")
	if node2.InDegree != 1 {
		t.Errorf("InDegree = %d, want 1", node2.InDegree)
	}

	node1, _ := dag.GetNode("hydra-001")
	if len(node1.OutEdges) != 1 || node1.OutEdges[0] != "hydra-002" {
		t.Errorf("OutEdges = %v, want [hydra-002]", node1.OutEdges)
	}

	// Test self-reference
	err = dag.AddEdge("hydra-001", "hydra-001")
	if !errors.Is(err, ErrSelfReference) {
		t.Errorf("AddEdge() self-reference error = %v, want ErrSelfReference", err)
	}

	// Test missing node
	err = dag.AddEdge("hydra-001", "hydra-999")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("AddEdge() missing node error = %v, want ErrNodeNotFound", err)
	}
}

func TestDAG_DetectCycle_NoCycle(t *testing.T) {
	dag := NewDAG()

	// Create: A <- B <- C (linear - no cycle)
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("C"), Status: types.StatusOpen})
	dag.AddEdge("B", "A") // B depends on A
	dag.AddEdge("C", "B") // C depends on B

	cycle, err := dag.DetectCycle()
	if err != nil {
		t.Errorf("Expected no cycle, got error: %v", err)
	}
	if len(cycle) > 0 {
		t.Errorf("Expected no cycle, got: %v", cycle)
	}
}

func TestDAG_DetectCycle_Diamond(t *testing.T) {
	dag := NewDAG()

	// Create diamond: A <- B <- D
	//                A <- C <- D
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("C"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("D"), Status: types.StatusOpen})
	dag.AddEdge("B", "A")
	dag.AddEdge("C", "A")
	dag.AddEdge("D", "B")
	dag.AddEdge("D", "C")

	cycle, err := dag.DetectCycle()
	if err != nil {
		t.Errorf("Expected no cycle for diamond, got error: %v", err)
	}
	if len(cycle) > 0 {
		t.Errorf("Expected no cycle for diamond, got: %v", cycle)
	}
}

func TestDAG_AddEdge_PreventsCycle(t *testing.T) {
	dag := NewDAG()

	// Create: A <- B
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddEdge("B", "A") // B depends on A - this is fine

	// Try to add A -> B (A depends on B) - this would create cycle A <-> B
	err := dag.AddEdge("A", "B")
	if !errors.Is(err, ErrEdgeCreatesCycle) {
		t.Errorf("Expected ErrEdgeCreatesCycle, got: %v", err)
	}

	// Verify graph still has no cycle
	cycle, err := dag.DetectCycle()
	if err != nil {
		t.Errorf("Unexpected cycle error: %v", err)
	}
	if len(cycle) > 0 {
		t.Errorf("Unexpected cycle: %v", cycle)
	}
}

func TestDAG_DetectCycle_LongerCycle(t *testing.T) {
	dag := NewDAG()

	// Create: A <- B <- C, then manually create C <- A using internal method
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("C"), Status: types.StatusOpen})

	// Add edges normally (these don't create cycles)
	dag.AddEdge("B", "A") // B depends on A
	dag.AddEdge("C", "B") // C depends on B

	// At this point: A <- B <- C, no cycle

	cycle, err := dag.DetectCycle()
	if err != nil {
		t.Errorf("Expected no cycle initially, got: %v", err)
	}
	if len(cycle) > 0 {
		t.Errorf("Expected no cycle initially, got: %v", cycle)
	}
}

func TestDAG_TopologicalSort(t *testing.T) {
	dag := NewDAG()

	// Create graph: A <- B <- C
	//                ^
	//                |
	//                D
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("C"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("D"), Status: types.StatusOpen})

	dag.AddEdge("B", "A")
	dag.AddEdge("C", "B")
	dag.AddEdge("D", "A")

	order, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort() error = %v", err)
	}

	// Verify all nodes present
	if len(order) != 4 {
		t.Errorf("Expected 4 nodes, got %d: %v", len(order), order)
	}

	// Verify A comes before B and D
	aIdx := -1
	bIdx := -1
	dIdx := -1
	for i, id := range order {
		switch id {
		case "A":
			aIdx = i
		case "B":
			bIdx = i
		case "D":
			dIdx = i
		}
	}

	if aIdx == -1 || bIdx == -1 || dIdx == -1 {
		t.Error("Not all nodes found in order")
	}

	if aIdx >= bIdx {
		t.Error("A should come before B")
	}
	if aIdx >= dIdx {
		t.Error("A should come before D")
	}
}

func TestDAG_GetReadyNodes(t *testing.T) {
	dag := NewDAG()

	// Create: A (ready), B (depends on A), C (ready)
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("C"), Status: types.StatusOpen})

	dag.AddEdge("B", "A")

	ready := dag.GetReadyNodes()
	if len(ready) != 2 {
		t.Errorf("Expected 2 ready nodes, got %d", len(ready))
	}

	// Verify A and C are ready, B is not
	hasA, hasB, hasC := false, false, false
	for _, node := range ready {
		switch node.Bead.ID.String() {
		case "A":
			hasA = true
		case "B":
			hasB = true
		case "C":
			hasC = true
		}
	}

	if !hasA {
		t.Error("A should be ready")
	}
	if hasB {
		t.Error("B should not be ready (depends on A)")
	}
	if !hasC {
		t.Error("C should be ready")
	}
}

func TestDAG_MarkCompleted(t *testing.T) {
	dag := NewDAG()

	// Create: A <- B
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddEdge("B", "A")

	// Initially B should not be ready
	ready1 := dag.GetReadyNodes()
	if len(ready1) != 1 || ready1[0].Bead.ID.String() != "A" {
		t.Error("Initially only A should be ready")
	}

	// Mark A completed
	newlyReady, err := dag.MarkCompleted("A")
	if err != nil {
		t.Errorf("MarkCompleted() error = %v", err)
	}

	// B should now be in newlyReady
	if len(newlyReady) != 1 || newlyReady[0] != "B" {
		t.Errorf("Expected B to be newly ready, got %v", newlyReady)
	}

	// Verify B is now ready
	ready2 := dag.GetReadyNodes()
	if len(ready2) != 1 || ready2[0].Bead.ID.String() != "B" {
		t.Errorf("B should now be ready, got %v", ready2)
	}
}

func TestDAG_MarkFailed(t *testing.T) {
	dag := NewDAG()

	// Create: A <- B
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddEdge("B", "A")

	// Mark A failed
	err := dag.MarkFailed("A")
	if err != nil {
		t.Errorf("MarkFailed() error = %v", err)
	}

	// Verify A is failed
	node, _ := dag.GetNode("A")
	if node.Status() != types.StatusFailed {
		t.Errorf("Expected status Failed, got %v", node.Status())
	}

	// B should still be blocked (not unblocked by failure)
	ready := dag.GetReadyNodes()
	for _, n := range ready {
		if n.Bead.ID.String() == "B" {
			t.Error("B should not be ready (A failed)")
		}
	}
}

func TestDAG_ThreadSafety(t *testing.T) {
	dag := NewDAG()

	// Concurrent adds
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			beadID := types.NewNumericBeadID("hydra", "", id)
			dag.AddNode(types.Bead{ID: beadID, Status: types.StatusOpen})
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all nodes added
	if dag.GetNodeCount() != 10 {
		t.Errorf("Expected 10 nodes, got %d", dag.GetNodeCount())
	}
}

func TestDAG_Validate(t *testing.T) {
	dag := NewDAG()

	// Valid DAG
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddEdge("B", "A")

	if err := dag.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}

	// Add cycle using internal method to bypass prevention
	dag.addEdgeInternal("A", "B")
	if err := dag.Validate(); err == nil {
		t.Error("Validate() should return error for cycle")
	}
}

func TestDAG_GetNodeCount(t *testing.T) {
	dag := NewDAG()

	if dag.GetNodeCount() != 0 {
		t.Errorf("Expected 0 nodes initially, got %d", dag.GetNodeCount())
	}

	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})

	if dag.GetNodeCount() != 2 {
		t.Errorf("Expected 2 nodes, got %d", dag.GetNodeCount())
	}
}

// =============================================================================
// Circular Dependency Detection Tests (RED Phase)
// These tests verify comprehensive cycle detection functionality
// =============================================================================

func TestDAG_DetectCycle_SimpleCycle(t *testing.T) {
	dag := NewDAG()

	// Create simple cycle: A <-> B (A depends on B, B depends on A)
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})

	// Add first edge: B depends on A (B -> A)
	dag.AddEdge("B", "A")

	// Force create cycle by adding edge A -> B using internal method
	// (since AddEdge would prevent it)
	dag.addEdgeInternal("A", "B")

	cycle, err := dag.DetectCycle()

	if err == nil {
		t.Fatal("Expected CycleError for simple cycle A <-> B, got nil")
	}

	if !IsCycleError(err) {
		t.Errorf("Expected error to be CycleError, got: %T", err)
	}

	if cycle == nil || len(cycle) == 0 {
		t.Fatal("Expected cycle path, got nil or empty")
	}

	// Verify cycle contains both A and B
	hasA, hasB := false, false
	for _, node := range cycle {
		if node == "A" {
			hasA = true
		}
		if node == "B" {
			hasB = true
		}
	}

	if !hasA {
		t.Error("Cycle should contain node A")
	}
	if !hasB {
		t.Error("Cycle should contain node B")
	}

	// Verify cycle is closed (first == last)
	if len(cycle) > 1 && cycle[0] != cycle[len(cycle)-1] {
		t.Errorf("Cycle should be closed (first == last), got: %v", cycle)
	}
}

func TestDAG_DetectCycle_LongCycle(t *testing.T) {
	dag := NewDAG()

	// Create cycle: A -> B -> C -> D -> A
	nodes := []string{"A", "B", "C", "D"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	// Create linear dependencies: B depends on A, C depends on B, D depends on C
	dag.AddEdge("B", "A")
	dag.AddEdge("C", "B")
	dag.AddEdge("D", "C")

	// Force create cycle: A depends on D
	dag.addEdgeInternal("A", "D")

	cycle, err := dag.DetectCycle()

	if err == nil {
		t.Fatal("Expected CycleError for cycle A -> B -> C -> D -> A, got nil")
	}

	if !IsCycleError(err) {
		t.Errorf("Expected error to be CycleError, got: %T", err)
	}

	if cycle == nil || len(cycle) < 4 {
		t.Fatalf("Expected cycle with at least 4 nodes, got: %v", cycle)
	}

	// Verify all nodes are in the cycle
	for _, node := range nodes {
		found := false
		for _, c := range cycle {
			if c == node {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Cycle should contain node %s, got: %v", node, cycle)
		}
	}
}

func TestDAG_DetectCycle_SelfReference(t *testing.T) {
	dag := NewDAG()

	// Self-reference: A depends on itself
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})

	// AddEdge should prevent self-reference
	err := dag.AddEdge("A", "A")
	if !errors.Is(err, ErrSelfReference) {
		t.Errorf("AddEdge() self-reference error = %v, want ErrSelfReference", err)
	}

	// Force create self-loop using internal method
	dag.addEdgeInternal("A", "A")

	cycle, err := dag.DetectCycle()

	if err == nil {
		t.Fatal("Expected CycleError for self-reference, got nil")
	}

	if !IsCycleError(err) {
		t.Errorf("Expected error to be CycleError, got: %T", err)
	}

	if cycle == nil || len(cycle) == 0 {
		t.Fatal("Expected cycle path for self-reference")
	}

	// Verify cycle contains A
	hasA := false
	for _, node := range cycle {
		if node == "A" {
			hasA = true
			break
		}
	}
	if !hasA {
		t.Errorf("Self-reference cycle should contain node A, got: %v", cycle)
	}
}

func TestDAG_DetectCycle_MultipleCycles(t *testing.T) {
	dag := NewDAG()

	// Create graph with two independent cycles:
	// Cycle 1: A <-> B
	// Cycle 2: C <-> D
	// Plus a non-cyclic path: E -> F
	nodes := []string{"A", "B", "C", "D", "E", "F"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	// Create first cycle: B -> A, then force A -> B
	dag.AddEdge("B", "A")
	dag.addEdgeInternal("A", "B")

	// Create second cycle: D -> C, then force C -> D
	dag.AddEdge("D", "C")
	dag.addEdgeInternal("C", "D")

	// Add non-cyclic path
	dag.AddEdge("F", "E")

	cycle, err := dag.DetectCycle()

	if err == nil {
		t.Fatal("Expected CycleError for graph with multiple cycles, got nil")
	}

	if !IsCycleError(err) {
		t.Errorf("Expected error to be CycleError, got: %T", err)
	}

	if cycle == nil || len(cycle) == 0 {
		t.Fatal("Expected at least one cycle to be detected")
	}

	// Verify at least one complete cycle is found
	// (Implementation may return any of the cycles)
	cycleStart := cycle[0]
	cycleEnd := cycle[len(cycle)-1]
	if cycleStart != cycleEnd {
		t.Errorf("Detected cycle should be closed, got: %v", cycle)
	}
}

func TestDAG_DetectCycle_EmptyGraph(t *testing.T) {
	dag := NewDAG()

	cycle, err := dag.DetectCycle()

	if err != nil {
		t.Errorf("Expected no error for empty graph, got: %v", err)
	}

	if cycle != nil && len(cycle) > 0 {
		t.Errorf("Expected no cycle in empty graph, got: %v", cycle)
	}
}

func TestDAG_DetectCycle_SingleNode(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})

	cycle, err := dag.DetectCycle()

	if err != nil {
		t.Errorf("Expected no error for single node without self-reference, got: %v", err)
	}

	if cycle != nil && len(cycle) > 0 {
		t.Errorf("Expected no cycle for single node, got: %v", cycle)
	}
}

func TestDAG_DetectCycle_ComplexGraph(t *testing.T) {
	dag := NewDAG()

	// Create complex graph:
	// A <- B <- C
	//      ^    |
	//      |    v
	//      D <- E
	// And add edge C -> B to create cycle
	nodes := []string{"A", "B", "C", "D", "E"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	// Normal dependencies
	dag.AddEdge("B", "A") // B depends on A
	dag.AddEdge("C", "B") // C depends on B
	dag.AddEdge("E", "D") // E depends on D
	dag.AddEdge("D", "B") // D depends on B

	// Create cycle: Make B depend on E, so the cycle is B <- D <- E <- B
	// When traversing from B: B enables D, D enables E, E enables B
	// This creates cycle: B -> D -> E -> B
	dag.addEdgeInternal("B", "E")

	cycle, err := dag.DetectCycle()

	if err == nil {
		t.Fatal("Expected CycleError for complex graph with cycle, got nil")
	}

	if !IsCycleError(err) {
		t.Errorf("Expected error to be CycleError, got: %T", err)
	}

	if cycle == nil || len(cycle) < 3 {
		t.Fatalf("Expected cycle with at least 3 nodes, got: %v", cycle)
	}
}

func TestCycleError_Message(t *testing.T) {
	// Test CycleError message formatting
	err := &CycleError{
		Path: []string{"A", "B", "C", "A"},
	}

	expected := "circular dependency detected: A → B → C → A"
	if err.Error() != expected {
		t.Errorf("CycleError message = %q, want %q", err.Error(), expected)
	}
}

func TestCycleError_EmptyPath(t *testing.T) {
	// Test CycleError with empty path
	err := &CycleError{
		Path: []string{},
	}

	expected := "circular dependency detected: empty path"
	if err.Error() != expected {
		t.Errorf("CycleError message = %q, want %q", err.Error(), expected)
	}
}

func TestIsCycleError_WithWrappedError(t *testing.T) {
	// Test IsCycleError with wrapped CycleError
	cycleErr := &CycleError{Path: []string{"A", "B", "A"}}
	wrappedErr := fmt.Errorf("operation failed: %w", cycleErr)

	if !IsCycleError(wrappedErr) {
		t.Error("IsCycleError should return true for wrapped CycleError")
	}
}

func TestIsCycleError_WithNonCycleError(t *testing.T) {
	// Test IsCycleError with non-CycleError
	normalErr := errors.New("some other error")

	if IsCycleError(normalErr) {
		t.Error("IsCycleError should return false for non-CycleError")
	}
}

func TestIsCycleError_WithNil(t *testing.T) {
	// Test IsCycleError with nil
	if IsCycleError(nil) {
		t.Error("IsCycleError should return false for nil error")
	}
}

func TestDAG_HasCycle_WithCycle(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})

	dag.AddEdge("B", "A")
	dag.addEdgeInternal("A", "B")

	if !dag.HasCycle() {
		t.Error("HasCycle() should return true when cycle exists")
	}
}

func TestDAG_HasCycle_WithoutCycle(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})

	dag.AddEdge("B", "A")

	if dag.HasCycle() {
		t.Error("HasCycle() should return false when no cycle exists")
	}
}

func TestDAG_HasCycle_EmptyGraph(t *testing.T) {
	dag := NewDAG()

	if dag.HasCycle() {
		t.Error("HasCycle() should return false for empty graph")
	}
}

func TestDAG_Validate_WithCycle(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})

	dag.AddEdge("B", "A")
	dag.addEdgeInternal("A", "B")

	err := dag.Validate()

	if err == nil {
		t.Fatal("Validate() should return error for cyclic graph")
	}

	if !IsCycleError(err) {
		t.Errorf("Validate() should return CycleError, got: %T", err)
	}
}

func TestDAG_TopologicalSort_WithCycle(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("C"), Status: types.StatusOpen})

	dag.AddEdge("B", "A")
	dag.AddEdge("C", "B")
	dag.addEdgeInternal("A", "C")

	_, err := dag.TopologicalSort()

	if err == nil {
		t.Fatal("TopologicalSort() should return error for cyclic graph")
	}

	if !IsCycleError(err) {
		t.Errorf("TopologicalSort() should return CycleError, got: %T", err)
	}
}

func TestDAG_TopologicalSortWithPriority_WithCycle(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen, Priority: 1})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen, Priority: 2})

	dag.AddEdge("B", "A")
	dag.addEdgeInternal("A", "B")

	_, err := dag.TopologicalSortWithPriority()

	if err == nil {
		t.Fatal("TopologicalSortWithPriority() should return error for cyclic graph")
	}

	if !IsCycleError(err) {
		t.Errorf("TopologicalSortWithPriority() should return CycleError, got: %T", err)
	}
}

func TestDAG_wouldCreateCycle_Prevention(t *testing.T) {
	dag := NewDAG()

	// Create: A <- B <- C
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("C"), Status: types.StatusOpen})

	dag.AddEdge("B", "A")
	dag.AddEdge("C", "B")

	// Try to add C -> A - should work (no cycle)
	err := dag.AddEdge("C", "A")
	if err != nil {
		t.Errorf("Adding edge C->A should succeed, got: %v", err)
	}

	// Try to add A -> C - should fail (creates cycle A -> C -> B -> A)
	err = dag.AddEdge("A", "C")
	if !errors.Is(err, ErrEdgeCreatesCycle) {
		t.Errorf("Adding edge A->C should fail with ErrEdgeCreatesCycle, got: %v", err)
	}
}

func TestDAG_CyclePath_Reconstruction(t *testing.T) {
	dag := NewDAG()

	// Create specific cycle: A -> B -> C -> A
	nodes := []string{"A", "B", "C"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	dag.AddEdge("B", "A")
	dag.AddEdge("C", "B")
	dag.addEdgeInternal("A", "C")

	cycle, err := dag.DetectCycle()

	if err == nil {
		t.Fatal("Expected cycle detection error")
	}

	if cycle == nil {
		t.Fatal("Expected cycle path")
	}

	// Verify cycle path format: should start and end with same node
	if len(cycle) < 2 {
		t.Fatalf("Cycle should have at least 2 elements, got: %v", cycle)
	}

	first := cycle[0]
	last := cycle[len(cycle)-1]
	if first != last {
		t.Errorf("Cycle should be closed (first == last): first=%s, last=%s, cycle=%v", first, last, cycle)
	}

	// Verify all cycle nodes are in the path
	for _, expected := range nodes {
		found := false
		for _, actual := range cycle {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected node %s in cycle path, got: %v", expected, cycle)
		}
	}
}
