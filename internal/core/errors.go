package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// DAGError represents a general error that occurred during a DAG operation.
// It wraps the underlying error and provides context about the operation
// and bead involved.
type DAGError struct {
	// Op is the name of the operation being performed (e.g., "add-edge", "remove-node")
	Op string

	// Bead is the ID of the bead involved in the operation
	Bead string

	// Err is the underlying error that caused the failure
	Err error
}

// Error returns a formatted error message for the DAGError.
// Format: "dag {op} failed for bead {id}: {error}"
func (e *DAGError) Error() string {
	return fmt.Sprintf("dag %s failed for bead %s: %v", e.Op, e.Bead, e.Err)
}

// Unwrap returns the underlying error to support error chaining.
// This allows errors.Is and errors.As to work properly with DAGError.
func (e *DAGError) Unwrap() error {
	return e.Err
}

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

// Unwrap returns nil since CycleError does not wrap another error.
func (e *CycleError) Unwrap() error {
	return nil
}

// IsCycleError checks if the given error is or wraps a CycleError.
// It uses errors.As to traverse the error chain.
func IsCycleError(err error) bool {
	var cycleErr *CycleError
	return errors.As(err, &cycleErr)
}

// NodeNotFoundError represents an error when a requested node
// does not exist in the DAG.
type NodeNotFoundError struct {
	// ID is the identifier of the node that was not found
	ID string
}

// Error returns a formatted error message for the NodeNotFoundError.
// Format: "node {id} not found in DAG"
func (e *NodeNotFoundError) Error() string {
	return fmt.Sprintf("node %s not found in DAG", e.ID)
}

// Unwrap returns nil since NodeNotFoundError does not wrap another error.
func (e *NodeNotFoundError) Unwrap() error {
	return nil
}

// DuplicateNodeError represents an error when attempting to add a node
// that already exists in the DAG.
type DuplicateNodeError struct {
	// ID is the identifier of the duplicate node
	ID string
}

// Error returns a formatted error message for the DuplicateNodeError.
// Format: "node {id} already exists in DAG"
func (e *DuplicateNodeError) Error() string {
	return fmt.Sprintf("node %s already exists in DAG", e.ID)
}

// Unwrap returns nil since DuplicateNodeError does not wrap another error.
func (e *DuplicateNodeError) Unwrap() error {
	return nil
}

// InvalidStateError represents an error when attempting an invalid
// state transition for a bead.
type InvalidStateError struct {
	// ID is the identifier of the bead involved in the invalid transition
	ID string

	// From is the current state of the bead
	From types.BeadStatus

	// To is the attempted target state
	To types.BeadStatus
}

// Error returns a formatted error message for the InvalidStateError.
// Format: "invalid state transition for {id}: {from} → {to}"
func (e *InvalidStateError) Error() string {
	return fmt.Sprintf("invalid state transition for %s: %s → %s", e.ID, e.From, e.To)
}

// Unwrap returns nil since InvalidStateError does not wrap another error.
func (e *InvalidStateError) Unwrap() error {
	return nil
}

// RetryExhaustedError represents an error when a bead has exhausted all retry attempts.
type RetryExhaustedError struct {
	// BeadID is the identifier of the bead that exhausted retries
	BeadID string

	// Attempts is the number of retry attempts made
	Attempts int

	// LastError is the last error that caused the retry
	LastError error
}

// Error returns a formatted error message for the RetryExhaustedError.
// Format: "bead {id} exhausted {attempts} retry attempts: {error}"
func (e *RetryExhaustedError) Error() string {
	return fmt.Sprintf("bead %s exhausted %d retry attempts: %v", e.BeadID, e.Attempts, e.LastError)
}

// Unwrap returns the last error to support error chaining.
func (e *RetryExhaustedError) Unwrap() error {
	return e.LastError
}

// BeadFetchError represents an error when fetching bead information fails.
type BeadFetchError struct {
	// Op is the operation being performed (e.g., "list", "get")
	Op string

	// Project is the project name
	Project string

	// Repo is the repository name (may be empty)
	Repo string

	// Err is the underlying error
	Err error
}

// Error returns a formatted error message for the BeadFetchError.
func (e *BeadFetchError) Error() string {
	if e.Repo != "" {
		return fmt.Sprintf("bead %s failed for %s/%s: %v", e.Op, e.Project, e.Repo, e.Err)
	}
	return fmt.Sprintf("bead %s failed for %s: %v", e.Op, e.Project, e.Err)
}

// Unwrap returns the underlying error to support error chaining.
func (e *BeadFetchError) Unwrap() error {
	return e.Err
}

// WorkerSpawnError represents an error when spawning a worker fails.
type WorkerSpawnError struct {
	// BeadID is the identifier of the bead for which the worker failed
	BeadID string

	// AgentType is the agent type that was requested
	AgentType string

	// Err is the underlying error
	Err error
}

// Error returns a formatted error message for the WorkerSpawnError.
func (e *WorkerSpawnError) Error() string {
	return fmt.Sprintf("failed to spawn %s worker for bead %s: %v", e.AgentType, e.BeadID, e.Err)
}

// Unwrap returns the underlying error to support error chaining.
func (e *WorkerSpawnError) Unwrap() error {
	return e.Err
}

// GracefulDegradationError represents a non-fatal error that the overseer
// can recover from and continue operating.
type GracefulDegradationError struct {
	// Component is the component that experienced the error
	Component string

	// Operation is the operation that failed
	Operation string

	// Err is the underlying error
	Err error

	// Action describes the recovery action taken
	Action string
}

// Error returns a formatted error message for the GracefulDegradationError.
func (e *GracefulDegradationError) Error() string {
	if e.Action != "" {
		return fmt.Sprintf("[%s] %s failed: %v (recovery: %s)", e.Component, e.Operation, e.Err, e.Action)
	}
	return fmt.Sprintf("[%s] %s failed: %v", e.Component, e.Operation, e.Err)
}

// Unwrap returns the underlying error to support error chaining.
func (e *GracefulDegradationError) Unwrap() error {
	return e.Err
}
