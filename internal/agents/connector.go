package agents

import (
	"context"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// AgentConnector defines the interface for agent operations
// This abstraction allows Hydra to work with different agents (Pi, Claude, etc.)
type AgentConnector interface {
	// Start initializes the agent connection/session
	Start(ctx context.Context, bead types.Bead, opts AgentOptions) (*AgentSession, error)

	// Execute sends a prompt/task to the agent and waits for completion
	Execute(ctx context.Context, session *AgentSession, prompt string) (*AgentResult, error)

	// Abort forcibly stops an agent session
	Abort(ctx context.Context, session *AgentSession) error

	// Status returns the current status of an agent session
	Status(ctx context.Context, session *AgentSession) (AgentStatus, error)

	// Cleanup performs any necessary cleanup when session ends
	Cleanup(ctx context.Context, session *AgentSession) error

	// IsHealthy checks if the agent connector is functioning
	IsHealthy() bool
}

// AgentOptions contains configuration for starting an agent
type AgentOptions struct {
	ProjectPath string
	Timeout     time.Duration
	AgentType   string
	Model       string
	Provider    string
	// Intelligence context for pattern injection
	Patterns     []Pattern
	AntiPatterns []AntiPattern
}

// Pattern represents a learned pattern
type Pattern struct {
	ID         string
	Title      string
	Content    string
	Confidence float64
}

// AntiPattern represents patterns to avoid
type AntiPattern struct {
	ID      string
	Title   string
	Content string
}

// AgentSession represents an active agent session
type AgentSession struct {
	ID        string
	BeadID    types.BeadID
	PID       int
	PGID      int // Process Group ID for cleanup
	StartTime time.Time
	connector AgentConnector
	metadata  map[string]interface{}
}

// AgentResult contains the outcome of agent execution
type AgentResult struct {
	Success  bool
	ExitCode int
	Output   string
	Error    string
	Duration time.Duration
	Metadata map[string]interface{}
}

// AgentStatus represents the current state of an agent session
type AgentStatus struct {
	State        AgentState
	Progress     float64
	Message      string
	LastActivity time.Time
}

type AgentState int

const (
	AgentStateUnknown AgentState = iota
	AgentStateStarting
	AgentStateRunning
	AgentStateCompleted
	AgentStateFailed
	AgentStateAborted
)


