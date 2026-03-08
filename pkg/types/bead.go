package types

import (
	"fmt"
	"time"
)

// BeadID represents a unique identifier for a bead
type BeadID struct {
	Project string // e.g., "my-project"
	Repo    string // e.g., "backend" (empty for single repo)
	Number  int    // e.g., 123
}

// String returns the string representation of a BeadID
// Format: "project/repo-123" for multi-repo, "project-123" for single
func (b BeadID) String() string {
	if b.Repo != "" {
		return fmt.Sprintf("%s/%s-%d", b.Project, b.Repo, b.Number)
	}
	return fmt.Sprintf("%s-%d", b.Project, b.Number)
}

// ParseBeadID parses a string into a BeadID
func ParseBeadID(s string) (BeadID, error) {
	// TODO: Implement parsing logic
	return BeadID{}, fmt.Errorf("not implemented")
}

// Bead represents a single task in the workflow
type Bead struct {
	ID           BeadID            `json:"id"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Type         string            `json:"type"` // requirements, test, implement, fix, review, etc.
	Status       BeadStatus        `json:"status"`
	Dependencies []BeadID          `json:"dependencies"`
	Project      string            `json:"project"`
	Repo         string            `json:"repo"` // For multi-repo projects
	Labels       []string          `json:"labels"`
	Priority     int               `json:"priority"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
	Metadata     map[string]string `json:"metadata"`
}

// BeadStatus represents the current state of a bead
type BeadStatus string

const (
	StatusOpen       BeadStatus = "open"
	StatusInProgress BeadStatus = "in_progress"
	StatusCompleted  BeadStatus = "completed"
	StatusFailed     BeadStatus = "failed"
	StatusBlocked    BeadStatus = "blocked"
)

// IsTerminal returns true if the status is a terminal state
func (s BeadStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed
}

// Project represents a software project that contains beads
type Project struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Type     RepoType `json:"type"`            // single or multi
	Repos    []Repo   `json:"repos,omitempty"` // For multi-repo projects
	Priority int      `json:"priority"`
}

// RepoType indicates whether a project is single or multi-repo
type RepoType string

const (
	RepoTypeSingle RepoType = "single"
	RepoTypeMulti  RepoType = "multi"
)

// Repo represents a single repository within a multi-repo project
type Repo struct {
	Name string `json:"name"`
	Path string `json:"path"` // Relative to project root
}

// AgentType represents the type of agent to spawn for a bead
type AgentType string

// Known agent types
const (
	AgentOrchestrator         AgentType = "orchestrator"
	AgentPRD                  AgentType = "prd"
	AgentTestSpecialist       AgentType = "test-specialist"
	AgentTestValidator        AgentType = "test-validator"
	AgentFixSpecialist        AgentType = "fix-specialist"
	AgentCodeReviewer         AgentType = "code-reviewer"
	AgentAdversarialReviewer  AgentType = "adversarial-reviewer"
	AgentDeploymentSpecialist AgentType = "deployment-specialist"
	AgentHuman                AgentType = "human" // Special: approval gates
)

// AgentTypeMap maps bead types to agent types
var AgentTypeMap = map[string][]AgentType{
	"requirements":  {AgentOrchestrator, AgentPRD},
	"test":          {AgentTestSpecialist},
	"test-validate": {AgentTestValidator},
	"implement":     {AgentOrchestrator},
	"refactor":      {AgentOrchestrator, AgentFixSpecialist},
	"review":        {AgentCodeReviewer, AgentAdversarialReviewer},
	"deploy":        {AgentDeploymentSpecialist},
	"fix":           {AgentFixSpecialist},
	"bug":           {AgentFixSpecialist},
	"approval-gate": {AgentHuman},
}

// DetermineAgentTypes returns the agent types for a given bead type
func DetermineAgentTypes(beadType string) []AgentType {
	if agents, ok := AgentTypeMap[beadType]; ok {
		return agents
	}
	// Default to orchestrator
	return []AgentType{AgentOrchestrator}
}

// Worker represents an active worker process
type Worker struct {
	ID        string    `json:"id"`
	BeadID    BeadID    `json:"bead_id"`
	PID       int       `json:"pid"`
	AgentType AgentType `json:"agent_type"`
	StartTime time.Time `json:"start_time"`
	Status    string    `json:"status"`
}
