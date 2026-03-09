package types

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BeadID is a unique identifier for a bead, stored as a string in the format
// "project-id" for single-repo projects or "project/repo-id" for multi-repo.
// The ID portion is a base36-encoded string (e.g., "hydra-zxc", "myproject-abc123").
type BeadID string

// String returns the string representation of the BeadID.
// This is the canonical format used for serialization and display.
func (b BeadID) String() string {
	return string(b)
}

// Project extracts the project name from the BeadID.
// For single-repo: "myproject-abc" → "myproject"
// For multi-repo: "myproject/backend-abc" → "myproject"
func (b BeadID) Project() string {
	s := string(b)
	if idx := strings.Index(s, "/"); idx != -1 {
		return s[:idx]
	}
	if idx := strings.LastIndex(s, "-"); idx != -1 {
		return s[:idx]
	}
	return s
}

// Repo extracts the repository name from the BeadID (if multi-repo).
// For single-repo: "myproject-abc" → ""
// For multi-repo: "myproject/backend-abc" → "backend"
func (b BeadID) Repo() string {
	s := string(b)
	// Check if it's a multi-repo format (contains /)
	if slashIdx := strings.Index(s, "/"); slashIdx != -1 {
		repoPart := s[slashIdx+1:]
		// Extract repo name (everything before the last dash)
		if dashIdx := strings.LastIndex(repoPart, "-"); dashIdx != -1 {
			return repoPart[:dashIdx]
		}
		return repoPart
	}
	return ""
}

// ID returns the base36-encoded identifier portion of the BeadID.
// For single-repo: "myproject-abc123" → "abc123"
// For multi-repo: "myproject/backend-abc123" → "abc123"
func (b BeadID) ID() string {
	s := string(b)
	// Handle multi-repo format first
	if slashIdx := strings.Index(s, "/"); slashIdx != -1 {
		s = s[slashIdx+1:]
	}
	// Extract ID from the last dash
	if dashIdx := strings.LastIndex(s, "-"); dashIdx != -1 {
		return s[dashIdx+1:]
	}
	return s
}

// Number attempts to parse the ID as an integer for backward compatibility
// with legacy numeric bead IDs. Returns an error if parsing fails.
// Note: Modern beads use base36 string IDs that may not be valid integers.
func (b BeadID) Number() (int, error) {
	idStr := b.ID()
	num, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, fmt.Errorf("bead ID %q is not a numeric ID: %w", idStr, err)
	}
	return num, nil
}

// MustNumber returns the numeric ID or panics if not a valid integer.
// Use with caution - only when you know the ID is numeric.
func (b BeadID) MustNumber() int {
	num, err := b.Number()
	if err != nil {
		panic(err)
	}
	return num
}

// ParseBeadID parses a string into a BeadID.
// Accepts formats:
//   - "project-id" (single-repo, e.g., "hydra-zxc")
//   - "project/repo-id" (multi-repo, e.g., "hydra/backend-abc")
//   - Legacy: "project-123" (single-repo numeric)
//   - Legacy: "project/repo-123" (multi-repo numeric)
func ParseBeadID(s string) (BeadID, error) {
	if s == "" {
		return "", fmt.Errorf("bead ID cannot be empty")
	}

	// Validate format: must have at least one dash to separate project/repo from ID
	if !strings.Contains(s, "-") {
		return "", fmt.Errorf("invalid bead ID format %q: must contain a dash separator", s)
	}

	return BeadID(s), nil
}

// NewBeadID creates a new BeadID from project, repo, and ID components.
// If repo is empty, creates single-repo format: "project-id"
// If repo is provided, creates multi-repo format: "project/repo-id"
func NewBeadID(project, repo, id string) BeadID {
	if repo != "" {
		return BeadID(fmt.Sprintf("%s/%s-%s", project, repo, id))
	}
	return BeadID(fmt.Sprintf("%s-%s", project, id))
}

// MarshalJSON implements json.Marshaler for BeadID.
func (b BeadID) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(b))
}

// UnmarshalJSON implements json.Unmarshaler for BeadID.
func (b *BeadID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	beadID, err := ParseBeadID(s)
	if err != nil {
		return err
	}
	*b = beadID
	return nil
}

// BeadStatus represents the current state of a bead.
type BeadStatus string

const (
	// StatusOpen indicates the bead is open and ready to be worked on.
	StatusOpen BeadStatus = "open"
	// StatusInProgress indicates the bead is currently being worked on.
	StatusInProgress BeadStatus = "in_progress"
	// StatusCompleted indicates the bead has been successfully completed.
	StatusCompleted BeadStatus = "completed"
	// StatusClosed indicates the bead has been closed (synonym for completed).
	StatusClosed BeadStatus = "closed"
	// StatusFailed indicates the bead has failed.
	StatusFailed BeadStatus = "failed"
	// StatusBlocked indicates the bead is blocked by dependencies.
	StatusBlocked BeadStatus = "blocked"
)

// IsTerminal returns true if the status is a terminal state (completed, closed, or failed).
func (s BeadStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusClosed || s == StatusFailed
}

// Bead represents a single task in the workflow.
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

// RepoType indicates whether a project is single or multi-repo.
type RepoType string

const (
	// RepoTypeSingle indicates a single-repository project.
	RepoTypeSingle RepoType = "single"
	// RepoTypeMulti indicates a multi-repository project.
	RepoTypeMulti RepoType = "multi"
)

// Project represents a software project that contains beads.
type Project struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Type     RepoType `json:"type"`            // single or multi
	Repos    []Repo   `json:"repos,omitempty"` // For multi-repo projects
	Priority int      `json:"priority"`
}

// Repo represents a single repository within a multi-repo project.
type Repo struct {
	Name string `json:"name"`
	Path string `json:"path"` // Relative to project root
}

// AgentType represents the type of agent to spawn for a bead.
type AgentType string

// Known agent types.
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

// AgentTypeMap maps bead types to agent types.
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

// DetermineAgentTypes returns the agent types for a given bead type.
// Returns orchestrator as default if bead type is unknown.
func DetermineAgentTypes(beadType string) []AgentType {
	if agents, ok := AgentTypeMap[beadType]; ok {
		return agents
	}
	// Default to orchestrator
	return []AgentType{AgentOrchestrator}
}

// Worker represents an active worker process.
type Worker struct {
	ID          string    `json:"id"`
	BeadID      BeadID    `json:"bead_id"`
	PID         int       `json:"pid"`
	AgentType   AgentType `json:"agent_type"`
	StartTime   time.Time `json:"start_time"`
	Status      string    `json:"status"`
	ProjectPath string    `json:"project_path"` // Path to project (for closing beads)
}

// LegacyBeadID represents the old BeadID structure for backward compatibility.
// Deprecated: Use BeadID (string type) instead.
type LegacyBeadID struct {
	Project string `json:"project"`
	Repo    string `json:"repo"`
	Number  int    `json:"number"`
}

// String returns the string representation of a LegacyBeadID.
// Format: "project/repo-number" for multi-repo, "project-number" for single.
func (b LegacyBeadID) String() string {
	if b.Repo != "" {
		return fmt.Sprintf("%s/%s-%d", b.Project, b.Repo, b.Number)
	}
	return fmt.Sprintf("%s-%d", b.Project, b.Number)
}

// ToBeadID converts a LegacyBeadID to the new BeadID string format.
// Note: This uses the numeric ID directly, which works for legacy numeric IDs.
// For new base36-encoded IDs, use NewBeadID instead.
func (b LegacyBeadID) ToBeadID() BeadID {
	id := strconv.Itoa(b.Number)
	return NewBeadID(b.Project, b.Repo, id)
}

// LegacyBeadIDFromBeadID extracts components from a BeadID into a LegacyBeadID.
// This is useful for backward compatibility with code expecting the old struct format.
// Note: Returns an error if the ID is not numeric.
func LegacyBeadIDFromBeadID(b BeadID) (LegacyBeadID, error) {
	num, err := b.Number()
	if err != nil {
		return LegacyBeadID{}, err
	}
	return LegacyBeadID{
		Project: b.Project(),
		Repo:    b.Repo(),
		Number:  num,
	}, nil
}

// NewNumericBeadID creates a BeadID with a numeric identifier.
// This is a convenience function for backward compatibility with legacy numeric IDs.
func NewNumericBeadID(project, repo string, number int) BeadID {
	id := strconv.Itoa(number)
	return NewBeadID(project, repo, id)
}

// BeadFromLegacyBeadID creates a BeadID from project path, repo name, and numeric ID.
// This is a convenience function for the beads client that parses legacy output.
func BeadIDFromLegacyParams(projectPath, repoName string, number int) BeadID {
	project := filepath.Base(projectPath)
	id := strconv.Itoa(number)
	return NewBeadID(project, repoName, id)
}

// IsTestBead returns true if the bead type indicates testing work.
func IsTestBead(beadType string) bool {
	return beadType == "test" || beadType == "test-validate"
}

// IsFixBead returns true if the bead type indicates bug fixing work.
func IsFixBead(beadType string) bool {
	return beadType == "fix" || beadType == "bug"
}

// IsReviewBead returns true if the bead type indicates code review work.
func IsReviewBead(beadType string) bool {
	return beadType == "review"
}

// WorkerStatus represents the current state of a worker.
type WorkerStatus string

const (
	// WorkerStatusRunning indicates the worker is currently executing.
	WorkerStatusRunning WorkerStatus = "running"
	// WorkerStatusCompleted indicates the worker finished successfully.
	WorkerStatusCompleted WorkerStatus = "completed"
	// WorkerStatusFailed indicates the worker failed permanently.
	WorkerStatusFailed WorkerStatus = "failed"
	// WorkerStatusRetryPending indicates the worker failed but will be retried.
	WorkerStatusRetryPending WorkerStatus = "retry_pending"
)
