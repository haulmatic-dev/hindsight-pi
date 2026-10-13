package core

import (
	"fmt"
	"sort"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// DAGNavigator provides thread-safe access to DAG operations.
type DAGNavigator struct {
	dag *DAG
}

// NewDAGNavigator creates a new DAG navigator.
func NewDAGNavigator(dag *DAG) *DAGNavigator {
	return &DAGNavigator{dag: dag}
}

// GetDependencies returns the IDs of beads that the given bead depends on.
func (n *DAGNavigator) GetDependencies(beadID string) ([]string, error) {
	if n.dag == nil {
		return nil, fmt.Errorf("DAG not initialized")
	}
	return n.dag.GetDependencies(beadID)
}

// GetDependents returns the IDs of beads that depend on the given bead.
func (n *DAGNavigator) GetDependents(beadID string) ([]string, error) {
	if n.dag == nil {
		return nil, fmt.Errorf("DAG not initialized")
	}
	return n.dag.GetOutEdges(beadID)
}

// GetBead retrieves a single bead from the DAG by its ID.
func (n *DAGNavigator) GetBead(beadID string) (types.Bead, error) {
	if n.dag == nil {
		return types.Bead{}, fmt.Errorf("DAG not initialized")
	}
	node, exists := n.dag.GetNode(beadID)
	if !exists {
		return types.Bead{}, fmt.Errorf("bead %s not found in DAG", beadID)
	}
	return node.Bead, nil
}

// GetNode returns the DAG node for the given bead ID.
func (n *DAGNavigator) GetNode(beadID string) (*Node, bool) {
	if n.dag == nil {
		return nil, false
	}
	return n.dag.GetNode(beadID)
}

// GetReadyNodes returns all nodes that are ready to execute.
func (n *DAGNavigator) GetReadyNodes() []*Node {
	if n.dag == nil {
		return nil
	}
	return n.dag.GetReadyNodes()
}

// MarkCompleted marks a bead as completed and returns newly ready dependents.
func (n *DAGNavigator) MarkCompleted(beadID string) ([]string, error) {
	if n.dag == nil {
		return nil, fmt.Errorf("DAG not initialized")
	}
	return n.dag.MarkCompleted(beadID)
}

// MarkFailed marks a bead as failed.
func (n *DAGNavigator) MarkFailed(beadID string) error {
	if n.dag == nil {
		return fmt.Errorf("DAG not initialized")
	}
	return n.dag.MarkFailed(beadID)
}

// Clear removes all nodes from the DAG.
func (n *DAGNavigator) Clear() {
	if n.dag != nil {
		n.dag.Clear()
	}
}

// GetReadyBeads returns beads that are ready for execution, filtering out
// approval gates and already-completed beads, sorted by priority descending.
func (n *DAGNavigator) GetReadyBeads(completed map[string]bool) []types.Bead {
	if n.dag == nil {
		return nil
	}

	readyNodes := n.dag.GetReadyNodes()
	var beads []types.Bead
	for _, node := range readyNodes {
		if node.Bead.Type == "approval-gate" || node.Bead.Type == "approval" {
			// Human gates are never worker-executed, whatever their status.
			continue
		}
		if hasBeadLabel(node.Bead.Labels, escalatedLabel) {
			// Escalated beads are parked for a human; never hand them to a
			// worker even if a stale status/label refresh made them look ready.
			continue
		}
		if hasBeadLabel(node.Bead.Labels, worktreeUnavailableLabel) || hasBeadLabel(node.Bead.Labels, gitOwnershipLabel) {
			// Hard-parked by a fail-closed worktree spawn: the chain worktree
			// was unavailable and isolation must never silently downgrade to
			// the shared main checkout (2026-09-11/12 corruption incidents).
			continue
		}
		if hasBeadLabel(node.Bead.Labels, integrationExhaustedLabel) {
			// Hard-parked by failIntegration after maxConsecutiveIntegrationFailures:
			// the close repeatedly failed at integrate/push. Scheduling it here
			// respawned the bead into the same failing integrate (ag6i, 2026-09-27:
			// ~95 respawns). Only Pan repair or a human clears the label.
			continue
		}
		if hasBeadLabel(node.Bead.Labels, integrationPendingLabel) {
			// Work complete, integrate failed; the retest stage owns the retry.
			continue
		}
		if completed != nil && completed[string(node.Bead.ID)] {
			continue
		}
		beads = append(beads, node.Bead)
	}

	sort.SliceStable(beads, func(i, j int) bool {
		return beads[i].Priority < beads[j].Priority
	})

	return beads
}

// GetAllDescendants returns all descendant node IDs for a given node ID.
func (n *DAGNavigator) GetAllDescendants(nodeID string) []string {
	var descendants []string
	visited := make(map[string]bool)

	var dfs func(id string)
	dfs = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true

		node, exists := n.dag.GetNode(id)
		if !exists {
			return
		}

		for _, outEdge := range node.OutEdges {
			descendants = append(descendants, outEdge)
			dfs(outEdge)
		}
	}

	dfs(nodeID)
	return descendants
}
