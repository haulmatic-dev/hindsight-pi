// Package core provides the core orchestration logic for the Hydra workflow engine.
package core

import (
	"container/list"
	"errors"
	"fmt"
	"sort"
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

// AddNodeWithID adds a new node to the DAG with a specific ID.
// This is a lower-level method that allows specifying the node ID independently
// of the bead's ID. Use AddNode() for the standard case.
//
// Parameters:
//   - id: The ID to use for the node.
//   - bead: The bead data to associate with the node.
//
// Returns:
//   - *Node: The created node.
//
// Example:
//
//	node := dag.AddNodeWithID("custom-id", bead)
func (d *DAG) AddNodeWithID(id string, bead types.Bead) *Node {
	d.mu.Lock()
	defer d.mu.Unlock()

	node := &Node{
		Bead:     bead,
		InDegree: 0,
		OutEdges: make([]string, 0),
		Status:   types.StatusOpen,
	}
	d.nodes[id] = node
	return node
}

// AddEdge creates a dependency edge where 'from' depends on 'to'.
// This means 'to' must complete before 'from' can start.
//
// Parameters:
//   - from: The ID of the dependent bead (the one that waits).
//   - to: The ID of the prerequisite bead (the one that must complete first).
//
// Returns:
//   - ErrNodeNotFound if either node doesn't exist.
//   - ErrSelfReference if from and to are the same.
//   - ErrEdgeCreatesCycle if adding this edge would create a cycle.
//   - nil if the edge was added successfully.
//
// Example:
//
//	// "project-456" depends on "project-123" completing first
//	err := dag.AddEdge("project-456", "project-123")
func (d *DAG) AddEdge(from, to string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Validate nodes exist
	fromNode, fromExists := d.nodes[from]
	if !fromExists {
		return fmt.Errorf("%w: 'from' node %s", ErrNodeNotFound, from)
	}

	toNode, toExists := d.nodes[to]
	if !toExists {
		return fmt.Errorf("%w: 'to' node %s", ErrNodeNotFound, to)
	}

	// Prevent self-references
	if from == to {
		return ErrSelfReference
	}

	// Check if edge already exists
	for _, existing := range toNode.OutEdges {
		if existing == from {
			return nil // Edge already exists, no error
		}
	}

	// Check for cycles using DFS before adding the edge
	if d.wouldCreateCycle(to, from) {
		return ErrEdgeCreatesCycle
	}

	// Add the edge (from depends on to)
	// Update InDegree of the dependent node
	fromNode.InDegree++

	// Add 'from' to the OutEdges of 'to'
	toNode.OutEdges = append(toNode.OutEdges, from)

	return nil
}

// wouldCreateCycle checks if adding an edge from 'from' to 'to' would create a cycle.
// This is a helper method that must be called with the write lock held.
//
// Parameters:
//   - from: The source node of the potential edge (the prerequisite).
//   - to: The target node of the potential edge (the dependent).
//
// Returns:
//   - true if adding the edge would create a cycle, false otherwise.
func (d *DAG) wouldCreateCycle(from, to string) bool {
	// If we can reach 'from' from 'to', adding an edge to->from creates a cycle
	visited := make(map[string]bool)
	return d.canReach(to, from, visited)
}

// canReach performs a depth-first search to determine if target is reachable from start.
// This is a helper method that must be called with the write lock held.
//
// Parameters:
//   - start: The node to start searching from.
//   - target: The node to search for.
//   - visited: A map tracking visited nodes to prevent infinite recursion.
//
// Returns:
//   - true if target is reachable from start, false otherwise.
func (d *DAG) canReach(start, target string, visited map[string]bool) bool {
	if start == target {
		return true
	}

	if visited[start] {
		return false
	}
	visited[start] = true

	node, exists := d.nodes[start]
	if !exists {
		return false
	}

	for _, neighbor := range node.OutEdges {
		if d.canReach(neighbor, target, visited) {
			return true
		}
	}

	return false
}

// GetNode retrieves a node by its ID.
//
// Parameters:
//   - id: The string ID of the node to retrieve.
//
// Returns:
//   - *Node: The node if found.
//   - bool: true if the node exists, false otherwise.
//
// Example:
//
//	node, exists := dag.GetNode("project-123")
//	if exists {
//	    fmt.Println(node.Bead.Title)
//	}
func (d *DAG) GetNode(id string) (*Node, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	node, exists := d.nodes[id]
	return node, exists
}

// HasNode checks if a node with the given ID exists in the DAG.
//
// Parameters:
//   - id: The string ID to check for.
//
// Returns:
//   - bool: true if the node exists, false otherwise.
//
// Example:
//
//	if dag.HasNode("project-123") {
//	    // Process the node
//	}
func (d *DAG) HasNode(id string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, exists := d.nodes[id]
	return exists
}

// GetReadyNodes returns all nodes that have no incomplete dependencies (InDegree == 0)
// and have StatusOpen status. These are the nodes that can be executed immediately.
// Uses RLock for thread-safe read access.
//
// Returns:
//   - []*Node: A slice of nodes that are ready for execution.
//
// Example:
//
//	ready := dag.GetReadyNodes()
//	for _, node := range ready {
//	    fmt.Printf("Node %s is ready to execute\n", node.Bead.ID)
//	}
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

// GetReadyNodeIDs returns all node IDs that have no incomplete dependencies (InDegree == 0).
// This method returns all nodes with InDegree == 0 regardless of status.
//
// Returns:
//   - []string: A slice of node IDs that are ready for execution.
//
// Example:
//
//	ready := dag.GetReadyNodeIDs()
//	for _, id := range ready {
//	    fmt.Printf("Node %s is ready\n", id)
//	}
func (d *DAG) GetReadyNodeIDs() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ready := make([]string, 0)
	for id, node := range d.nodes {
		if node.InDegree == 0 {
			ready = append(ready, id)
		}
	}

	return ready
}

// TopologicalSort returns a valid execution order for all nodes in the DAG.
// Nodes are ordered such that all dependencies come before the nodes that depend on them.
// Uses Kahn's algorithm for topological sorting.
//
// Returns:
//   - []string: A slice of node IDs in topological order.
//   - error: CycleError if the graph contains a cycle, nil otherwise.
//
// Example:
//
//	order, err := dag.TopologicalSort()
//	if err != nil {
//	    log.Fatal("Graph contains a cycle:", err)
//	}
//	for _, id := range order {
//	    fmt.Printf("Execute: %s\n", id)
//	}
func (d *DAG) TopologicalSort() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.nodes) == 0 {
		return []string{}, nil
	}

	// Step 1: Calculate in-degree for all nodes
	inDegree := make(map[string]int, len(d.nodes))
	for id := range d.nodes {
		inDegree[id] = 0
	}

	// For each edge u -> v, increment in-degree of v
	for _, node := range d.nodes {
		for _, dependentID := range node.OutEdges {
			inDegree[dependentID]++
		}
	}

	// Step 2: Queue nodes with in-degree 0
	queue := list.New()
	for id, degree := range inDegree {
		if degree == 0 {
			queue.PushBack(id)
		}
	}

	// Step 3: Process queue
	result := make([]string, 0, len(d.nodes))

	for queue.Len() > 0 {
		// Dequeue
		elem := queue.Front()
		nodeID := elem.Value.(string)
		queue.Remove(elem)

		result = append(result, nodeID)

		// For each dependent, decrement in-degree
		node := d.nodes[nodeID]
		for _, dependentID := range node.OutEdges {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				queue.PushBack(dependentID)
			}
		}
	}

	// Step 4: Check for cycle
	if len(result) != len(d.nodes) {
		// Find cycle for error message
		cycle, _ := d.detectCycleDFS()
		return nil, &CycleError{Path: cycle}
	}

	return result, nil
}

// TopologicalSortWithPriority returns a topologically sorted list of node IDs,
// sorted by priority (highest first) within each dependency level.
//
// Returns:
//   - []string: A slice of node IDs in topological order with priority ordering.
//   - error: CycleError if the graph contains a cycle, nil otherwise.
//
// Example:
//
//	order, err := dag.TopologicalSortWithPriority()
func (d *DAG) TopologicalSortWithPriority() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.nodes) == 0 {
		return []string{}, nil
	}

	// Step 1: Calculate in-degree for all nodes
	inDegree := make(map[string]int, len(d.nodes))
	for id := range d.nodes {
		inDegree[id] = 0
	}

	for _, node := range d.nodes {
		for _, dependentID := range node.OutEdges {
			inDegree[dependentID]++
		}
	}

	// Step 2: Use a priority queue (sorted by priority descending)
	type readyNode struct {
		id       string
		priority int
	}

	// Collect nodes with in-degree 0
	ready := make([]readyNode, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			node := d.nodes[id]
			ready = append(ready, readyNode{id: id, priority: node.Bead.Priority})
		}
	}

	// Sort by priority descending (higher priority first)
	sort.Slice(ready, func(i, j int) bool {
		return ready[i].priority > ready[j].priority
	})

	// Step 3: Process levels
	result := make([]string, 0, len(d.nodes))

	for len(ready) > 0 {
		// Take the highest priority node
		current := ready[0]
		ready = ready[1:]

		result = append(result, current.id)

		// Process dependents
		node := d.nodes[current.id]
		newReady := make([]readyNode, 0)
		for _, dependentID := range node.OutEdges {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				depNode := d.nodes[dependentID]
				newReady = append(newReady, readyNode{id: dependentID, priority: depNode.Bead.Priority})
			}
		}

		// Merge new ready nodes and sort
		ready = append(ready, newReady...)
		sort.Slice(ready, func(i, j int) bool {
			return ready[i].priority > ready[j].priority
		})
	}

	// Step 4: Check for cycle
	if len(result) != len(d.nodes) {
		cycle, _ := d.detectCycleDFS()
		return nil, &CycleError{Path: cycle}
	}

	return result, nil
}

// DetectCycle uses DFS with three-color marking to detect cycles.
// Returns cycle path if found, nil if no cycle.
// Time complexity: O(V + E), Space complexity: O(V)
//
// Returns:
//   - []string: The cycle path if found, nil otherwise.
//   - error: Any error that occurred during detection.
func (d *DAG) DetectCycle() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.detectCycleDFS()
}

// detectCycleDFS performs DFS-based cycle detection with three colors.
// This is a helper method that must be called with the read lock held.
func (d *DAG) detectCycleDFS() ([]string, error) {
	colors := make(map[string]Color, len(d.nodes))
	parent := make(map[string]string, len(d.nodes))

	// Initialize all nodes as White
	for id := range d.nodes {
		colors[id] = White
	}

	// For each unvisited node, perform DFS
	for id := range d.nodes {
		if colors[id] == White {
			if cycle := d.dfsVisit(id, colors, parent); cycle != nil {
				return cycle, nil
			}
		}
	}

	return nil, nil
}

// dfsVisit visits a node and its descendants using DFS.
// This is a helper method that must be called with the read lock held.
func (d *DAG) dfsVisit(nodeID string, colors map[string]Color, parent map[string]string) []string {
	colors[nodeID] = Gray

	// Visit all dependents
	node := d.nodes[nodeID]
	for _, dependentID := range node.OutEdges {
		parent[dependentID] = nodeID

		if colors[dependentID] == Gray {
			// Found a cycle - reconstruct path
			return d.reconstructCyclePath(dependentID, nodeID, parent)
		}

		if colors[dependentID] == White {
			if cycle := d.dfsVisit(dependentID, colors, parent); cycle != nil {
				return cycle
			}
		}
	}

	colors[nodeID] = Black
	return nil
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
