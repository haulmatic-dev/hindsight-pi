package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// =============================================================================
// Circular Dependencies Detection Tests (RED Phase)
// These tests verify comprehensive detection of ALL circular dependencies
// in a graph, not just the first cycle found.
// =============================================================================

// TestDetectCircularDependencies_SimpleCycle tests detection of a simple A <-> B cycle
func TestDetectCircularDependencies_SimpleCycle(t *testing.T) {
	dag := NewDAG()

	// Create simple cycle: A <-> B
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})

	// Add edges to create cycle using internal method
	dag.addEdgeInternal("A", "B")
	dag.addEdgeInternal("B", "A")

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected cycles to be detected, got nil")
	}

	if len(cycles) != 1 {
		t.Fatalf("Expected exactly 1 cycle, got %d", len(cycles))
	}

	// Verify the cycle contains A and B
	cycle := cycles[0]
	if len(cycle) < 3 {
		t.Errorf("Expected cycle with at least 3 nodes (A, B, A), got %d", len(cycle))
	}

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

	// Verify cycle is closed
	if cycle[0] != cycle[len(cycle)-1] {
		t.Errorf("Cycle should be closed (first == last), got: %v", cycle)
	}
}

// TestDetectCircularDependencies_LongCycle tests detection of a longer cycle
func TestDetectCircularDependencies_LongCycle(t *testing.T) {
	dag := NewDAG()

	// Create cycle: A -> B -> C -> D -> A
	nodes := []string{"A", "B", "C", "D"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	// Create linear chain
	dag.addEdgeInternal("B", "A")
	dag.addEdgeInternal("C", "B")
	dag.addEdgeInternal("D", "C")
	dag.addEdgeInternal("A", "D")

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected cycles to be detected, got nil")
	}

	if len(cycles) != 1 {
		t.Fatalf("Expected exactly 1 cycle, got %d", len(cycles))
	}

	// Verify all nodes are in the detected cycle
	cycle := cycles[0]
	for _, expectedNode := range nodes {
		found := false
		for _, actualNode := range cycle {
			if actualNode == expectedNode {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected node %s in cycle, got: %v", expectedNode, cycle)
		}
	}
}

// TestDetectCircularDependencies_MultipleCycles tests detection of multiple independent cycles
func TestDetectCircularDependencies_MultipleCycles(t *testing.T) {
	dag := NewDAG()

	// Create two independent cycles:
	// Cycle 1: A <-> B
	// Cycle 2: C <-> D
	nodes := []string{"A", "B", "C", "D"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	// First cycle
	dag.addEdgeInternal("A", "B")
	dag.addEdgeInternal("B", "A")

	// Second cycle
	dag.addEdgeInternal("C", "D")
	dag.addEdgeInternal("D", "C")

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected cycles to be detected, got nil")
	}

	if len(cycles) != 2 {
		t.Fatalf("Expected exactly 2 cycles, got %d", len(cycles))
	}

	// Verify each cycle contains the correct nodes
	cycle1HasA, cycle1HasB := false, false
	cycle2HasC, cycle2HasD := false, false

	for _, cycle := range cycles {
		for _, node := range cycle {
			if node == "A" {
				cycle1HasA = true
			}
			if node == "B" {
				cycle1HasB = true
			}
			if node == "C" {
				cycle2HasC = true
			}
			if node == "D" {
				cycle2HasD = true
			}
		}
	}

	if !cycle1HasA || !cycle1HasB {
		t.Error("First cycle should contain nodes A and B")
	}
	if !cycle2HasC || !cycle2HasD {
		t.Error("Second cycle should contain nodes C and D")
	}
}

// TestDetectCircularDependencies_SelfReference tests detection of self-referencing nodes
func TestDetectCircularDependencies_SelfReference(t *testing.T) {
	dag := NewDAG()

	// Create self-reference: A -> A
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.addEdgeInternal("A", "A")

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected cycles to be detected, got nil")
	}

	if len(cycles) != 1 {
		t.Fatalf("Expected exactly 1 cycle for self-reference, got %d", len(cycles))
	}

	// Verify the cycle contains A twice (A -> A)
	cycle := cycles[0]
	if len(cycle) != 2 {
		t.Errorf("Expected cycle [A, A], got %v", cycle)
	}

	if cycle[0] != "A" || cycle[1] != "A" {
		t.Errorf("Expected self-reference cycle with A, got: %v", cycle)
	}
}

// TestDetectCircularDependencies_NoCycle tests that no cycles are detected in a valid DAG
func TestDetectCircularDependencies_NoCycle(t *testing.T) {
	dag := NewDAG()

	// Create valid DAG: A <- B <- C
	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("B"), Status: types.StatusOpen})
	dag.AddNode(types.Bead{ID: types.BeadID("C"), Status: types.StatusOpen})

	dag.AddEdge("B", "A")
	dag.AddEdge("C", "B")

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error for valid DAG, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected empty slice, got nil")
	}

	if len(cycles) != 0 {
		t.Errorf("Expected 0 cycles in valid DAG, got %d: %v", len(cycles), cycles)
	}
}

// TestDetectCircularDependencies_EmptyGraph tests detection on empty graph
func TestDetectCircularDependencies_EmptyGraph(t *testing.T) {
	dag := NewDAG()

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error for empty graph, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected empty slice, got nil")
	}

	if len(cycles) != 0 {
		t.Errorf("Expected 0 cycles in empty graph, got %d", len(cycles))
	}
}

// TestDetectCircularDependencies_SingleNode tests detection on single node without self-reference
func TestDetectCircularDependencies_SingleNode(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(types.Bead{ID: types.BeadID("A"), Status: types.StatusOpen})

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error for single node, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected empty slice, got nil")
	}

	if len(cycles) != 0 {
		t.Errorf("Expected 0 cycles for single node without self-reference, got %d", len(cycles))
	}
}

// TestDetectCircularDependencies_ComplexGraph tests detection in a complex graph with multiple cycles
func TestDetectCircularDependencies_ComplexGraph(t *testing.T) {
	dag := NewDAG()

	// Create complex graph:
	// A -> B -> C -> A (cycle 1)
	// B -> D -> B (cycle 2 - shares node B with cycle 1)
	// E -> F (no cycle)
	nodes := []string{"A", "B", "C", "D", "E", "F"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	// First cycle: A -> B -> C -> A
	dag.addEdgeInternal("B", "A")
	dag.addEdgeInternal("C", "B")
	dag.addEdgeInternal("A", "C")

	// Second cycle: B -> D -> B
	dag.addEdgeInternal("D", "B")
	dag.addEdgeInternal("B", "D")

	// Non-cyclic path
	dag.AddEdge("F", "E")

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected cycles to be detected, got nil")
	}

	// Should detect at least 2 cycles
	if len(cycles) < 2 {
		t.Fatalf("Expected at least 2 cycles in complex graph, got %d", len(cycles))
	}

	// Verify all cycles are closed
	for i, cycle := range cycles {
		if len(cycle) < 2 {
			t.Errorf("Cycle %d should have at least 2 nodes, got %d", i, len(cycle))
		}
		if cycle[0] != cycle[len(cycle)-1] {
			t.Errorf("Cycle %d should be closed (first == last), got: %v", i, cycle)
		}
	}
}

// TestDetectCircularDependencies_DiamondPattern tests detection on diamond pattern (no cycles)
func TestDetectCircularDependencies_DiamondPattern(t *testing.T) {
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

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error for diamond pattern, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected empty slice, got nil")
	}

	if len(cycles) != 0 {
		t.Errorf("Expected 0 cycles in diamond pattern, got %d: %v", len(cycles), cycles)
	}
}

// TestDetectCircularDependencies_OverlappingCycles tests detection of overlapping cycles
func TestDetectCircularDependencies_OverlappingCycles(t *testing.T) {
	dag := NewDAG()

	// Create overlapping cycles:
	// A -> B -> C -> A (cycle 1)
	// A -> B -> D -> A (cycle 2 - shares edges A->B)
	nodes := []string{"A", "B", "C", "D"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	// Shared edges
	dag.addEdgeInternal("B", "A")

	// First cycle edges
	dag.addEdgeInternal("C", "B")
	dag.addEdgeInternal("A", "C")

	// Second cycle edges
	dag.addEdgeInternal("D", "B")
	dag.addEdgeInternal("A", "D")

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected cycles to be detected, got nil")
	}

	// Should detect both cycles
	if len(cycles) < 2 {
		t.Fatalf("Expected at least 2 overlapping cycles, got %d", len(cycles))
	}

	// Verify all nodes A, B, C, D appear in at least one cycle
	allNodes := map[string]bool{"A": false, "B": false, "C": false, "D": false}
	for _, cycle := range cycles {
		for _, node := range cycle {
			if _, exists := allNodes[node]; exists {
				allNodes[node] = true
			}
		}
	}

	for node, found := range allNodes {
		if !found {
			t.Errorf("Node %s should appear in at least one cycle", node)
		}
	}
}

// TestDetectCircularDependencies_ChainedCycles tests detection of chained cycles
func TestDetectCircularDependencies_ChainedCycles(t *testing.T) {
	dag := NewDAG()

	// Create chained cycles:
	// A <-> B (cycle 1)
	// B <-> C (cycle 2 - shares node B)
	// Result: A <-> B <-> C creates one large cycle
	nodes := []string{"A", "B", "C"}
	for _, node := range nodes {
		dag.AddNode(types.Bead{ID: types.BeadID(node), Status: types.StatusOpen})
	}

	// A <-> B
	dag.addEdgeInternal("A", "B")
	dag.addEdgeInternal("B", "A")

	// B <-> C
	dag.addEdgeInternal("B", "C")
	dag.addEdgeInternal("C", "B")

	cycles, err := dag.DetectCircularDependencies()

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cycles == nil {
		t.Fatal("Expected cycles to be detected, got nil")
	}

	// Should detect at least 1 cycle
	if len(cycles) < 1 {
		t.Fatalf("Expected at least 1 cycle, got %d", len(cycles))
	}

	// The main cycle should include A, B, and C
	foundAll := false
	for _, cycle := range cycles {
		hasA, hasB, hasC := false, false, false
		for _, node := range cycle {
			if node == "A" {
				hasA = true
			}
			if node == "B" {
				hasB = true
			}
			if node == "C" {
				hasC = true
			}
		}
		if hasA && hasB && hasC {
			foundAll = true
			break
		}
	}

	if !foundAll {
		t.Error("Expected to find a cycle containing all nodes A, B, and C")
	}
}
