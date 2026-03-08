package beads

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Client wraps the beads CLI commands
type Client struct{}

// NewClient creates a new beads client
func NewClient() *Client {
	return &Client{}
}

// BeadJSON represents the main bead structure from the CLI JSON output
type BeadJSON struct {
	ID                 string       `json:"id"`
	Title              string       `json:"title"`
	Description        *string      `json:"description,omitempty"`
	Status             string       `json:"status"`
	Priority           int32        `json:"priority"`
	IssueType          string       `json:"issue_type"`
	Labels             []string     `json:"labels,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
	Dependencies       []Dependency `json:"dependencies,omitempty"`
	AcceptanceCriteria *string      `json:"acceptance_criteria,omitempty"`
	Assignee           *string      `json:"assignee,omitempty"`
	CloseReason        *string      `json:"close_reason,omitempty"`
	ClosedAt           *time.Time   `json:"closed_at,omitempty"`
	ClosedBySession    *string      `json:"closed_by_session,omitempty"`
	CreatedBy          *string      `json:"created_by,omitempty"`
	DeferUntil         *time.Time   `json:"defer_until,omitempty"`
	Design             *string      `json:"design,omitempty"`
	DueAt              *time.Time   `json:"due_at,omitempty"`
	Ephemeral          bool         `json:"ephemeral,omitempty"`
	EstimatedMinutes   *int32       `json:"estimated_minutes,omitempty"`
	ExternalRef        *string      `json:"external_ref,omitempty"`
	IsTemplate         bool         `json:"is_template,omitempty"`
	Notes              *string      `json:"notes,omitempty"`
	Owner              *string      `json:"owner,omitempty"`
	Pinned             bool         `json:"pinned,omitempty"`
	Sender             *string      `json:"sender,omitempty"`
	SourceRepo         *string      `json:"source_repo,omitempty"`
	SourceSystem       *string      `json:"source_system,omitempty"`
	CompactedAt        *time.Time   `json:"compacted_at,omitempty"`
	CompactedAtCommit  *string      `json:"compacted_at_commit,omitempty"`
	CompactionLevel    *int32       `json:"compaction_level,omitempty"`
	OriginalSize       *int32       `json:"original_size,omitempty"`
	OriginalType       *string      `json:"original_type,omitempty"`
	DeleteReason       *string      `json:"delete_reason,omitempty"`
	DeletedAt          *time.Time   `json:"deleted_at,omitempty"`
	DeletedBy          *string      `json:"deleted_by,omitempty"`
}

// Dependency represents a relationship between two issues
type Dependency struct {
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	DependsOnID string    `json:"depends_on_id"`
	IssueID     string    `json:"issue_id"`
	Metadata    *string   `json:"metadata,omitempty"`
	ThreadID    *string   `json:"thread_id,omitempty"`
	Type        string    `json:"type"`
}

// ReadyBead represents a minimal bead output for the ready command
type ReadyBead struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Description        *string   `json:"description,omitempty"`
	Status             string    `json:"status"`
	Priority           int32     `json:"priority"`
	IssueType          string    `json:"issue_type"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	CreatedBy          *string   `json:"created_by,omitempty"`
	Assignee           *string   `json:"assignee,omitempty"`
	Owner              *string   `json:"owner,omitempty"`
	Notes              *string   `json:"notes,omitempty"`
	AcceptanceCriteria *string   `json:"acceptance_criteria,omitempty"`
	EstimatedMinutes   *int32    `json:"estimated_minutes,omitempty"`
}

// BeadDetail represents a full bead with all details for the show command
type BeadDetail struct {
	BeadJSON
	Dependents []DependentWithMetadata `json:"dependents,omitempty"`
	Events     []Event                 `json:"events,omitempty"`
	Parent     *string                 `json:"parent,omitempty"`
}

// DependentWithMetadata represents a dependent issue with dependency metadata
type DependentWithMetadata struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	Priority       int32  `json:"priority"`
	DependencyType string `json:"dependency_type"`
}

// Event represents an event in the issue's history (audit log)
type Event struct {
	ID        int64     `json:"id"`
	IssueID   string    `json:"issue_id"`
	EventType string    `json:"event_type"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"created_at"`
	OldValue  *string   `json:"old_value,omitempty"`
	NewValue  *string   `json:"new_value,omitempty"`
	Comment   *string   `json:"comment,omitempty"`
}

// BeadWithCounts represents a bead with dependency counts for list/search views
type BeadWithCounts struct {
	BeadJSON
	DependencyCount uint `json:"dependency_count"`
	DependentCount  uint `json:"dependent_count"`
}

// getWorkDir returns the working directory for running br commands
func (c *Client) getWorkDir(projectPath, repoName string) string {
	if repoName != "" {
		return filepath.Join(projectPath, repoName)
	}
	return projectPath
}

// GetReadyBeads retrieves beads that are ready to execute (dependencies satisfied)
func (c *Client) GetReadyBeads(projectPath, repoName string) ([]types.Bead, error) {
	workDir := c.getWorkDir(projectPath, repoName)

	// Execute br ready with JSON output
	cmd := exec.Command("br", "ready", "--json")
	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("br ready --json failed: %w (stderr: %s)", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("br ready --json failed: %w", err)
	}

	// Parse JSON output
	return c.parseReadyJSON(output, projectPath, repoName)
}

// parseReadyJSON parses the JSON output of 'br ready --json'
func (c *Client) parseReadyJSON(output []byte, projectPath, repoName string) ([]types.Bead, error) {
	var readyBeads []ReadyBead
	if err := json.Unmarshal(output, &readyBeads); err != nil {
		return nil, fmt.Errorf("failed to parse ready beads JSON: %w", err)
	}

	beads := make([]types.Bead, len(readyBeads))
	for i, rb := range readyBeads {
		beads[i] = types.Bead{
			ID:          types.BeadID(rb.ID),
			Title:       rb.Title,
			Description: dereferenceString(rb.Description),
			Type:        rb.IssueType,
			Status:      types.BeadStatus(rb.Status),
			Project:     filepath.Base(projectPath),
			Repo:        repoName,
			Labels:      nil, // Not available in ReadyBead
			Priority:    int(rb.Priority),
			CreatedAt:   rb.CreatedAt,
			UpdatedAt:   rb.UpdatedAt,
			Metadata: map[string]string{
				"bead_id": rb.ID,
			},
		}
	}

	return beads, nil
}

// GetBead retrieves full details of a bead by its string ID
func (c *Client) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	workDir := c.getWorkDir(projectPath, repoName)

	cmd := exec.Command("br", "show", beadID.String(), "--json")
	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("br show %s --json failed: %w (stderr: %s)", beadID.String(), err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("br show %s --json failed: %w", beadID.String(), err)
	}

	return c.parseBeadDetailJSON(output, projectPath, repoName)
}

// parseBeadDetailJSON parses the JSON output of 'br show <id> --json'
// Note: br show returns an array with a single element
func (c *Client) parseBeadDetailJSON(output []byte, projectPath, repoName string) (*types.Bead, error) {
	var details []BeadDetail
	if err := json.Unmarshal(output, &details); err != nil {
		return nil, fmt.Errorf("failed to parse bead detail JSON: %w", err)
	}

	if len(details) == 0 {
		return nil, fmt.Errorf("no bead details found in response")
	}

	detail := details[0]

	// Convert dependencies - extract the DependsOnID from each dependency
	dependencies := make([]types.BeadID, 0, len(detail.Dependencies))
	for _, dep := range detail.Dependencies {
		// The DependsOnID contains the ID of the dependency
		dependencies = append(dependencies, types.BeadID(dep.DependsOnID))
	}

	bead := &types.Bead{
		ID:           types.BeadID(detail.ID),
		Title:        detail.Title,
		Description:  dereferenceString(detail.Description),
		Type:         detail.IssueType,
		Status:       types.BeadStatus(detail.Status),
		Dependencies: dependencies,
		Project:      filepath.Base(projectPath),
		Repo:         repoName,
		Labels:       detail.Labels,
		Priority:     int(detail.Priority),
		CreatedAt:    detail.CreatedAt,
		UpdatedAt:    detail.UpdatedAt,
		CompletedAt:  detail.ClosedAt,
		Metadata: map[string]string{
			"bead_id": detail.ID,
		},
	}

	// Add optional metadata
	if detail.CloseReason != nil {
		bead.Metadata["close_reason"] = *detail.CloseReason
	}
	if detail.CreatedBy != nil {
		bead.Metadata["created_by"] = *detail.CreatedBy
	}
	if detail.SourceRepo != nil {
		bead.Metadata["source_repo"] = *detail.SourceRepo
	}

	return bead, nil
}

// CloseBead marks a bead as completed
func (c *Client) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	workDir := c.getWorkDir(projectPath, repoName)

	cmd := exec.Command("br", "close", beadID.String(), "-r", reason)
	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("br close %s failed: %w (stderr: %s)", beadID.String(), err, string(exitErr.Stderr))
		}
		return fmt.Errorf("br close %s failed: %w", beadID.String(), err)
	}

	_ = output // Close command output is typically empty on success
	return nil
}

// ListBeads lists all beads in a project/repo
func (c *Client) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	workDir := c.getWorkDir(projectPath, repoName)

	cmd := exec.Command("br", "list", "--json")
	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("br list --json failed: %w (stderr: %s)", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("br list --json failed: %w", err)
	}

	return c.parseListJSON(output, projectPath, repoName)
}

// parseListJSON parses the JSON output of 'br list --json'
func (c *Client) parseListJSON(output []byte, projectPath, repoName string) ([]types.Bead, error) {
	var beadsWithCounts []BeadWithCounts
	if err := json.Unmarshal(output, &beadsWithCounts); err != nil {
		return nil, fmt.Errorf("failed to parse list JSON: %w", err)
	}

	beads := make([]types.Bead, len(beadsWithCounts))
	for i, bwc := range beadsWithCounts {
		beads[i] = types.Bead{
			ID:          types.BeadID(bwc.ID),
			Title:       bwc.Title,
			Description: dereferenceString(bwc.Description),
			Type:        bwc.IssueType,
			Status:      types.BeadStatus(bwc.Status),
			Project:     filepath.Base(projectPath),
			Repo:        repoName,
			Labels:      bwc.Labels,
			Priority:    int(bwc.Priority),
			CreatedAt:   bwc.CreatedAt,
			UpdatedAt:   bwc.UpdatedAt,
			Metadata: map[string]string{
				"bead_id":          bwc.ID,
				"dependency_count": fmt.Sprintf("%d", bwc.DependencyCount),
				"dependent_count":  fmt.Sprintf("%d", bwc.DependentCount),
			},
		}
	}

	return beads, nil
}

// dereferenceString safely dereferences a string pointer
func dereferenceString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
