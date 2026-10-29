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
		depBead, exists := g.beadsByID[depIDStr]
		if !exists {
			continue
		}

		// Skip already-completed dependencies — they're already satisfied
		if depBead.Status == types.StatusCompleted || depBead.Status == types.StatusClosed {
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

		if node.InDegree > 0 && node.Status() == types.StatusOpen {
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
	switch node.Status() {
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

// RollbackReleases reverts the releases granted by beadID's completion: every
// dependent of beadID that had been released by that completion (InDegree
// decremented to zero while still open) is re-blocked, returning the graph to
// its pre-completion readiness. Dependents that were never released (still
// blocked by another open dependency) are left untouched. Returns the
// re-blocked dependent IDs; a no-op returns nil with a nil error. Rolling back
// a bead that is not completed, or rolling back the same completion twice, is
// a no-op (idempotent).
func (g *DependencyGraph) RollbackReleases(beadID string) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.dag.RollbackReleases(beadID)
}

// RollbackReleases reverts the releases granted by id's completion: every
// dependent whose InDegree was decremented to zero by that completion and is
// still open gets its InDegree restored, so it returns to the blocked set.
// Dependents still blocked by another open dependency (InDegree > 0), already
// terminal, or already re-blocked by an earlier rollback are untouched —
// which makes the rollback idempotent. A non-completed bead is a no-op.
// Returns the re-blocked dependent IDs in edge order; empty when nothing was
// re-blocked.
func (d *DAG) RollbackReleases(id string) ([]string, error) {
	node, exists := d.nodes[id]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}

	// Only a completed bead has granted releases to revert.
	if node.Bead.Status != types.StatusCompleted {
		return nil, nil
	}

	rolledBack := make([]string, 0)
	for _, dependentID := range node.OutEdges {
		dependent, ok := d.nodes[dependentID]
		if !ok {
			continue // Skip if dependent no longer exists
		}
		// "Released by this completion" = InDegree decremented to zero and the
		// dependent is still open. Everything else (never released, terminal,
		// or already re-blocked by a prior rollback) stays as it is.
		if dependent.InDegree != 0 || dependent.Bead.Status != types.StatusOpen {
			continue
		}
		dependent.InDegree++
		rolledBack = append(rolledBack, dependentID)
	}
	if len(rolledBack) == 0 {
		return nil, nil
	}
	return rolledBack, nil
}
