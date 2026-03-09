package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// GraphStats represents statistics about the dependency graph.
type GraphStats struct {
	TotalNodes     int
	ReadyNodes     int
	BlockedNodes   int
	CompletedNodes int
	FailedNodes    int
}

// DependencyGraphStats is an alias for GraphStats for backward compatibility.
type DependencyGraphStats = GraphStats

// DependencyGraph manages a directed acyclic graph of beads and their dependencies.
type DependencyGraph struct {
	dag       *DAG
	beadsByID map[string]types.Bead
	mu        sync.RWMutex
}

// NewDependencyGraph creates a new empty DependencyGraph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		dag:       NewDAG(),
		beadsByID: make(map[string]types.Bead),
	}
}

// DependencyGraphBuilder is an alias for DependencyGraph (legacy name).
// Deprecated: Use DependencyGraph instead.
type DependencyGraphBuilder = DependencyGraph

// NewDependencyGraphBuilder creates a new DependencyGraph (legacy alias).
// Deprecated: Use NewDependencyGraph instead.
func NewDependencyGraphBuilder() *DependencyGraph {
	return NewDependencyGraph()
}

// BuildFromBeads builds the dependency graph from a slice of beads.
// Returns ErrCyclicDependency if the beads contain a circular dependency.
func (g *DependencyGraph) BuildFromBeads(beads []types.Bead) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Reset first to ensure clean state
	g.dag = NewDAG()
	g.beadsByID = make(map[string]types.Bead)

	// First pass: add all nodes
	for _, bead := range beads {
		if err := g.addBeadNode(bead); err != nil {
			return err
		}
	}

	// Second pass: add edges for dependencies
	for _, bead := range beads {
		if err := g.addDependencyEdges(bead); err != nil {
			return err
		}
	}

	return nil
}

// addBeadNode adds a single bead as a node in the graph.
func (g *DependencyGraph) addBeadNode(bead types.Bead) error {
	if bead.ID == "" {
		return errors.New("bead has empty ID")
	}

	beadID := string(bead.ID)

	// Skip duplicates
	if _, exists := g.beadsByID[beadID]; exists {
		return nil
	}

	g.beadsByID[beadID] = bead

	if err := g.dag.AddNode(bead); err != nil {
		if errors.Is(err, ErrNodeAlreadyExists) {
			return fmt.Errorf("%w: %s", ErrNodeAlreadyExists, beadID)
		}
		return err
	}

	return nil
}

// addDependencyEdges adds edges for a bead's dependencies.
func (g *DependencyGraph) addDependencyEdges(bead types.Bead) error {
	beadID := string(bead.ID)

	for _, depID := range bead.Dependencies {
		// Check for self-reference
		if depID == bead.ID {
			return fmt.Errorf("%w: %s depends on itself", ErrSelfReference, beadID)
		}

		depIDStr := string(depID)

		// Skip missing dependencies
		if _, exists := g.beadsByID[depIDStr]; !exists {
			continue
		}

		// Add edge: bead depends on dep (bead is blocked by dep)
		// AddEdge performs cycle detection internally
		if err := g.dag.AddEdge(beadID, depIDStr); err != nil {
			return g.handleEdgeError(err, beadID)
		}
	}

	return nil
}

// handleEdgeError converts edge addition errors to appropriate types.
func (g *DependencyGraph) handleEdgeError(err error, beadID string) error {
	switch {
	case errors.Is(err, ErrEdgeCreatesCycle):
		return fmt.Errorf("%w: cycle detected involving %s", ErrCyclicDependency, beadID)
	case errors.Is(err, ErrSelfReference):
		return fmt.Errorf("%w: %s depends on itself", ErrSelfReference, beadID)
	default:
		return err
	}
}

// GetReadyBeads returns all beads that are ready to execute (no dependencies or all dependencies completed).
func (g *DependencyGraph) GetReadyBeads() []types.Bead {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := g.dag.GetReadyNodes()
	beads := make([]types.Bead, 0, len(nodes))

	for _, node := range nodes {
		beads = append(beads, node.Bead)
	}

	return beads
}

// GetBlockedBeads returns all beads that are blocked by dependencies.
func (g *DependencyGraph) GetBlockedBeads() []types.Bead {
	g.mu.RLock()
	defer g.mu.RUnlock()

	blocked := make([]types.Bead, 0)

	for id, bead := range g.beadsByID {
		node, exists := g.dag.GetNode(id)
		if !exists {
			continue
		}

		if node.InDegree > 0 && node.Status == types.StatusOpen {
			blocked = append(blocked, bead)
		}
	}

	return blocked
}

// MarkCompleted marks a bead as completed and returns newly unblocked bead IDs.
func (g *DependencyGraph) MarkCompleted(beadID string) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.dag.MarkCompleted(beadID)
}

// MarkFailed marks a bead as failed.
func (g *DependencyGraph) MarkFailed(beadID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.dag.MarkFailed(beadID)
}

// GetStats returns statistics about the dependency graph.
func (g *DependencyGraph) GetStats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := GraphStats{TotalNodes: g.dag.GetNodeCount()}

	for id := range g.beadsByID {
		node, exists := g.dag.GetNode(id)
		if !exists {
			continue
		}

		g.updateStatsForNode(&stats, node)
	}

	return stats
}

// updateStatsForNode updates statistics based on a node's state.
func (g *DependencyGraph) updateStatsForNode(stats *GraphStats, node *Node) {
	switch node.Status {
	case types.StatusCompleted, types.StatusClosed:
		stats.CompletedNodes++
	case types.StatusFailed:
		stats.FailedNodes++
	default:
		if node.InDegree == 0 {
			stats.ReadyNodes++
		} else {
			stats.BlockedNodes++
		}
	}
}

// GetTopologicalOrder returns the beads in topological order (dependencies before dependents).
func (g *DependencyGraph) GetTopologicalOrder() ([]string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.dag.TopologicalSort()
}

// Validate checks if the dependency graph is valid (no cycles).
func (g *DependencyGraph) Validate() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if err := g.dag.Validate(); err != nil {
		return err
	}

	return nil
}

// Reset clears the dependency graph and all state.
func (g *DependencyGraph) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.dag = NewDAG()
	g.beadsByID = make(map[string]types.Bead)
}
