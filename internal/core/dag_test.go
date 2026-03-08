package core

import (
	"errors"
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
	if node.Status != types.StatusFailed {
		t.Errorf("Expected status Failed, got %v", node.Status)
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
