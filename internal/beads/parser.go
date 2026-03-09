package beads

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadContext provides context for parsing beads from a specific project/repo
type BeadContext struct {
	Project string
	Repo    string
}

// BeadCore contains the common fields present in all bead JSON responses
type BeadCore struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description *string   `json:"description"`
	Status      string    `json:"status"`
	Priority    int       `json:"priority"`
	IssueType   string    `json:"issue_type"`
	Labels      []string  `json:"labels"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ReadyBead represents a bead from 'br ready --json' output
type ReadyBead struct {
	BeadCore
	CreatedBy *string `json:"created_by"`
	Assignee  *string `json:"assignee"`
	Owner     *string `json:"owner"`
	Notes     *string `json:"notes"`
}

// Dependency represents a bead dependency relationship
type Dependency struct {
	ID          int64     `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	IssueID     string    `json:"issue_id"`
	DependsOnID string    `json:"depends_on_id"`
	Type        string    `json:"type"`
	Metadata    *string   `json:"metadata"`
	Status      string    `json:"status"`
}

// BeadSummary represents a bead with dependency counts from 'br list --json'
type BeadSummary struct {
	BeadCore
	DependencyCount int `json:"dependency_count"`
	DependentCount  int `json:"dependent_count"`
}

// DependentInfo represents a dependent bead with dependency metadata
type DependentInfo struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	Priority       int32  `json:"priority"`
	DependencyType string `json:"dependency_type"`
}

// BeadEvent represents a bead lifecycle event
type BeadEvent struct {
	ID        int64     `json:"id"`
	IssueID   string    `json:"issue_id"`
	EventType string    `json:"event_type"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"created_at"`
}

// BeadDetail represents full bead details from 'br show <id> --json'
type BeadDetail struct {
	BeadCore
	Dependencies       []Dependency    `json:"dependencies"`
	CloseReason        *string         `json:"close_reason"`
	ClosedAt           *time.Time      `json:"closed_at"`
	CreatedBy          *string         `json:"created_by"`
	Assignee           *string         `json:"assignee"`
	Owner              *string         `json:"owner"`
	SourceRepo         *string         `json:"source_repo"`
	SourceSystem       *string         `json:"source_system"`
	DueAt              *time.Time      `json:"due_at"`
	DeferUntil         *time.Time      `json:"defer_until"`
	EstimatedMinutes   *int            `json:"estimated_minutes"`
	ExternalRef        *string         `json:"external_ref"`
	Notes              *string         `json:"notes"`
	Design             *string         `json:"design"`
	AcceptanceCriteria *string         `json:"acceptance_criteria"`
	Pinned             *bool           `json:"pinned"`
	Ephemeral          *bool           `json:"ephemeral"`
	Dependents         []DependentInfo `json:"dependents"`
	Events             []BeadEvent     `json:"events"`
}

// Backwards compatibility type aliases
type BeadJSON = BeadCore
type DependentWithMetadata = DependentInfo
type Event = BeadEvent

// Parser handles parsing of beads CLI JSON output
type Parser struct{}

// NewParser creates a new parser instance
func NewParser() *Parser {
	return &Parser{}
}

// derefString safely dereferences a string pointer, returning empty string if nil
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// parseJSON unmarshals JSON into the target type with consistent error handling
func parseJSON[T any](data []byte, context string) (T, error) {
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("failed to parse %s JSON: %w", context, err)
	}
	return result, nil
}

// coreToBead converts BeadCore fields to a types.Bead with the given context
func coreToBead(core BeadCore, ctx BeadContext) types.Bead {
	return types.Bead{
		ID:          types.BeadID(core.ID),
		Title:       core.Title,
		Description: derefString(core.Description),
		Type:        core.IssueType,
		Status:      types.BeadStatus(core.Status),
		Project:     ctx.Project,
		Repo:        ctx.Repo,
		Labels:      core.Labels,
		Priority:    core.Priority,
		CreatedAt:   core.CreatedAt,
		UpdatedAt:   core.UpdatedAt,
		Metadata:    map[string]string{"bead_id": core.ID},
	}
}

// ParseReadyBeads parses JSON output from 'br ready --json' command
func (p *Parser) ParseReadyBeads(output []byte, ctx BeadContext) ([]types.Bead, error) {
	responses, err := parseJSON[[]ReadyBead](output, "ready beads")
	if err != nil {
		return nil, err
	}

	beads := make([]types.Bead, len(responses))
	for i, resp := range responses {
		beads[i] = coreToBead(resp.BeadCore, ctx)
	}
	return beads, nil
}

// ParseBeadDetail parses JSON output from 'br show <id> --json' command
func (p *Parser) ParseBeadDetail(output []byte, ctx BeadContext) (*types.Bead, error) {
	responses, err := parseJSON[[]BeadDetail](output, "bead detail")
	if err != nil {
		return nil, err
	}

	if len(responses) == 0 {
		return nil, fmt.Errorf("no bead details found in response")
	}

	resp := responses[0]
	bead := coreToBead(resp.BeadCore, ctx)

	// Extract dependencies, their status, and relationship type
	bead.Dependencies = make([]types.BeadID, len(resp.Dependencies))
	for i, dep := range resp.Dependencies {
		bead.Dependencies[i] = types.BeadID(dep.DependsOnID)
		if dep.Status != "" {
			bead.Metadata[dep.DependsOnID+"_status"] = dep.Status
		}
		if dep.Type != "" {
			bead.Metadata[dep.DependsOnID+"_relationship"] = dep.Type
		}
	}

	// Set completion time
	bead.CompletedAt = resp.ClosedAt

	// Add optional metadata
	if resp.CloseReason != nil {
		bead.Metadata["close_reason"] = *resp.CloseReason
	}
	if resp.CreatedBy != nil {
		bead.Metadata["created_by"] = *resp.CreatedBy
	}
	if resp.SourceRepo != nil {
		bead.Metadata["source_repo"] = *resp.SourceRepo
	}
	if resp.Assignee != nil {
		bead.Metadata["assignee"] = *resp.Assignee
	}
	if resp.Owner != nil {
		bead.Metadata["owner"] = *resp.Owner
	}
	if resp.Notes != nil {
		bead.Metadata["notes"] = *resp.Notes
	}
	if resp.SourceSystem != nil {
		bead.Metadata["source_system"] = *resp.SourceSystem
	}
	if resp.EstimatedMinutes != nil {
		bead.Metadata["estimated_minutes"] = fmt.Sprintf("%d", *resp.EstimatedMinutes)
	}
	if resp.Design != nil {
		bead.Metadata["design"] = *resp.Design
	}
	if resp.AcceptanceCriteria != nil {
		bead.Metadata["acceptance_criteria"] = *resp.AcceptanceCriteria
	}
	if resp.DueAt != nil {
		bead.Metadata["due_at"] = resp.DueAt.Format(time.RFC3339)
	}
	if resp.DeferUntil != nil {
		bead.Metadata["defer_until"] = resp.DeferUntil.Format(time.RFC3339)
	}
	if resp.ExternalRef != nil {
		bead.Metadata["external_ref"] = *resp.ExternalRef
	}
	if resp.Pinned != nil {
		bead.Metadata["pinned"] = fmt.Sprintf("%t", *resp.Pinned)
	}
	if resp.Ephemeral != nil {
		bead.Metadata["ephemeral"] = fmt.Sprintf("%t", *resp.Ephemeral)
	}
	if len(resp.Events) > 0 {
		bead.Metadata["event_count"] = fmt.Sprintf("%d", len(resp.Events))
		// Extract last event information
		lastEvent := resp.Events[len(resp.Events)-1]
		bead.Metadata["last_event_type"] = lastEvent.EventType
		bead.Metadata["last_event_actor"] = lastEvent.Actor
	}

	// Add dependent information
	if len(resp.Dependents) > 0 {
		bead.Metadata["dependent_count"] = fmt.Sprintf("%d", len(resp.Dependents))
		dependentIDs := make([]string, len(resp.Dependents))
		for i, dep := range resp.Dependents {
			dependentIDs[i] = dep.ID
		}
		bead.Metadata["dependent_ids"] = strings.Join(dependentIDs, ",")
	}

	return &bead, nil
}

// ParseBeadList parses JSON output from 'br list --json' command
func (p *Parser) ParseBeadList(output []byte, ctx BeadContext) ([]types.Bead, error) {
	responses, err := parseJSON[[]BeadSummary](output, "bead list")
	if err != nil {
		return nil, err
	}

	beads := make([]types.Bead, len(responses))
	for i, resp := range responses {
		bead := coreToBead(resp.BeadCore, ctx)
		bead.Metadata["dependency_count"] = fmt.Sprintf("%d", resp.DependencyCount)
		bead.Metadata["dependent_count"] = fmt.Sprintf("%d", resp.DependentCount)
		beads[i] = bead
	}
	return beads, nil
}
