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
	ID          string    `json:"id"`
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
// It strips log lines (starting with timestamps) to find the actual JSON
func parseJSON[T any](data []byte, context string) (T, error) {
	var result T

	// Convert to string and split by lines
	str := string(data)
	lines := strings.Split(str, "\n")

	// Find the start of JSON (skip log lines that start with timestamps)
	var jsonStart int
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// JSON starts with [ or {, skip lines starting with timestamps or empty lines
		if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
			jsonStart = i
			break
		}
	}

	// Join remaining lines as JSON
	jsonData := strings.Join(lines[jsonStart:], "\n")

	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		return result, fmt.Errorf("failed to parse %s JSON: %w", context, err)
	}
	return result, nil
}

// createBaseBead converts BeadCore fields to a types.Bead with the given context
func createBaseBead(core BeadCore, ctx BeadContext) types.Bead {
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
		beads[i] = createBaseBead(resp.BeadCore, ctx)
	}
	return beads, nil
}

// metadataSetter defines a function that sets a metadata field if the condition is true
type metadataSetter func()

// setStringIfNotNil sets a metadata field from a string pointer if not nil
func setStringIfNotNil(metadata map[string]string, key string, value *string) {
	if value != nil {
		metadata[key] = *value
	}
}

// setTimeIfNotNil sets a metadata field from a time pointer if not nil
func setTimeIfNotNil(metadata map[string]string, key string, value *time.Time) {
	if value != nil {
		metadata[key] = value.Format(time.RFC3339)
	}
}

// setBoolIfNotNil sets a metadata field from a bool pointer if not nil
func setBoolIfNotNil(metadata map[string]string, key string, value *bool) {
	if value != nil {
		metadata[key] = fmt.Sprintf("%t", *value)
	}
}

// setIntIfNotNil sets a metadata field from an int pointer if not nil
func setIntIfNotNil(metadata map[string]string, key string, value *int) {
	if value != nil {
		metadata[key] = fmt.Sprintf("%d", *value)
	}
}

// extractDependencies converts dependency JSON to bead dependencies and metadata
func extractDependencies(deps []Dependency, metadata map[string]string) []types.BeadID {
	dependencies := make([]types.BeadID, len(deps))
	for i, dep := range deps {
		dependencies[i] = types.BeadID(dep.DependsOnID)
		if dep.Status != "" {
			metadata[dep.DependsOnID+"_status"] = dep.Status
		}
		if dep.Type != "" {
			metadata[dep.DependsOnID+"_relationship"] = dep.Type
		}
	}
	return dependencies
}

// extractEventMetadata extracts event information into metadata
func extractEventMetadata(events []BeadEvent, metadata map[string]string) {
	if len(events) == 0 {
		return
	}
	metadata["event_count"] = fmt.Sprintf("%d", len(events))
	lastEvent := events[len(events)-1]
	metadata["last_event_type"] = lastEvent.EventType
	metadata["last_event_actor"] = lastEvent.Actor
}

// extractDependentMetadata extracts dependent information into metadata
func extractDependentMetadata(dependents []DependentInfo, metadata map[string]string) {
	if len(dependents) == 0 {
		return
	}
	metadata["dependent_count"] = fmt.Sprintf("%d", len(dependents))
	dependentIDs := make([]string, len(dependents))
	for i, dep := range dependents {
		dependentIDs[i] = dep.ID
	}
	metadata["dependent_ids"] = strings.Join(dependentIDs, ",")
}

// setMetadataFromDetail populates all metadata fields from a BeadDetail
func setMetadataFromDetail(resp BeadDetail, metadata map[string]string) {
	// Assignment and ownership
	setStringIfNotNil(metadata, "close_reason", resp.CloseReason)
	setStringIfNotNil(metadata, "created_by", resp.CreatedBy)
	setStringIfNotNil(metadata, "assignee", resp.Assignee)
	setStringIfNotNil(metadata, "owner", resp.Owner)
	setStringIfNotNil(metadata, "notes", resp.Notes)

	// Source information
	setStringIfNotNil(metadata, "source_repo", resp.SourceRepo)
	setStringIfNotNil(metadata, "source_system", resp.SourceSystem)

	// Work fields
	setIntIfNotNil(metadata, "estimated_minutes", resp.EstimatedMinutes)
	setStringIfNotNil(metadata, "external_ref", resp.ExternalRef)

	// Design and acceptance criteria
	setStringIfNotNil(metadata, "design", resp.Design)
	setStringIfNotNil(metadata, "acceptance_criteria", resp.AcceptanceCriteria)

	// Scheduling
	setTimeIfNotNil(metadata, "due_at", resp.DueAt)
	setTimeIfNotNil(metadata, "defer_until", resp.DeferUntil)

	// Flags
	setBoolIfNotNil(metadata, "pinned", resp.Pinned)
	setBoolIfNotNil(metadata, "ephemeral", resp.Ephemeral)

	// Events and dependents
	extractEventMetadata(resp.Events, metadata)
	extractDependentMetadata(resp.Dependents, metadata)
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
	bead := createBaseBead(resp.BeadCore, ctx)

	// Extract dependencies with their status and relationship type
	bead.Dependencies = extractDependencies(resp.Dependencies, bead.Metadata)

	// Set completion time
	bead.CompletedAt = resp.ClosedAt

	// Populate all metadata fields
	setMetadataFromDetail(resp, bead.Metadata)

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
		bead := createBaseBead(resp.BeadCore, ctx)
		bead.Metadata["dependency_count"] = fmt.Sprintf("%d", resp.DependencyCount)
		bead.Metadata["dependent_count"] = fmt.Sprintf("%d", resp.DependentCount)
		beads[i] = bead
	}
	return beads, nil
}
