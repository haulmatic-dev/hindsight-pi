// Package core provides the core orchestration logic for the Hydra workflow engine.
package core

import (
	"errors"
	"fmt"
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
)

// Color represents the visit state for DFS cycle detection.
type Color int

const (
	White Color = iota // Unvisited
	Gray               // In progress (currently in recursion stack)
	Black              // Finished
)

// Node represents a bead within the DAG, tracking its dependencies and state.
// It contains the bead data, dependency counts, and execution status.
type Node struct {
	// Bead is the underlying task data for this node.
	Bead types.Bead

	// InDegree is the number of incomplete dependencies (incoming edges).
	// A node is "ready" when InDegree reaches 0.
	InDegree int

	// OutEdges contains the IDs of beads that depend on this node.
	// These are the outgoing edges from this node.
	OutEdges []string

	// Status tracks the current execution state of the bead.
	Status types.BeadStatus
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
		Status:   bead.Status,
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
		if node.InDegree == 0 && node.Status == types.StatusOpen {
			ready = append(ready, node)
		}
	}

	return ready
}

// dfsVisit visits a node and its descendants using DFS.
// This is a helper method that must be called with the read lock held.
func (d *DAG) dfsVisit(nodeID string, colors map[string]Color, parent map[string]string, stack *[]string) []string {
	colors[nodeID] = Gray
	*stack = append(*stack, nodeID)

	// Visit all dependents
	node := d.nodes[nodeID]
	for _, dependentID := range node.OutEdges {
		parent[dependentID] = nodeID

		if colors[dependentID] == Gray {
			// Found a cycle - reconstruct path from the Gray node to current end
			return d.reconstructCyclePathFromStack(*stack, dependentID)
		}

		if colors[dependentID] == White {
			if cycle := d.dfsVisit(dependentID, colors, parent, stack); cycle != nil {
				return cycle
			}
		}
	}

	// Remove from stack when done
	*stack = (*stack)[:len(*stack)-1]
	colors[nodeID] = Black
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

// reconstructCyclePath reconstructs the cycle path from start to end.
// This is a helper method that must be called with the read lock held.
func (d *DAG) reconstructCyclePath(start, end string, parent map[string]string) []string {
	cycle := []string{start}
	current := end

	// Walk backwards from end to start
	for current != start {
		cycle = append([]string{current}, cycle...)
		current = parent[current]
		if current == "" {
			// Should not happen if cycle exists, but handle gracefully
			break
		}
	}

	cycle = append(cycle, start) // Close the cycle
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
// The algorithm uses three-color marking:
//   - White: Unvisited
//   - Gray: Currently being processed (in recursion stack)
//   - Black: Finished processing
//
// Time Complexity: O(V + E) where V is the number of vertices and E is the number of edges.
// Space Complexity: O(V) for the recursion stack and color map.
func (d *DAG) DetectCycle() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.nodes) == 0 {
		return nil, nil
	}

	// Three-color marking for cycle detection
	colors := make(map[string]Color, len(d.nodes))
	for id := range d.nodes {
		colors[id] = White
	}

	parent := make(map[string]string)

	// For each unvisited node, perform DFS
	for id := range d.nodes {
		if colors[id] == White {
			stack := []string{}
			if cycle := d.dfsVisit(id, colors, parent, &stack); cycle != nil {
				return cycle, &CycleError{Path: cycle}
			}
		}
	}

	return nil, nil
}

// detectCycleInternal performs cycle detection without acquiring locks.
// Must be called with read lock already held.
func (d *DAG) detectCycleInternal() ([]string, error) {
	colors := make(map[string]Color, len(d.nodes))
	for id := range d.nodes {
		colors[id] = White
	}

	parent := make(map[string]string)

	for id := range d.nodes {
		if colors[id] == White {
			stack := []string{}
			if cycle := d.dfsVisit(id, colors, parent, &stack); cycle != nil {
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

// GetNodeCount returns the total number of nodes in the DAG.
//
// Returns:
//   - int: The count of nodes in the graph.
func (d *DAG) GetNodeCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return len(d.nodes)
}

// NodeCount returns the total number of nodes in the DAG.
// Alias for GetNodeCount() for backward compatibility.
// Uses RLock for thread-safe read access.
//
// Returns:
//   - int: The count of nodes in the graph.
func (d *DAG) NodeCount() int {
	return d.GetNodeCount()
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

	node.Status = status
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
	if node.Status == types.StatusCompleted {
		return nil, fmt.Errorf("node %q is already completed", id)
	}
	if node.Status == types.StatusFailed {
		return nil, fmt.Errorf("cannot mark failed node %q as completed", id)
	}

	// Mark as completed
	node.Status = types.StatusCompleted

	// Reduce InDegree of all dependents and collect newly ready nodes
	newlyReady := make([]string, 0)
	for _, dependentID := range node.OutEdges {
		dependent, exists := d.nodes[dependentID]
		if !exists {
			continue // Skip if dependent no longer exists
		}

		dependent.InDegree--
		if dependent.InDegree == 0 && dependent.Status == types.StatusOpen {
			newlyReady = append(newlyReady, dependentID)
		}
	}

	return newlyReady, nil
}

// CompleteNode marks a node as completed and decrements the InDegree of dependent nodes.
// Alias for MarkCompleted for API consistency.
//
// Parameters:
//   - id: The ID of the completed node.
//
// Returns:
//   - ErrNodeNotFound if the node doesn't exist, nil otherwise.
//
// Example:
//
//	err := dag.CompleteNode("project-123")
func (d *DAG) CompleteNode(id string) error {
	_, err := d.MarkCompleted(id)
	return err
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
	if node.Status == types.StatusCompleted {
		return fmt.Errorf("cannot mark completed node %q as failed", id)
	}
	if node.Status == types.StatusFailed {
		return fmt.Errorf("node %q is already failed", id)
	}

	// Mark as failed - dependents remain blocked
	node.Status = types.StatusFailed

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

	return node.Status, nil
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

// GetDependents returns the IDs of all nodes that depend on the given node.
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
//	deps, err := dag.GetDependents("project-123")
//	fmt.Printf("Blocks: %v\n", deps)
func (d *DAG) GetDependents(id string) ([]string, error) {
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
		for i := 0; i < len(ready)-1; i++ {
			for j := i + 1; j < len(ready); j++ {
				if ready[j].Bead.Priority > ready[i].Bead.Priority {
					ready[i], ready[j] = ready[j], ready[i]
				}
			}
		}

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

// GetExecutionOrder returns nodes in priority order within topological order.
// Nodes at the same topological level are sorted by priority (high to low).
// Time complexity: O(V + E), Space complexity: O(V)
//
// Returns:
//   - []string: Node IDs in execution order.
//   - error: CycleError if a cycle is detected.
//
// Example:
//
//	order, err := dag.GetExecutionOrder()
func (d *DAG) GetExecutionOrder() ([]string, error) {
	return d.TopologicalSortWithPriority()
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
		if node.Status == types.StatusCompleted {
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
		if node.Status == types.StatusFailed {
			count++
		}
	}
	return count
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

// Reset resets the DAG to an empty state.
// Alias for Clear() for backward compatibility.
// Uses Lock for thread-safe write access.
func (d *DAG) Reset() {
	d.Clear()
}
