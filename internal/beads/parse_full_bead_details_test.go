package beads

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// =============================================================================
// RED PHASE TESTS: Parse Full Bead Details
// These tests are designed to FAIL initially (RED phase)
// They test functionality that needs to be implemented
// =============================================================================

// TestParseFullBeadDetails_EventsMetadata tests that events are properly
// parsed and stored in metadata (currently not implemented)
func TestParseFullBeadDetails_EventsMetadata(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-events",
		"title": "Bead with Events",
		"description": "Testing events parsing",
		"status": "closed",
		"priority": 3,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T14:00:00Z",
		"events": [
			{
				"id": 1,
				"issue_id": "test-events",
				"event_type": "created",
				"actor": "user123",
				"created_at": "2026-03-09T10:00:00Z"
			},
			{
				"id": 2,
				"issue_id": "test-events",
				"event_type": "status_changed",
				"actor": "user456",
				"created_at": "2026-03-09T12:00:00Z",
				"old_value": "open",
				"new_value": "in_progress"
			},
			{
				"id": 3,
				"issue_id": "test-events",
				"event_type": "closed",
				"actor": "user789",
				"created_at": "2026-03-09T14:00:00Z",
				"comment": "Work completed successfully"
			}
		]
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: Event count should be stored in metadata
	if bead.Metadata["event_count"] != "3" {
		t.Errorf("Metadata['event_count'] = %q, want %q", bead.Metadata["event_count"], "3")
	}

	// Test: Last event type should be stored
	if bead.Metadata["last_event_type"] != "closed" {
		t.Errorf("Metadata['last_event_type'] = %q, want %q", bead.Metadata["last_event_type"], "closed")
	}

	// Test: Last event actor should be stored
	if bead.Metadata["last_event_actor"] != "user789" {
		t.Errorf("Metadata['last_event_actor'] = %q, want %q", bead.Metadata["last_event_actor"], "user789")
	}
}

// TestParseFullBeadDetails_DependentsMetadata tests that dependents are
// properly parsed and stored in metadata (currently not implemented)
func TestParseFullBeadDetails_DependentsMetadata(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-dependents",
		"title": "Bead with Dependents",
		"description": "Testing dependents parsing",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"dependents": [
			{
				"id": "dependent-1",
				"title": "Dependent Task 1",
				"status": "open",
				"priority": 2,
				"dependency_type": "blocks"
			},
			{
				"id": "dependent-2",
				"title": "Dependent Task 2",
				"status": "in_progress",
				"priority": 3,
				"dependency_type": "requires"
			}
		]
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: Dependent count should be stored
	if bead.Metadata["dependent_count"] != "2" {
		t.Errorf("Metadata['dependent_count'] = %q, want %q", bead.Metadata["dependent_count"], "2")
	}

	// Test: Dependent IDs should be stored as comma-separated list
	expectedDependents := "dependent-1,dependent-2"
	if bead.Metadata["dependent_ids"] != expectedDependents {
		t.Errorf("Metadata['dependent_ids'] = %q, want %q", bead.Metadata["dependent_ids"], expectedDependents)
	}
}

// TestParseFullBeadDetails_DependencyStatusMetadata tests that dependency
// status is extracted and stored for each dependency
func TestParseFullBeadDetails_DependencyStatusMetadata(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-deps",
		"title": "Bead with Dependency Status",
		"description": "Testing dependency status parsing",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"dependencies": [
			{
				"depends_on_id": "dep-123",
				"issue_id": "test-deps",
				"type": "blocks",
				"created_at": "2026-03-09T09:00:00Z",
				"status": "completed"
			},
			{
				"depends_on_id": "dep-456",
				"issue_id": "test-deps",
				"type": "requires",
				"created_at": "2026-03-09T09:00:00Z",
				"status": "open"
			},
			{
				"depends_on_id": "dep-789",
				"issue_id": "test-deps",
				"type": "related_to",
				"created_at": "2026-03-09T09:00:00Z",
				"status": "in_progress"
			}
		]
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: Each dependency should have its status in metadata
	testCases := []struct {
		key      string
		expected string
	}{
		{"dep-123_status", "completed"},
		{"dep-456_status", "open"},
		{"dep-789_status", "in_progress"},
	}

	for _, tc := range testCases {
		t.Run(tc.key, func(t *testing.T) {
			if bead.Metadata[tc.key] != tc.expected {
				t.Errorf("Metadata[%q] = %q, want %q", tc.key, bead.Metadata[tc.key], tc.expected)
			}
		})
	}
}

// TestParseFullBeadDetails_OptionalTimeFields tests parsing of optional
// time-related fields like due_at and defer_until
func TestParseFullBeadDetails_OptionalTimeFields(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-times",
		"title": "Bead with Time Fields",
		"description": "Testing time fields parsing",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T11:00:00Z",
		"closed_at": "2026-03-09T12:00:00Z",
		"due_at": "2026-03-15T00:00:00Z",
		"defer_until": "2026-03-10T00:00:00Z"
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: due_at should be stored in metadata
	if bead.Metadata["due_at"] != "2026-03-15T00:00:00Z" {
		t.Errorf("Metadata['due_at'] = %q, want %q", bead.Metadata["due_at"], "2026-03-15T00:00:00Z")
	}

	// Test: defer_until should be stored in metadata
	if bead.Metadata["defer_until"] != "2026-03-10T00:00:00Z" {
		t.Errorf("Metadata['defer_until'] = %q, want %q", bead.Metadata["defer_until"], "2026-03-10T00:00:00Z")
	}

	// Test: CompletedAt should be set from closed_at
	if bead.CompletedAt == nil {
		t.Error("CompletedAt is nil, expected it to be set from closed_at")
	} else {
		expectedTime := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
		if !bead.CompletedAt.Equal(expectedTime) {
			t.Errorf("CompletedAt = %v, want %v", *bead.CompletedAt, expectedTime)
		}
	}
}

// TestParseFullBeadDetails_AssignmentFields tests parsing of assignment
// related fields like assignee, owner, and created_by
func TestParseFullBeadDetails_AssignmentFields(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-assignment",
		"title": "Bead with Assignment Fields",
		"description": "Testing assignment fields parsing",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"created_by": "user123",
		"assignee": "dev456",
		"owner": "team-backend"
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: created_by should be stored
	if bead.Metadata["created_by"] != "user123" {
		t.Errorf("Metadata['created_by'] = %q, want %q", bead.Metadata["created_by"], "user123")
	}

	// Test: assignee should be stored
	if bead.Metadata["assignee"] != "dev456" {
		t.Errorf("Metadata['assignee'] = %q, want %q", bead.Metadata["assignee"], "dev456")
	}

	// Test: owner should be stored
	if bead.Metadata["owner"] != "team-backend" {
		t.Errorf("Metadata['owner'] = %q, want %q", bead.Metadata["owner"], "team-backend")
	}
}

// TestParseFullBeadDetails_WorkFields tests parsing of work-related fields
// like estimated_minutes, external_ref, and notes
func TestParseFullBeadDetails_WorkFields(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-work",
		"title": "Bead with Work Fields",
		"description": "Testing work fields parsing",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"estimated_minutes": 480,
		"external_ref": "JIRA-123",
		"source_system": "jira",
		"notes": "Important implementation notes"
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: estimated_minutes should be stored
	if bead.Metadata["estimated_minutes"] != "480" {
		t.Errorf("Metadata['estimated_minutes'] = %q, want %q", bead.Metadata["estimated_minutes"], "480")
	}

	// Test: external_ref should be stored
	if bead.Metadata["external_ref"] != "JIRA-123" {
		t.Errorf("Metadata['external_ref'] = %q, want %q", bead.Metadata["external_ref"], "JIRA-123")
	}

	// Test: source_system should be stored
	if bead.Metadata["source_system"] != "jira" {
		t.Errorf("Metadata['source_system'] = %q, want %q", bead.Metadata["source_system"], "jira")
	}

	// Test: notes should be stored
	if bead.Metadata["notes"] != "Important implementation notes" {
		t.Errorf("Metadata['notes'] = %q, want %q", bead.Metadata["notes"], "Important implementation notes")
	}
}

// TestParseFullBeadDetails_DesignAndACFields tests parsing of design
// and acceptance criteria fields
func TestParseFullBeadDetails_DesignAndACFields(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-design",
		"title": "Bead with Design and AC",
		"description": "Testing design and AC fields parsing",
		"status": "open",
		"priority": 5,
		"issue_type": "feature",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"design": "https://figma.com/design123",
		"acceptance_criteria": "1. Must pass all tests\n2. Must have 80% coverage"
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: design should be stored
	if bead.Metadata["design"] != "https://figma.com/design123" {
		t.Errorf("Metadata['design'] = %q, want %q", bead.Metadata["design"], "https://figma.com/design123")
	}

	// Test: acceptance_criteria should be stored
	expectedAC := "1. Must pass all tests\n2. Must have 80% coverage"
	if bead.Metadata["acceptance_criteria"] != expectedAC {
		t.Errorf("Metadata['acceptance_criteria'] = %q, want %q", bead.Metadata["acceptance_criteria"], expectedAC)
	}
}

// TestParseFullBeadDetails_FlagFields tests parsing of boolean flag fields
// like pinned and ephemeral
func TestParseFullBeadDetails_FlagFields(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-flags",
		"title": "Bead with Flags",
		"description": "Testing flag fields parsing",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"pinned": true,
		"ephemeral": false
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: pinned should be stored
	if bead.Metadata["pinned"] != "true" {
		t.Errorf("Metadata['pinned'] = %q, want %q", bead.Metadata["pinned"], "true")
	}

	// Test: ephemeral should be stored
	if bead.Metadata["ephemeral"] != "false" {
		t.Errorf("Metadata['ephemeral'] = %q, want %q", bead.Metadata["ephemeral"], "false")
	}
}

// TestParseFullBeadDetails_AllFieldsTogether tests parsing a bead with ALL
// possible fields present simultaneously
func TestParseFullBeadDetails_AllFieldsTogether(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "hydra", Repo: "backend"}

	jsonOutput := []byte(`[{
		"id": "hydra-backend-abc123",
		"title": "Complete Full Featured Bead",
		"description": "A bead with every possible field populated",
		"status": "closed",
		"priority": 10,
		"issue_type": "feature",
		"labels": ["urgent", "backend", "api", "v2"],
		"created_at": "2026-03-01T08:00:00Z",
		"updated_at": "2026-03-09T16:00:00Z",
		"closed_at": "2026-03-09T16:00:00Z",
		"close_reason": "completed",
		"created_by": "product-manager",
		"assignee": "senior-dev",
		"owner": "platform-team",
		"due_at": "2026-03-10T17:00:00Z",
		"defer_until": "2026-03-05T00:00:00Z",
		"estimated_minutes": 2880,
		"external_ref": "PROJ-456",
		"source_repo": "backend",
		"source_system": "linear",
		"notes": "This is a critical feature for Q1",
		"design": "https://figma.com/file/ABC123",
		"acceptance_criteria": "1. API response time < 100ms\n2. 100% test coverage\n3. Documentation complete",
		"pinned": true,
		"ephemeral": false,
		"dependencies": [
			{
				"depends_on_id": "hydra-backend-dep1",
				"issue_id": "hydra-backend-abc123",
				"type": "blocks",
				"created_at": "2026-03-01T07:00:00Z",
				"status": "completed"
			},
			{
				"depends_on_id": "hydra-backend-dep2",
				"issue_id": "hydra-backend-abc123",
				"type": "requires",
				"created_at": "2026-03-01T07:00:00Z",
				"status": "in_progress"
			}
		],
		"dependents": [
			{
				"id": "hydra-backend-child1",
				"title": "Child Task 1",
				"status": "open",
				"priority": 5,
				"dependency_type": "blocks"
			}
		],
		"events": [
			{
				"id": 1,
				"issue_id": "hydra-backend-abc123",
				"event_type": "created",
				"actor": "product-manager",
				"created_at": "2026-03-01T08:00:00Z"
			},
			{
				"id": 2,
				"issue_id": "hydra-backend-abc123",
				"event_type": "closed",
				"actor": "senior-dev",
				"created_at": "2026-03-09T16:00:00Z",
				"comment": "All acceptance criteria met"
			}
		]
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Verify all basic fields
	if bead.ID != types.BeadID("hydra-backend-abc123") {
		t.Errorf("ID = %q, want %q", bead.ID, "hydra-backend-abc123")
	}
	if bead.Title != "Complete Full Featured Bead" {
		t.Errorf("Title = %q, want %q", bead.Title, "Complete Full Featured Bead")
	}
	if bead.Type != "feature" {
		t.Errorf("Type = %q, want %q", bead.Type, "feature")
	}
	if bead.Status != types.BeadStatus("closed") {
		t.Errorf("Status = %q, want %q", bead.Status, "closed")
	}
	if bead.Priority != 10 {
		t.Errorf("Priority = %d, want %d", bead.Priority, 10)
	}
	if len(bead.Labels) != 4 {
		t.Errorf("len(Labels) = %d, want 4", len(bead.Labels))
	}
	if len(bead.Dependencies) != 2 {
		t.Errorf("len(Dependencies) = %d, want 2", len(bead.Dependencies))
	}

	// Verify all metadata fields
	metadataChecks := map[string]string{
		"close_reason":                  "completed",
		"created_by":                    "product-manager",
		"assignee":                      "senior-dev",
		"owner":                         "platform-team",
		"due_at":                        "2026-03-10T17:00:00Z",
		"defer_until":                   "2026-03-05T00:00:00Z",
		"estimated_minutes":             "2880",
		"external_ref":                  "PROJ-456",
		"source_repo":                   "backend",
		"source_system":                 "linear",
		"notes":                         "This is a critical feature for Q1",
		"design":                        "https://figma.com/file/ABC123",
		"pinned":                        "true",
		"ephemeral":                     "false",
		"event_count":                   "2",
		"last_event_type":               "closed",
		"last_event_actor":              "senior-dev",
		"dependent_count":               "1",
		"dependent_ids":                 "hydra-backend-child1",
		"dep-hydra-backend-dep1_status": "completed",
		"dep-hydra-backend-dep2_status": "in_progress",
	}

	for key, expected := range metadataChecks {
		t.Run("metadata_"+key, func(t *testing.T) {
			if bead.Metadata[key] != expected {
				t.Errorf("Metadata[%q] = %q, want %q", key, bead.Metadata[key], expected)
			}
		})
	}

	// Verify acceptance criteria with multiline content
	expectedAC := "1. API response time < 100ms\n2. 100% test coverage\n3. Documentation complete"
	if bead.Metadata["acceptance_criteria"] != expectedAC {
		t.Errorf("Metadata['acceptance_criteria'] = %q, want %q", bead.Metadata["acceptance_criteria"], expectedAC)
	}

	// Verify completed time
	if bead.CompletedAt == nil {
		t.Error("CompletedAt is nil, expected it to be set")
	} else {
		expectedTime := time.Date(2026, 3, 9, 16, 0, 0, 0, time.UTC)
		if !bead.CompletedAt.Equal(expectedTime) {
			t.Errorf("CompletedAt = %v, want %v", *bead.CompletedAt, expectedTime)
		}
	}
}

// TestParseFullBeadDetails_MissingOptionalFields tests that parsing succeeds
// when optional fields are missing
func TestParseFullBeadDetails_MissingOptionalFields(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-minimal",
		"title": "Minimal Bead",
		"description": "Only required fields",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z"
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Basic fields should be set
	if bead.ID != types.BeadID("test-minimal") {
		t.Errorf("ID = %q, want %q", bead.ID, "test-minimal")
	}

	// Optional fields should have empty/zero values
	if bead.CompletedAt != nil {
		t.Error("CompletedAt should be nil for open bead")
	}

	if len(bead.Labels) != 0 {
		t.Errorf("Labels should be empty, got %v", bead.Labels)
	}

	if len(bead.Dependencies) != 0 {
		t.Errorf("Dependencies should be empty, got %v", bead.Dependencies)
	}
}

// TestParseFullBeadDetails_InvalidJSON tests error handling for invalid JSON
func TestParseFullBeadDetails_InvalidJSON(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	invalidJSON := []byte(`{invalid json`)

	bead, err := parser.ParseBeadDetail(invalidJSON, ctx)

	if err == nil {
		t.Error("ParseBeadDetail() expected error for invalid JSON, got nil")
	}

	if bead != nil {
		t.Error("ParseBeadDetail() should return nil bead when there's an error")
	}

	// Verify error message contains expected text
	if err != nil && !containsString(err.Error(), "failed to parse") {
		t.Errorf("Error message should contain 'failed to parse', got: %v", err)
	}
}

// TestParseFullBeadDetails_EmptyArray tests error handling for empty array response
func TestParseFullBeadDetails_EmptyArray(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	emptyArray := []byte(`[]`)

	bead, err := parser.ParseBeadDetail(emptyArray, ctx)

	if err == nil {
		t.Error("ParseBeadDetail() expected error for empty array, got nil")
	}

	if bead != nil {
		t.Error("ParseBeadDetail() should return nil bead when there's an error")
	}

	// Verify error message
	if err != nil && !containsString(err.Error(), "no bead details found") {
		t.Errorf("Error message should contain 'no bead details found', got: %v", err)
	}
}

// TestParseFullBeadDetails_NullFields tests handling of explicit null values
func TestParseFullBeadDetails_NullFields(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-nulls",
		"title": "Bead with Nulls",
		"description": null,
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"closed_at": null,
		"close_reason": null,
		"created_by": null,
		"assignee": null
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Description should be empty string when null
	if bead.Description != "" {
		t.Errorf("Description = %q, want empty string for null", bead.Description)
	}

	// CompletedAt should be nil when closed_at is null
	if bead.CompletedAt != nil {
		t.Error("CompletedAt should be nil when closed_at is null")
	}

	// Null metadata fields should not be present
	if _, exists := bead.Metadata["close_reason"]; exists {
		t.Error("Metadata should not contain 'close_reason' when null")
	}

	if _, exists := bead.Metadata["created_by"]; exists {
		t.Error("Metadata should not contain 'created_by' when null")
	}
}

// TestParseFullBeadDetails_LargeInput tests parsing of large bead descriptions
func TestParseFullBeadDetails_LargeInput(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	// Create a large description
	largeDesc := ""
	for i := 0; i < 1000; i++ {
		largeDesc += "This is a line of text that makes the description very long. "
	}

	// Create JSON with the large description
	beadData := map[string]interface{}{
		"id":          "test-large",
		"title":       "Large Bead",
		"description": largeDesc,
		"status":      "open",
		"priority":    5,
		"issue_type":  "task",
		"created_at":  "2026-03-09T10:00:00Z",
		"updated_at":  "2026-03-09T10:00:00Z",
	}

	jsonBytes, err := json.Marshal([]map[string]interface{}{beadData})
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}

	bead, err := parser.ParseBeadDetail(jsonBytes, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Verify the large description was parsed correctly
	if bead.Description != largeDesc {
		t.Errorf("Description length = %d, want %d", len(bead.Description), len(largeDesc))
	}
}

// TestParseFullBeadDetails_SpecialCharacters tests handling of special
// characters in string fields
func TestParseFullBeadDetails_SpecialCharacters(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-special",
		"title": "Title with \"quotes\" and \t tabs \n newlines",
		"description": "Description with emojis 🎉 🚀 and unicode 中文",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"notes": "Notes with <html> & \"special\" 'chars'"
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Verify special characters are preserved
	expectedTitle := "Title with \"quotes\" and \t tabs \n newlines"
	if bead.Title != expectedTitle {
		t.Errorf("Title = %q, want %q", bead.Title, expectedTitle)
	}

	expectedDesc := "Description with emojis 🎉 🚀 and unicode 中文"
	if bead.Description != expectedDesc {
		t.Errorf("Description = %q, want %q", bead.Description, expectedDesc)
	}

	expectedNotes := "Notes with <html> & \"special\" 'chars'"
	if bead.Metadata["notes"] != expectedNotes {
		t.Errorf("Metadata['notes'] = %q, want %q", bead.Metadata["notes"], expectedNotes)
	}
}

// TestParseFullBeadDetails_DependencyRelationshipTypes tests that dependency
// relationship types are stored in metadata
func TestParseFullBeadDetails_DependencyRelationshipTypes(t *testing.T) {
	parser := NewParser()
	ctx := BeadContext{Project: "test-project", Repo: "test-repo"}

	jsonOutput := []byte(`[{
		"id": "test-rel-types",
		"title": "Bead with Relationship Types",
		"description": "Testing relationship type storage",
		"status": "open",
		"priority": 5,
		"issue_type": "task",
		"created_at": "2026-03-09T10:00:00Z",
		"updated_at": "2026-03-09T10:00:00Z",
		"dependencies": [
			{
				"depends_on_id": "dep-blocks",
				"issue_id": "test-rel-types",
				"type": "blocks",
				"created_at": "2026-03-09T09:00:00Z",
				"status": "completed"
			},
			{
				"depends_on_id": "dep-requires",
				"issue_id": "test-rel-types",
				"type": "requires",
				"created_at": "2026-03-09T09:00:00Z",
				"status": "open"
			}
		]
	}]`)

	bead, err := parser.ParseBeadDetail(jsonOutput, ctx)

	if err != nil {
		t.Fatalf("ParseBeadDetail() unexpected error = %v", err)
	}

	if bead == nil {
		t.Fatal("ParseBeadDetail() returned nil bead")
	}

	// Test: Dependency relationship types should be stored
	if bead.Metadata["dep-blocks_relationship"] != "blocks" {
		t.Errorf("Metadata['dep-blocks_relationship'] = %q, want %q",
			bead.Metadata["dep-blocks_relationship"], "blocks")
	}

	if bead.Metadata["dep-requires_relationship"] != "requires" {
		t.Errorf("Metadata['dep-requires_relationship'] = %q, want %q",
			bead.Metadata["dep-requires_relationship"], "requires")
	}
}
