// Package core provides the core orchestration logic for the Hydra workflow engine.
package core

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

var (
	// ErrNodeAlreadyExists is returned when attempting to add a node that already exists.
	ErrNodeAlreadyExists = errors.New("node already exists")

	// ErrNodeNotFound is returned when a requested node does not exist in the DAG.
	ErrNodeNotFound = errors.New("node not found")

	// ErrEdgeCreatesCycle is returned when adding an edge would create a cycle in the DAG.
	ErrEdgeCreatesCycle = errors.New("adding edge would create a cycle")

	// ErrSelfReference is returned when attempting to create a self-referencing edge.
	ErrSelfReference = errors.New("node cannot depend on itself")

	// ErrCyclicDependency is returned when the dependency graph contains a cycle.
	ErrCyclicDependency = errors.New("cyclic dependency detected")
)

// CycleError represents a circular dependency detected in the DAG.
// It contains the path that forms the cycle.
type CycleError struct {
	// Path is a slice of node IDs that form the cycle.
	// The first and last elements should be the same node ID.
	Path []string
}

// Error returns a formatted error message for the CycleError.
// Format: "circular dependency detected: A → B → C → A"
func (e *CycleError) Error() string {
	if len(e.Path) == 0 {
		return "circular dependency detected: empty path"
	}

	return fmt.Sprintf("circular dependency detected: %s", strings.Join(e.Path, " → "))
}

// IsCycleError checks if the given error is or wraps a CycleError.
// It uses errors.As to traverse the error chain.
func IsCycleError(err error) bool {
	var cycleErr *CycleError
	return errors.As(err, &cycleErr)
}

// VisitState represents the visit state for DFS cycle detection.
type VisitState int

const (
	StateUnvisited VisitState = iota // Node has not been visited
	StateVisiting                    // Node is being processed (in recursion stack)
	StateVisited                     // Node has been fully processed
)

// Node represents a bead within the DAG, tracking its dependencies and state.
type Node struct {
	Bead     types.Bead
	InDegree int      // Number of incomplete dependencies
	OutEdges []string // IDs of nodes that depend on this node (outgoing edges)
}

// Status returns the bead's status for convenience.
func (n *Node) Status() types.BeadStatus {
	return n.Bead.Status
}

// DAG represents a Directed Acyclic Graph for tracking bead dependencies.
// It is thread-safe for concurrent access.
type DAG struct {
	// nodes maps bead IDs to their corresponding Node pointers.
	// The key is the string representation of types.BeadID.
	nodes map[string]*Node

	// mu provides read-write locking for thread-safe access to the DAG.
	// Use RLock for read operations, Lock for write operations.
	mu sync.RWMutex
}

// NewDAG creates and initializes a new empty DAG.
//
// Returns:
//   - A pointer to the newly created DAG instance.
//
// Example:
//
//	dag := NewDAG()
func NewDAG() *DAG {
	return &DAG{
		nodes: make(map[string]*Node),
	}
}

// AddNode adds a new bead to the DAG as a node.
// The node's initial InDegree is set to 0 and Status is set to the bead's current status.
//
// Parameters:
//   - bead: The bead to add as a node in the graph.
//
// Returns:
//   - error: ErrNodeAlreadyExists if a node with the same ID already exists, nil otherwise.
//
// Example:
//
//	bead := types.Bead{ID: "project-123", Title: "Implement feature"}
//	err := dag.AddNode(bead)
func (d *DAG) AddNode(bead types.Bead) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	id := string(bead.ID)

	if _, exists := d.nodes[id]; exists {
		return fmt.Errorf("%w: %s", ErrNodeAlreadyExists, id)
	}

	d.nodes[id] = &Node{
		Bead:     bead,
		InDegree: 0,
		OutEdges: make([]string, 0),
	}

	return nil
}

// GetNode retrieves a node by its ID.
func (d *DAG) GetNode(id string) (*Node, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	node, exists := d.nodes[id]
	return node, exists
}

// HasNode checks if a node exists in the DAG.
func (d *DAG) HasNode(id string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, exists := d.nodes[id]
	return exists
}

// AddEdge adds a dependency edge from 'from' to 'to' (from depends on to).
// This updates the InDegree of the 'to' node.
// Returns ErrEdgeCreatesCycle if the edge would create a cycle.
func (d *DAG) AddEdge(from, to string) error {
	if from == to {
		return ErrSelfReference
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if edge would create cycle
	if d.wouldCreateCycle(from, to) {
		return ErrEdgeCreatesCycle
	}

	return d.addEdgeInternal(from, to)
}

// addEdgeInternal adds an edge without cycle detection.
// Used internally and for testing purposes.
// Must be called with the lock already held.
// Edge direction: 'from' depends on 'to' (from -> to means from is blocked by to)
func (d *DAG) addEdgeInternal(from, to string) error {
	fromNode, fromExists := d.nodes[from]
	if !fromExists {
		return fmt.Errorf("from node %s not found: %w", from, ErrNodeNotFound)
	}

	toNode, toExists := d.nodes[to]
	if !toExists {
		return fmt.Errorf("to node %s not found: %w", to, ErrNodeNotFound)
	}

	// Add the edge: 'from' depends on 'to'
	// 'to' enables 'from', so 'to' gets OutEdge, 'from' increments InDegree
	toNode.OutEdges = append(toNode.OutEdges, from)
	fromNode.InDegree++

	return nil
}

// wouldCreateCycle checks if adding an edge from 'from' to 'to' would create a cycle.
// Edge direction: from depends on to (to enables from, stored as to.OutEdges = append(to.OutEdges, from))
// This means: if 'from' can already reach 'to', adding edge from->to would create a cycle.
// This is a helper method that must be called with the lock held.
func (d *DAG) wouldCreateCycle(from, to string) bool {
	visited := make(map[string]bool)
	return d.canReachWithVisited(from, to, visited)
}

// canReachWithVisited checks if there's a path from 'from' to 'to' using DFS.
// This is a helper method that must be called with the lock held.
func (d *DAG) canReachWithVisited(from, to string, visited map[string]bool) bool {
	if from == to {
		return true
	}

	if visited[from] {
		return false
	}
	visited[from] = true

	node, exists := d.nodes[from]
	if !exists {
		return false
	}

	for _, dependentID := range node.OutEdges {
		if d.canReachWithVisited(dependentID, to, visited) {
			return true
		}
	}

	return false
}

// GetReadyNodes returns all nodes that are ready to execute (InDegree == 0).
func (d *DAG) GetReadyNodes() []*Node {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ready := make([]*Node, 0)
	for _, node := range d.nodes {
		if node.InDegree == 0 && node.Bead.Status == types.StatusOpen {
			ready = append(ready, node)
		}
	}

	return ready
}

// dfsVisit visits a node and its descendants using DFS.
// This is a helper method that must be called with the read lock held.
func (d *DAG) dfsVisit(nodeID string, states map[string]VisitState, parent map[string]string, stack *[]string) []string {
	states[nodeID] = StateVisiting
	*stack = append(*stack, nodeID)

	// Visit all dependents
	node := d.nodes[nodeID]
	for _, dependentID := range node.OutEdges {
		parent[dependentID] = nodeID

		if states[dependentID] == StateVisiting {
			// Found a cycle - reconstruct path from the Visiting node to current end
			return d.reconstructCyclePathFromStack(*stack, dependentID)
		}

		if states[dependentID] == StateUnvisited {
			if cycle := d.dfsVisit(dependentID, states, parent, stack); cycle != nil {
				return cycle
			}
		}
	}

	// Remove from stack when done
	*stack = (*stack)[:len(*stack)-1]
	states[nodeID] = StateVisited
	return nil
}

// reconstructCyclePathFromStack reconstructs the cycle path from the DFS stack.
// It finds the cycle start node in the stack and returns the path from that node
// through the stack to the end, closing the cycle.
func (d *DAG) reconstructCyclePathFromStack(stack []string, cycleStart string) []string {
	// Find the index of cycleStart in the stack
	startIdx := -1
	for i, node := range stack {
		if node == cycleStart {
			startIdx = i
			break
		}
	}

	if startIdx == -1 {
		// Should not happen, but return empty if cycleStart not found
		return []string{cycleStart}
	}

	// Extract the cycle path: from cycleStart to end of stack, then back to cycleStart
	cycle := make([]string, 0, len(stack)-startIdx+1)
	cycle = append(cycle, stack[startIdx:]...)
	cycle = append(cycle, cycleStart) // Close the cycle
	return cycle
}

// Validate runs DetectCycle and returns error if cycle found.
//
// Returns:
//   - nil if the DAG is valid (no cycles).
//   - CycleError if a cycle is detected.
func (d *DAG) Validate() error {
	cycle, err := d.DetectCycle()
	if err != nil {
		return err
	}
	if cycle != nil {
		return &CycleError{Path: cycle}
	}
	return nil
}

// DetectCycle detects if the DAG contains any cycles using DFS.
//
// Returns:
//   - []string: The cycle path if a cycle is detected (e.g., ["A", "B", "C", "A"]).
//   - error: CycleError if a cycle is found, nil otherwise.
//
// The algorithm uses three-state marking:
//   - Unvisited: Node has not been processed
//   - Visiting: Node is being processed (in recursion stack)
//   - Visited: Node has been fully processed
//
// Time Complexity: O(V + E) where V is the number of vertices and E is the number of edges.
// Space Complexity: O(V) for the recursion stack and state map.
func (d *DAG) DetectCycle() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.detectCycleInternal()
}

// DetectCircularDependencies finds ALL elementary cycles in the DAG.
// Unlike DetectCycle which stops at the first cycle, this method finds all unique cycles.
//
// Returns:
//   - [][]string: A slice of cycles, where each cycle is a path (e.g., ["A", "B", "C", "A"]).
//   - error: Only if a system error occurs (e.g., out of memory), nil on success.
//
// Note: Returns empty slice (not nil) when no cycles are found.
// Each cycle path is closed (first == last).
//
// Time Complexity: O(V + E) per cycle found
// Space Complexity: O(V) for the recursion stack.
func (d *DAG) DetectCircularDependencies() ([][]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.nodes) == 0 {
		return [][]string{}, nil
	}

	cycles := make([][]string, 0)
	seen := make(map[string]bool)

	// Sort nodes for deterministic ordering
	nodeList := make([]string, 0, len(d.nodes))
	for id := range d.nodes {
		nodeList = append(nodeList, id)
	}
	sort.Strings(nodeList)

	// Use DFS from each node to find all cycles
	for _, startNode := range nodeList {
		visited := make(map[string]bool)
		d.findCyclesFromNode(startNode, startNode, []string{startNode}, visited, &cycles, seen)
	}

	return cycles, nil
}

// findCyclesFromNode recursively finds all cycles starting from startNode.
// It uses a visited set to prevent infinite recursion and a seen set for deduplication.
func (d *DAG) findCyclesFromNode(startNode, currentNode string, path []string, visited map[string]bool, cycles *[][]string, seen map[string]bool) {
	node := d.nodes[currentNode]
	if node == nil {
		return
	}

	for _, successor := range node.OutEdges {
		if successor == startNode {
			// Found a cycle back to start
			// For self-reference (path = [startNode]), require len(path) == 1
			// For longer cycles, require len(path) > 1
			if len(path) == 1 || len(path) > 1 {
				cycle := make([]string, len(path))
				copy(cycle, path)
				cycle = append(cycle, startNode)

				// Deduplicate: normalize and check if seen
				key := d.canonicalCycleKey(cycle)
				if !seen[key] {
					seen[key] = true
					*cycles = append(*cycles, cycle)
				}
			}
			// Don't recurse further when we hit startNode
			continue
		}

		if !visited[successor] {
			// Continue DFS - mark as visited to prevent revisiting in this path
			visited[successor] = true
			d.findCyclesFromNode(startNode, successor, append(path, successor), visited, cycles, seen)
			delete(visited, successor) // Backtrack
		}
	}
}

// canonicalCycleKey creates a unique key for a cycle to enable deduplication.
// It normalizes the cycle by finding the lexicographically smallest rotation.
func (d *DAG) canonicalCycleKey(cycle []string) string {
	if len(cycle) <= 1 {
		return strings.Join(cycle, ",")
	}

	// Remove the closing duplicate (last element if it equals first)
	nodes := cycle
	if cycle[0] == cycle[len(cycle)-1] {
		nodes = cycle[:len(cycle)-1]
	}

	// Find the lexicographically smallest rotation as canonical form
	minKey := ""
	n := len(nodes)

	for i := 0; i < n; i++ {
		// Create rotation starting at position i
		rotation := make([]string, n)
		for j := 0; j < n; j++ {
			rotation[j] = nodes[(i+j)%n]
		}
		key := strings.Join(rotation, ",")
		if minKey == "" || key < minKey {
			minKey = key
		}
	}

	return minKey
}

// detectCycleInternal performs cycle detection without acquiring locks.
// Must be called with read lock already held.
func (d *DAG) detectCycleInternal() ([]string, error) {
	states := make(map[string]VisitState, len(d.nodes))
	for id := range d.nodes {
		states[id] = StateUnvisited
	}

	parent := make(map[string]string)

	for id := range d.nodes {
		if states[id] == StateUnvisited {
			stack := []string{}
			if cycle := d.dfsVisit(id, states, parent, &stack); cycle != nil {
				return cycle, &CycleError{Path: cycle}
			}
		}
	}

	return nil, nil
}

// HasCycle checks if the DAG contains any cycles.
// This is a convenience method for quick validation.
//
// Returns:
//   - bool: true if the graph contains a cycle, false if it's a valid DAG.
func (d *DAG) HasCycle() bool {
	cycle, _ := d.DetectCycle()
	return cycle != nil
}

// RemoveNode removes a node and all its associated edges from the DAG.
// This operation updates the InDegree of all nodes that depended on the removed node.
//
// Parameters:
//   - id: The ID of the node to remove.
//
// Returns:
//   - ErrNodeNotFound if the node doesn't exist, nil otherwise.
//
// Example:
//
//	err := dag.RemoveNode("project-123")
//	if err != nil {
//	    log.Printf("Failed to remove node: %v", err)
//	}
func (d *DAG) RemoveNode(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	node, exists := d.nodes[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}

	// Remove this node from the OutEdges of all nodes it depends on
	// (nodes that have edges pointing to this node)
	for fromID, fromNode := range d.nodes {
		if fromID == id {
			continue
		}

		// Check if fromNode has an edge to the node being removed
		for i, outID := range fromNode.OutEdges {
			if outID == id {
				// Remove this edge by swapping with last element and truncating
				fromNode.OutEdges[i] = fromNode.OutEdges[len(fromNode.OutEdges)-1]
				fromNode.OutEdges = fromNode.OutEdges[:len(fromNode.OutEdges)-1]
				break
			}
		}
	}

	// Reduce InDegree of all nodes that this node pointed to
	for _, outID := range node.OutEdges {
		if outNode, exists := d.nodes[outID]; exists {
			if outNode.InDegree > 0 {
				outNode.InDegree--
			}
		}
	}

	// Remove the node from the map
	delete(d.nodes, id)

	return nil
}

// GetAllNodes returns all nodes in the graph.
// Returns a copy of the nodes slice to prevent external modification.
// Uses RLock for thread-safe read access.
//
// Returns:
//   - []*Node: A slice containing all nodes in the graph.
//
// Example:
//
//	nodes := dag.GetAllNodes()
//	fmt.Printf("Total nodes: %d\n", len(nodes))
func (d *DAG) GetAllNodes() []*Node {
	d.mu.RLock()
	defer d.mu.RUnlock()

	nodes := make([]*Node, 0, len(d.nodes))
	for _, node := range d.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// CloseReadyEpics auto-closes all epic nodes whose blocking dependencies are
// closed AND whose downstream TASK children are all closed. Epics are
// organizational containers with no real "work", so we never spawn workers
// for them. Without this step, an epic whose dependencies and task children
// are all done would stay open forever, blocking its own downstream dependents
// via the dependency graph.
//
// Only TASK children count toward the close check; downstream epic children
// (e.g. PHASE 1 -> PHASE 2) are ignored. A phase epic with no task children of
// its own closes as soon as its dependencies are done, and the next phase
// epic closes on the next pass through this loop. This avoids a deadlock
// where phase A waits for phase B, and phase B waits for phase A.
//
// The method loops to a fixed point: closing one epic can decrement the
// InDegree of its downstream epic, allowing that next phase to close in the
// next iteration. Without the loop, only one level of epics would close per
// pass.
//
// Returns the list of closed epic IDs so the caller can persist the "closed"
// state to the beads store via br close.
//
// This must be called after the DAG is built (so all nodes and edges are
// present) and before the scheduler selects ready beads.
func (d *DAG) CloseReadyEpics() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	var closed []string
	for {
		madeProgress := false
		for _, node := range d.nodes {
			// Skip non-epics. Real work beads are scheduled by the regular
			// worker pipeline and closed via br close; we don't touch them.
			if node.Bead.Type != "epic" {
				continue
			}
			// Only auto-close epics that are still actionable (open/in_progress).
			// Closed epics are already terminal; failed epics need human review.
			if node.Bead.Status != types.StatusOpen && node.Bead.Status != types.StatusInProgress {
				continue
			}
			// InDegree == 0 means all blocking dependencies are closed.
			if node.InDegree != 0 {
				continue
			}
			// All TASK children must be closed before closing an epic. We
			// deliberately ignore epic children: a phase epic like PHASE 1
			// shouldn't have to wait for PHASE 2 to be closed before PHASE 1
			// itself can close. Each phase epic manages its own sub-tree of
			// task beads, and downstream phase epics are closed by their own
			// pass through this loop.
			//
			// This avoids a deadlock where phase A blocks phase B blocks phase A
			// (because phase A's "children" include phase B, which is open
			// while phase A is also open).
			allTaskChildrenClosed := true
			for _, childID := range node.OutEdges {
				child, ok := d.nodes[childID]
				if !ok {
					continue
				}
				if child.Bead.Type == "epic" {
					continue // downstream epics close independently
				}
				if child.Bead.Status != types.StatusClosed {
					allTaskChildrenClosed = false
					break
				}
				// completed != success: a closed-failed child must not satisfy
				// the epic unless its fix bead ("Fix: <title>") closed
				// successfully. Observed live 2026-08-06: srv-smart-engine phase
				// epics auto-closed over children that were closed-failed, so
				// whole phases reported done with no working code.
				if beadHasLabel(child.Bead, closedFailedLabel) && !d.fixBeadSucceeded(child.Bead) {
					allTaskChildrenClosed = false
					break
				}
			}
			if !allTaskChildrenClosed {
				continue
			}

			node.Bead.Status = types.StatusClosed
			closed = append(closed, string(node.Bead.ID))
			madeProgress = true
			// Decrement InDegree on downstream dependents so the cascade check
			// sees them as ready in subsequent iterations. This mirrors what
			// MarkCompleted does for regular completion, but skips validation
			// since we only call it for organizational epics (not real work).
			for _, dependentID := range node.OutEdges {
				dependent, ok := d.nodes[dependentID]
				if !ok {
					continue
				}
				if dependent.InDegree > 0 {
					dependent.InDegree--
				}
			}
		}
		if !madeProgress {
			break
		}
	}
	return closed
}

// GetNodeCount returns the total number of nodes in the DAG.
//
// Returns:
//   - int: The count of nodes in the graph.
func (d *DAG) GetNodeCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return len(d.nodes)
}

// UpdateNodeStatus updates the status of a node.
//
// Parameters:
//   - id: The ID of the node to update.
//   - status: The new status to set.
//
// Returns:
//   - ErrNodeNotFound if the node doesn't exist, nil otherwise.
//
// Example:
//
//	err := dag.UpdateNodeStatus("project-123", types.StatusInProgress)
func (d *DAG) UpdateNodeStatus(id string, status types.BeadStatus) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	node, exists := d.nodes[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}

	node.Bead.Status = status
	return nil
}

// MarkCompleted marks a node as completed and reduces InDegree of all dependents.
// Returns a list of newly ready node IDs (those whose InDegree became 0).
// Uses Lock for thread-safe write access.
//
// Parameters:
//   - id: The ID of the completed node.
//
// Returns:
//   - []string: List of newly ready node IDs.
//   - error: ErrNodeNotFound if node doesn't exist, or error if invalid state transition.
//
// Errors:
//   - Returns error if node not found
//   - Returns error if node is not in a valid state to be marked completed
//
// Example:
//
//	ready, err := dag.MarkCompleted("project-123")
//	if err == nil {
//	    fmt.Printf("Newly ready nodes: %v\n", ready)
//	}
func (d *DAG) MarkCompleted(id string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	node, exists := d.nodes[id]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}

	// Validate state transition - can only complete from in_progress or open
	if node.Bead.Status == types.StatusCompleted {
		return nil, fmt.Errorf("node %q is already completed", id)
	}
	if node.Bead.Status == types.StatusFailed {
		return nil, fmt.Errorf("cannot mark failed node %q as completed", id)
	}

	// Mark as completed
	node.Bead.Status = types.StatusCompleted

	// Reduce InDegree of all dependents and collect newly ready nodes
	newlyReady := make([]string, 0)
	for _, dependentID := range node.OutEdges {
		dependent, exists := d.nodes[dependentID]
		if !exists {
			continue // Skip if dependent no longer exists
		}

		dependent.InDegree--
		if dependent.InDegree == 0 && dependent.Bead.Status == types.StatusOpen {
			newlyReady = append(newlyReady, dependentID)
		}
	}

	return newlyReady, nil
}

// MarkFailed marks a node as failed.
// Failed nodes do not unblock their dependents - dependencies remain blocked.
// Uses Lock for thread-safe write access.
//
// Parameters:
//   - id: The ID of the failed node.
//
// Returns:
//   - ErrNodeNotFound if node doesn't exist, or error if invalid state transition.
//
// Errors:
//   - Returns error if node not found
//   - Returns error if node is already in a terminal state
//
// Example:
//
//	err := dag.MarkFailed("project-123")
func (d *DAG) MarkFailed(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	node, exists := d.nodes[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}

	// Validate state transition
	if node.Bead.Status == types.StatusCompleted {
		return fmt.Errorf("cannot mark completed node %q as failed", id)
	}
	if node.Bead.Status == types.StatusFailed {
		return fmt.Errorf("node %q is already failed", id)
	}

	// Mark as failed - dependents remain blocked
	node.Bead.Status = types.StatusFailed

	return nil
}

// GetNodeStatus returns the current status of a node.
// Uses RLock for thread-safe read access.
//
// Parameters:
//   - id: The ID of the node to check.
//
// Returns:
//   - types.BeadStatus: The current status.
//   - error: ErrNodeNotFound if node doesn't exist.
//
// Errors:
//   - Returns error if node not found
func (d *DAG) GetNodeStatus(id string) (types.BeadStatus, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	node, exists := d.nodes[id]
	if !exists {
		return "", fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}

	return node.Bead.Status, nil
}

// GetDependencies returns the IDs of all nodes that the given node depends on.
// This is the reverse lookup of OutEdges.
//
// Parameters:
//   - id: The ID of the node to get dependencies for.
//
// Returns:
//   - []string: A slice of dependency node IDs.
//   - error: ErrNodeNotFound if the node doesn't exist, nil otherwise.
//
// Example:
//
//	deps, err := dag.GetDependencies("project-456")
//	fmt.Printf("Depends on: %v\n", deps)
func (d *DAG) GetDependencies(id string) ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, exists := d.nodes[id]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}

	// Build the list by finding all nodes that have this node in their OutEdges
	dependencies := make([]string, 0)
	for fromID, fromNode := range d.nodes {
		if fromID == id {
			continue
		}
		for _, outID := range fromNode.OutEdges {
			if outID == id {
				dependencies = append(dependencies, fromID)
				break
			}
		}
	}

	return dependencies, nil
}

// GetOutEdges returns the IDs of all nodes that depend on the given node.
// This is equivalent to OutEdges.
//
// Parameters:
//   - id: The ID of the node to get dependents for.
//
// Returns:
//   - []string: A slice of dependent node IDs.
//   - error: ErrNodeNotFound if the node doesn't exist, nil otherwise.
//
// Example:
//
//	deps, err := dag.GetOutEdges("project-123")
//	fmt.Printf("Blocks: %v\n", deps)
func (d *DAG) GetOutEdges(id string) ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	node, exists := d.nodes[id]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}

	// Return a copy to prevent external modification
	dependents := make([]string, len(node.OutEdges))
	copy(dependents, node.OutEdges)

	return dependents, nil
}

// TopologicalSort performs a topological sort using Kahn's algorithm.
// Returns node IDs in dependency order (dependencies before dependents).
// Time: O(V+E), Space: O(V)
func (d *DAG) TopologicalSort() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.nodes) == 0 {
		return []string{}, nil
	}

	// Copy in-degrees
	inDegree := make(map[string]int, len(d.nodes))
	for id, node := range d.nodes {
		inDegree[id] = node.InDegree
	}

	// Queue nodes with in-degree 0
	queue := make([]string, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	result := make([]string, 0, len(d.nodes))

	// Process nodes
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		result = append(result, nodeID)

		node := d.nodes[nodeID]
		for _, dependentID := range node.OutEdges {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				queue = append(queue, dependentID)
			}
		}
	}

	// Check for cycle
	if len(result) != len(d.nodes) {
		cycle, _ := d.detectCycleInternal()
		return nil, &CycleError{Path: cycle}
	}

	return result, nil
}

// TopologicalSortWithPriority performs topological sort with priority ordering.
// At each level, selects highest priority nodes first.
func (d *DAG) TopologicalSortWithPriority() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.nodes) == 0 {
		return []string{}, nil
	}

	inDegree := make(map[string]int, len(d.nodes))
	for id, node := range d.nodes {
		inDegree[id] = node.InDegree
	}

	result := make([]string, 0, len(d.nodes))
	processed := make(map[string]bool)

	for len(result) < len(d.nodes) {
		// Find ready nodes
		ready := make([]*Node, 0)
		for id, degree := range inDegree {
			if degree == 0 && !processed[id] {
				if node, exists := d.nodes[id]; exists {
					ready = append(ready, node)
				}
			}
		}

		if len(ready) == 0 {
			cycle, _ := d.detectCycleInternal()
			return nil, &CycleError{Path: cycle}
		}

		// Sort by priority (highest first)
		sort.Slice(ready, func(i, j int) bool {
			return ready[i].Bead.Priority > ready[j].Bead.Priority
		})

		// Process highest priority
		node := ready[0]
		nodeID := node.Bead.ID.String()
		result = append(result, nodeID)
		processed[nodeID] = true

		for _, dependentID := range node.OutEdges {
			inDegree[dependentID]--
		}
	}

	return result, nil
}

// GetBlockedCount returns the number of nodes that are blocked (InDegree > 0).
// Uses RLock for thread-safe read access.
//
// Returns:
//   - int: The count of blocked nodes.
func (d *DAG) GetBlockedCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	count := 0
	for _, node := range d.nodes {
		if node.InDegree > 0 {
			count++
		}
	}
	return count
}

// GetCompletedCount returns the number of nodes marked as completed.
// Uses RLock for thread-safe read access.
//
// Returns:
//   - int: The count of completed nodes.
func (d *DAG) GetCompletedCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	count := 0
	for _, node := range d.nodes {
		if node.Bead.Status == types.StatusCompleted {
			count++
		}
	}
	return count
}

// GetFailedCount returns the number of nodes marked as failed.
// Uses RLock for thread-safe read access.
//
// Returns:
//   - int: The count of failed nodes.
func (d *DAG) GetFailedCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	count := 0
	for _, node := range d.nodes {
		if node.Bead.Status == types.StatusFailed {
			count++
		}
	}
	return count
}

// DAGStats holds statistical information about the DAG state.
type DAGStats struct {
	TotalNodes     int
	ReadyNodes     int
	BlockedNodes   int
	CompletedNodes int
	FailedNodes    int
}

// GetStats returns comprehensive statistics about the DAG state.
//
// Returns:
//   - DAGStats: A struct containing counts of nodes in various states.
func (d *DAG) GetStats() DAGStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := DAGStats{
		TotalNodes:     len(d.nodes),
		BlockedNodes:   0,
		CompletedNodes: 0,
		FailedNodes:    0,
		ReadyNodes:     0,
	}

	for _, node := range d.nodes {
		switch node.Bead.Status {
		case types.StatusCompleted:
			stats.CompletedNodes++
		case types.StatusFailed:
			stats.FailedNodes++
		case types.StatusBlocked:
			stats.BlockedNodes++
		case types.StatusOpen, types.StatusInProgress:
			// A node is ready if it's not blocked, not completed, and not failed
			// and all its dependencies are satisfied
			if node.InDegree == 0 {
				stats.ReadyNodes++
			}
		}
	}

	return stats
}

// Clear removes all nodes from the DAG, resetting it to an empty state.
//
// Example:
//
//	dag.Clear()
//	fmt.Printf("Nodes after clear: %d\n", dag.GetNodeCount()) // Output: 0
func (d *DAG) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nodes = make(map[string]*Node)
}

// beadHasLabel reports whether the bead carries the given label.
func beadHasLabel(b types.Bead, label string) bool {
	for _, l := range b.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// fixBeadSucceeded reports whether a closed-failed bead has a fix bead
// (titled "Fix: <original title>", per createFixBeadForFailedClose) that
// closed WITHOUT the closed-failed label — i.e. the fix actually landed.
func (d *DAG) fixBeadSucceeded(original types.Bead) bool {
	want := "Fix: " + original.Title
	for _, n := range d.nodes {
		if n.Bead.Title != want {
			continue
		}
		if n.Bead.Status == types.StatusClosed && !beadHasLabel(n.Bead, closedFailedLabel) {
			return true
		}
	}
	return false
}
