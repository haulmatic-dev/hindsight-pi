package core

import (
	"fmt"
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
