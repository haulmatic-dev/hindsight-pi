package beads

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// CLIExecutor is the interface for executing CLI commands
type CLIExecutor interface {
	Execute(name string, arg ...string) ([]byte, error)
}

// defaultExecutor implements CLIExecutor using actual exec.Command
type defaultExecutor struct{}

func (e *defaultExecutor) Execute(name string, arg ...string) ([]byte, error) {
	// This will be used by the actual client
	// For testing, we use the mockExecutor
	return nil, fmt.Errorf("default executor should not be called in tests")
}

// TestableClient is a beads client that can use a custom CLI executor
type TestableClient struct {
	executor CLIExecutor
}

// NewTestableClient creates a new testable beads client with a custom executor
func NewTestableClient(executor CLIExecutor) *TestableClient {
	return &TestableClient{executor: executor}
}

// GetReadyBeads retrieves beads that are ready to execute (dependencies satisfied)
func (c *TestableClient) GetReadyBeads(projectPath, repoName string) ([]types.Bead, error) {
	output, err := c.executor.Execute("br", "ready", "--json")
	if err != nil {
		return nil, fmt.Errorf("br ready --json failed: %w", err)
	}

	return c.parseReadyJSON(output, projectPath, repoName)
}

// parseReadyJSON parses the JSON output of 'br ready --json'
func (c *TestableClient) parseReadyJSON(output []byte, projectPath, repoName string) ([]types.Bead, error) {
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
			Project:     projectPath,
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
func (c *TestableClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	output, err := c.executor.Execute("br", "show", beadID.String(), "--json")
	if err != nil {
		return nil, fmt.Errorf("br show %s --json failed: %w", beadID.String(), err)
	}

	return c.parseBeadDetailJSON(output, projectPath, repoName)
}

// parseBeadDetailJSON parses the JSON output of 'br show <id> --json'
func (c *TestableClient) parseBeadDetailJSON(output []byte, projectPath, repoName string) (*types.Bead, error) {
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
		dependencies = append(dependencies, types.BeadID(dep.DependsOnID))
	}

	bead := &types.Bead{
		ID:           types.BeadID(detail.ID),
		Title:        detail.Title,
		Description:  dereferenceString(detail.Description),
		Type:         detail.IssueType,
		Status:       types.BeadStatus(detail.Status),
		Dependencies: dependencies,
		Project:      projectPath,
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

// ListBeads lists all beads in a project/repo
func (c *TestableClient) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	output, err := c.executor.Execute("br", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("br list --json failed: %w", err)
	}

	return c.parseListJSON(output, projectPath, repoName)
}

// parseListJSON parses the JSON output of 'br list --json'
func (c *TestableClient) parseListJSON(output []byte, projectPath, repoName string) ([]types.Bead, error) {
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
			Project:     projectPath,
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

// CloseBead marks a bead as completed
func (c *TestableClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	_, err := c.executor.Execute("br", "close", beadID.String(), "-m", reason)
	if err != nil {
		return fmt.Errorf("br close %s failed: %w", beadID.String(), err)
	}

	return nil
}

// mockResponse represents a mocked CLI response
type mockResponse struct {
	Output []byte
	Err    error
}

// mockExecutor implements CLIExecutor for testing
type mockExecutor struct {
	responses map[string]mockResponse
	calls     []struct {
		Name string
		Args []string
	}
}

// NewMockExecutor creates a new mock executor
func NewMockExecutor() *mockExecutor {
	return &mockExecutor{
		responses: make(map[string]mockResponse),
		calls: make([]struct {
			Name string
			Args []string
		}, 0),
	}
}

// AddResponse adds a mock response for a specific command
func (m *mockExecutor) AddResponse(name string, args []string, output []byte, err error) {
	key := m.key(name, args)
	m.responses[key] = mockResponse{Output: output, Err: err}
}

// Execute implements CLIExecutor
func (m *mockExecutor) Execute(name string, arg ...string) ([]byte, error) {
	m.calls = append(m.calls, struct {
		Name string
		Args []string
	}{Name: name, Args: arg})

	key := m.key(name, arg)
	if resp, ok := m.responses[key]; ok {
		return resp.Output, resp.Err
	}

	// Return empty response if not found
	return []byte{}, nil
}

// GetCalls returns all calls made to the executor
func (m *mockExecutor) GetCalls() []struct {
	Name string
	Args []string
} {
	return m.calls
}

// key generates a unique key for a command
func (m *mockExecutor) key(name string, args []string) string {
	key := name
	for _, arg := range args {
		key += " " + arg
	}
	return key
}

// createTestTime creates a time.Time for testing
func createTestTime(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func TestGetReadyBeads_Success(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	// Create test data
	createdAt := createTestTime(2025, 1, 15)
	updatedAt := createTestTime(2025, 1, 20)
	description := "Test bead description"

	readyBeads := []ReadyBead{
		{
			ID:          "hydra-abc123",
			Title:       "Implement feature X",
			Description: &description,
			Status:      "open",
			Priority:    1,
			IssueType:   "implement",
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
			CreatedBy:   strPtr("user@example.com"),
			Assignee:    strPtr("developer@example.com"),
			Owner:       strPtr("team-lead@example.com"),
			Notes:       strPtr("Some notes"),
		},
		{
			ID:        "hydra-def456",
			Title:     "Fix bug Y",
			Status:    "open",
			Priority:  2,
			IssueType: "fix",
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		},
	}

	jsonData, err := json.Marshal(readyBeads)
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}

	mockExec.AddResponse("br", []string{"ready", "--json"}, jsonData, nil)

	beads, err := client.GetReadyBeads("/projects/hydra", "")
	if err != nil {
		t.Fatalf("GetReadyBeads failed: %v", err)
	}

	// Verify correct number of beads returned
	if len(beads) != 2 {
		t.Errorf("Expected 2 beads, got %d", len(beads))
	}

	// Verify first bead fields
	bead1 := beads[0]
	if bead1.ID != "hydra-abc123" {
		t.Errorf("Expected ID 'hydra-abc123', got '%s'", bead1.ID)
	}
	if bead1.Title != "Implement feature X" {
		t.Errorf("Expected title 'Implement feature X', got '%s'", bead1.Title)
	}
	if bead1.Description != "Test bead description" {
		t.Errorf("Expected description 'Test bead description', got '%s'", bead1.Description)
	}
	if bead1.Type != "implement" {
		t.Errorf("Expected type 'implement', got '%s'", bead1.Type)
	}
	if bead1.Status != types.StatusOpen {
		t.Errorf("Expected status 'open', got '%s'", bead1.Status)
	}
	if bead1.Priority != 1 {
		t.Errorf("Expected priority 1, got %d", bead1.Priority)
	}
	if !bead1.CreatedAt.Equal(createdAt) {
		t.Errorf("Expected CreatedAt %v, got %v", createdAt, bead1.CreatedAt)
	}
	if !bead1.UpdatedAt.Equal(updatedAt) {
		t.Errorf("Expected UpdatedAt %v, got %v", updatedAt, bead1.UpdatedAt)
	}

	// Verify second bead
	bead2 := beads[1]
	if bead2.ID != "hydra-def456" {
		t.Errorf("Expected ID 'hydra-def456', got '%s'", bead2.ID)
	}
	if bead2.Description != "" {
		t.Errorf("Expected empty description for second bead, got '%s'", bead2.Description)
	}

	// Verify correct command was called
	calls := mockExec.GetCalls()
	if len(calls) != 1 {
		t.Errorf("Expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "br" {
		t.Errorf("Expected command 'br', got '%s'", calls[0].Name)
	}
	if len(calls[0].Args) != 2 || calls[0].Args[0] != "ready" || calls[0].Args[1] != "--json" {
		t.Errorf("Expected args ['ready', '--json'], got %v", calls[0].Args)
	}
}

func TestGetReadyBeads_CLIError(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	// Simulate CLI error with stderr
	cliErr := errors.New("exit status 1: bead not found")
	mockExec.AddResponse("br", []string{"ready", "--json"}, []byte{}, cliErr)

	beads, err := client.GetReadyBeads("/projects/hydra", "")

	// Verify error is returned
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify error message contains CLI error
	if err.Error() != "br ready --json failed: exit status 1: bead not found" {
		t.Errorf("Expected specific error message, got: %v", err)
	}

	// Verify no beads returned
	if beads != nil {
		t.Errorf("Expected nil beads on error, got %v", beads)
	}
}

func TestGetReadyBeads_InvalidJSON(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	// Return invalid JSON
	mockExec.AddResponse("br", []string{"ready", "--json"}, []byte("invalid json"), nil)

	beads, err := client.GetReadyBeads("/projects/hydra", "")

	// Verify error is returned
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}

	// Verify error is about JSON parsing
	expectedErrMsg := "failed to parse ready beads JSON"
	if err.Error()[:len(expectedErrMsg)] != expectedErrMsg {
		t.Errorf("Expected JSON parsing error, got: %v", err)
	}

	if beads != nil {
		t.Errorf("Expected nil beads on error, got %v", beads)
	}
}

func TestGetReadyBeads_EmptyResult(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	// Return empty array
	mockExec.AddResponse("br", []string{"ready", "--json"}, []byte("[]"), nil)

	beads, err := client.GetReadyBeads("/projects/hydra", "")
	if err != nil {
		t.Fatalf("GetReadyBeads failed for empty result: %v", err)
	}

	// Verify empty slice returned
	if beads == nil {
		t.Error("Expected empty slice, got nil")
	}
	if len(beads) != 0 {
		t.Errorf("Expected 0 beads, got %d", len(beads))
	}
}

func TestGetBead_Success(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	createdAt := createTestTime(2025, 1, 15)
	updatedAt := createTestTime(2025, 1, 20)
	closedAt := createTestTime(2025, 1, 25)
	description := "Detailed bead description"
	closeReason := "completed"
	createdBy := "user@example.com"
	sourceRepo := "github.com/haulmatic-dev/hydra"

	beadDetail := []BeadDetail{
		{
			BeadJSON: BeadJSON{
				ID:          "hydra-abc123",
				Title:       "Complete implementation",
				Description: &description,
				Status:      "completed",
				Priority:    1,
				IssueType:   "implement",
				Labels:      []string{"urgent", "backend"},
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAt,
				Dependencies: []Dependency{
					{
						CreatedAt:   createdAt,
						DependsOnID: "hydra-dep001",
						IssueID:     "hydra-abc123",
						Type:        "blocks",
					},
					{
						CreatedAt:   createdAt,
						DependsOnID: "hydra-dep002",
						IssueID:     "hydra-abc123",
						Type:        "relates_to",
						Metadata:    strPtr("{\"related_field\": \"value\"}"),
					},
				},
				CloseReason: &closeReason,
				ClosedAt:    &closedAt,
				CreatedBy:   &createdBy,
				SourceRepo:  &sourceRepo,
			},
			Dependents: []DependentWithMetadata{
				{
					ID:             "hydra-child001",
					Title:          "Dependent task",
					Status:         "open",
					Priority:       2,
					DependencyType: "blocked_by",
				},
			},
			Events: []Event{
				{
					ID:        1,
					IssueID:   "hydra-abc123",
					EventType: "created",
					Actor:     "user@example.com",
					CreatedAt: createdAt,
				},
			},
		},
	}

	jsonData, err := json.Marshal(beadDetail)
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}

	mockExec.AddResponse("br", []string{"show", "hydra-abc123", "--json"}, jsonData, nil)

	bead, err := client.GetBead("hydra", "", "hydra-abc123")
	if err != nil {
		t.Fatalf("GetBead failed: %v", err)
	}

	// Verify basic fields
	if bead.ID != "hydra-abc123" {
		t.Errorf("Expected ID 'hydra-abc123', got '%s'", bead.ID)
	}
	if bead.Title != "Complete implementation" {
		t.Errorf("Expected title 'Complete implementation', got '%s'", bead.Title)
	}
	if bead.Description != description {
		t.Errorf("Expected description '%s', got '%s'", description, bead.Description)
	}
	if bead.Type != "implement" {
		t.Errorf("Expected type 'implement', got '%s'", bead.Type)
	}
	if bead.Status != types.BeadStatus("completed") {
		t.Errorf("Expected status 'completed', got '%s'", bead.Status)
	}
	if bead.Priority != 1 {
		t.Errorf("Expected priority 1, got %d", bead.Priority)
	}

	// Verify dependencies parsed correctly
	if len(bead.Dependencies) != 2 {
		t.Errorf("Expected 2 dependencies, got %d", len(bead.Dependencies))
	}
	if bead.Dependencies[0] != "hydra-dep001" {
		t.Errorf("Expected first dependency 'hydra-dep001', got '%s'", bead.Dependencies[0])
	}
	if bead.Dependencies[1] != "hydra-dep002" {
		t.Errorf("Expected second dependency 'hydra-dep002', got '%s'", bead.Dependencies[1])
	}

	// Verify labels
	if len(bead.Labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(bead.Labels))
	}
	if bead.Labels[0] != "urgent" || bead.Labels[1] != "backend" {
		t.Errorf("Expected labels ['urgent', 'backend'], got %v", bead.Labels)
	}

	// Verify metadata extracted
	if bead.Metadata["bead_id"] != "hydra-abc123" {
		t.Errorf("Expected metadata bead_id 'hydra-abc123', got '%s'", bead.Metadata["bead_id"])
	}
	if bead.Metadata["close_reason"] != "completed" {
		t.Errorf("Expected metadata close_reason 'completed', got '%s'", bead.Metadata["close_reason"])
	}
	if bead.Metadata["created_by"] != "user@example.com" {
		t.Errorf("Expected metadata created_by 'user@example.com', got '%s'", bead.Metadata["created_by"])
	}
	if bead.Metadata["source_repo"] != sourceRepo {
		t.Errorf("Expected metadata source_repo '%s', got '%s'", sourceRepo, bead.Metadata["source_repo"])
	}

	// Verify completed at time
	if bead.CompletedAt == nil || !bead.CompletedAt.Equal(closedAt) {
		t.Errorf("Expected CompletedAt %v, got %v", closedAt, bead.CompletedAt)
	}

	// Verify command was called with correct arguments
	calls := mockExec.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("Expected 1 call, got %d", len(calls))
	}
	if len(calls[0].Args) != 3 || calls[0].Args[0] != "show" || calls[0].Args[1] != "hydra-abc123" || calls[0].Args[2] != "--json" {
		t.Errorf("Expected args ['show', 'hydra-abc123', '--json'], got %v", calls[0].Args)
	}
}

func TestGetBead_NotFound(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	// Simulate not found error
	cliErr := errors.New("exit status 1: bead not found")
	mockExec.AddResponse("br", []string{"show", "nonexistent-123", "--json"}, []byte{}, cliErr)

	bead, err := client.GetBead("hydra", "", "nonexistent-123")

	// Verify error is returned
	if err == nil {
		t.Fatal("Expected error for not found bead, got nil")
	}

	// Verify error message
	expectedMsg := "br show nonexistent-123 --json failed: exit status 1: bead not found"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error '%s', got '%s'", expectedMsg, err.Error())
	}

	if bead != nil {
		t.Errorf("Expected nil bead on error, got %v", bead)
	}
}

func TestGetBead_EmptyResponse(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	// Return empty array
	mockExec.AddResponse("br", []string{"show", "hydra-abc123", "--json"}, []byte("[]"), nil)

	bead, err := client.GetBead("hydra", "", "hydra-abc123")

	// Verify error is returned for empty response
	if err == nil {
		t.Fatal("Expected error for empty response, got nil")
	}

	if err.Error() != "no bead details found in response" {
		t.Errorf("Expected 'no bead details found in response' error, got: %v", err)
	}

	if bead != nil {
		t.Errorf("Expected nil bead on error, got %v", bead)
	}
}

func TestGetBead_InvalidJSON(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	mockExec.AddResponse("br", []string{"show", "hydra-abc123", "--json"}, []byte("not valid json"), nil)

	bead, err := client.GetBead("hydra", "", "hydra-abc123")

	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}

	expectedMsg := "failed to parse bead detail JSON"
	if err.Error()[:len(expectedMsg)] != expectedMsg {
		t.Errorf("Expected JSON parsing error, got: %v", err)
	}

	if bead != nil {
		t.Errorf("Expected nil bead on error, got %v", bead)
	}
}

func TestGetBead_NoDependencies(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	createdAt := createTestTime(2025, 1, 15)
	updatedAt := createTestTime(2025, 1, 20)

	beadDetail := []BeadDetail{
		{
			BeadJSON: BeadJSON{
				ID:           "hydra-nodeps",
				Title:        "Bead with no dependencies",
				Status:       "open",
				Priority:     1,
				IssueType:    "test",
				CreatedAt:    createdAt,
				UpdatedAt:    updatedAt,
				Dependencies: []Dependency{}, // Empty dependencies
				Labels:       []string{},
			},
		},
	}

	jsonData, _ := json.Marshal(beadDetail)
	mockExec.AddResponse("br", []string{"show", "hydra-nodeps", "--json"}, jsonData, nil)

	bead, err := client.GetBead("hydra", "", "hydra-nodeps")
	if err != nil {
		t.Fatalf("GetBead failed: %v", err)
	}

	// Verify empty dependencies slice (not nil)
	if bead.Dependencies == nil {
		t.Error("Expected empty dependencies slice, got nil")
	}
	if len(bead.Dependencies) != 0 {
		t.Errorf("Expected 0 dependencies, got %d", len(bead.Dependencies))
	}
}

func TestListBeads_Success(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	createdAt := createTestTime(2025, 1, 15)
	updatedAt := createTestTime(2025, 1, 20)
	description1 := "First bead description"
	description2 := "Second bead description"

	beadsWithCounts := []BeadWithCounts{
		{
			BeadJSON: BeadJSON{
				ID:          "hydra-abc123",
				Title:       "First bead",
				Description: &description1,
				Status:      "open",
				Priority:    1,
				IssueType:   "implement",
				Labels:      []string{"backend", "urgent"},
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAt,
			},
			DependencyCount: 2,
			DependentCount:  1,
		},
		{
			BeadJSON: BeadJSON{
				ID:          "hydra-def456",
				Title:       "Second bead",
				Description: &description2,
				Status:      "in_progress",
				Priority:    2,
				IssueType:   "test",
				Labels:      []string{"frontend"},
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAt,
			},
			DependencyCount: 0,
			DependentCount:  0,
		},
		{
			BeadJSON: BeadJSON{
				ID:        "hydra-ghi789",
				Title:     "Third bead",
				Status:    "completed",
				Priority:  3,
				IssueType: "review",
				Labels:    []string{},
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
			DependencyCount: 5,
			DependentCount:  2,
		},
	}

	jsonData, err := json.Marshal(beadsWithCounts)
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}

	mockExec.AddResponse("br", []string{"list", "--json"}, jsonData, nil)

	beads, err := client.ListBeads("hydra", "")
	if err != nil {
		t.Fatalf("ListBeads failed: %v", err)
	}

	// Verify all beads parsed
	if len(beads) != 3 {
		t.Errorf("Expected 3 beads, got %d", len(beads))
	}

	// Verify first bead
	bead1 := beads[0]
	if bead1.ID != "hydra-abc123" {
		t.Errorf("Expected first bead ID 'hydra-abc123', got '%s'", bead1.ID)
	}
	if bead1.Title != "First bead" {
		t.Errorf("Expected first bead title 'First bead', got '%s'", bead1.Title)
	}
	if bead1.Status != types.StatusOpen {
		t.Errorf("Expected first bead status 'open', got '%s'", bead1.Status)
	}
	if bead1.Priority != 1 {
		t.Errorf("Expected first bead priority 1, got %d", bead1.Priority)
	}
	if len(bead1.Labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(bead1.Labels))
	}

	// Verify counts in metadata
	if bead1.Metadata["dependency_count"] != "2" {
		t.Errorf("Expected dependency_count '2', got '%s'", bead1.Metadata["dependency_count"])
	}
	if bead1.Metadata["dependent_count"] != "1" {
		t.Errorf("Expected dependent_count '1', got '%s'", bead1.Metadata["dependent_count"])
	}

	// Verify second bead
	bead2 := beads[1]
	if bead2.ID != "hydra-def456" {
		t.Errorf("Expected second bead ID 'hydra-def456', got '%s'", bead2.ID)
	}
	if bead2.Status != types.StatusInProgress {
		t.Errorf("Expected second bead status 'in_progress', got '%s'", bead2.Status)
	}
	if bead2.Priority != 2 {
		t.Errorf("Expected second bead priority 2, got %d", bead2.Priority)
	}
	if bead2.Metadata["dependency_count"] != "0" {
		t.Errorf("Expected dependency_count '0', got '%s'", bead2.Metadata["dependency_count"])
	}

	// Verify third bead
	bead3 := beads[2]
	if bead3.ID != "hydra-ghi789" {
		t.Errorf("Expected third bead ID 'hydra-ghi789', got '%s'", bead3.ID)
	}
	if bead3.Status != types.BeadStatus("completed") {
		t.Errorf("Expected third bead status 'completed', got '%s'", bead3.Status)
	}
	if bead3.Metadata["dependency_count"] != "5" {
		t.Errorf("Expected dependency_count '5', got '%s'", bead3.Metadata["dependency_count"])
	}

	// Verify command was called
	calls := mockExec.GetCalls()
	if len(calls) != 1 {
		t.Errorf("Expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "br" {
		t.Errorf("Expected command 'br', got '%s'", calls[0].Name)
	}
	if len(calls[0].Args) != 2 || calls[0].Args[0] != "list" || calls[0].Args[1] != "--json" {
		t.Errorf("Expected args ['list', '--json'], got %v", calls[0].Args)
	}
}

func TestListBeads_CLIError(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	cliErr := errors.New("exit status 1: database locked")
	mockExec.AddResponse("br", []string{"list", "--json"}, []byte{}, cliErr)

	beads, err := client.ListBeads("hydra", "")

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	expectedMsg := "br list --json failed: exit status 1: database locked"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error '%s', got '%s'", expectedMsg, err.Error())
	}

	if beads != nil {
		t.Errorf("Expected nil beads on error, got %v", beads)
	}
}

func TestListBeads_EmptyResult(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	mockExec.AddResponse("br", []string{"list", "--json"}, []byte("[]"), nil)

	beads, err := client.ListBeads("hydra", "")
	if err != nil {
		t.Fatalf("ListBeads failed for empty result: %v", err)
	}

	if beads == nil {
		t.Error("Expected empty slice, got nil")
	}
	if len(beads) != 0 {
		t.Errorf("Expected 0 beads, got %d", len(beads))
	}
}

func TestListBeads_InvalidJSON(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	mockExec.AddResponse("br", []string{"list", "--json"}, []byte("{invalid"), nil)

	beads, err := client.ListBeads("hydra", "")

	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}

	expectedMsg := "failed to parse list JSON"
	if err.Error()[:len(expectedMsg)] != expectedMsg {
		t.Errorf("Expected JSON parsing error, got: %v", err)
	}

	if beads != nil {
		t.Errorf("Expected nil beads on error, got %v", beads)
	}
}

func TestCloseBead_Success(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	// Close command returns empty output on success
	mockExec.AddResponse("br", []string{"close", "hydra-abc123", "-m", "Task completed successfully"}, []byte(""), nil)

	err := client.CloseBead("hydra", "", "hydra-abc123", "Task completed successfully")
	if err != nil {
		t.Fatalf("CloseBead failed: %v", err)
	}

	// Verify correct command and arguments were passed
	calls := mockExec.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("Expected 1 call, got %d", len(calls))
	}

	call := calls[0]
	if call.Name != "br" {
		t.Errorf("Expected command 'br', got '%s'", call.Name)
	}
	if len(call.Args) != 4 {
		t.Errorf("Expected 4 args, got %d: %v", len(call.Args), call.Args)
	}
	if call.Args[0] != "close" {
		t.Errorf("Expected first arg 'close', got '%s'", call.Args[0])
	}
	if call.Args[1] != "hydra-abc123" {
		t.Errorf("Expected second arg 'hydra-abc123', got '%s'", call.Args[1])
	}
	if call.Args[2] != "-m" {
		t.Errorf("Expected third arg '-m', got '%s'", call.Args[2])
	}
	if call.Args[3] != "Task completed successfully" {
		t.Errorf("Expected fourth arg 'Task completed successfully', got '%s'", call.Args[3])
	}
}

func TestCloseBead_CLIError(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewTestableClient(mockExec)

	cliErr := errors.New("exit status 1: bead not found")
	mockExec.AddResponse("br", []string{"close", "nonexistent-123", "-m", "completed"}, []byte{}, cliErr)

	err := client.CloseBead("hydra", "", "nonexistent-123", "completed")

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	expectedMsg := "br close nonexistent-123 failed: exit status 1: bead not found"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestCloseBead_DifferentReasons(t *testing.T) {
	testCases := []struct {
		name   string
		reason string
	}{
		{
			name:   "completed",
			reason: "completed",
		},
		{
			name:   "cancelled",
			reason: "cancelled",
		},
		{
			name:   "duplicate",
			reason: "duplicate",
		},
		{
			name:   "not_planned",
			reason: "not_planned",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockExec := NewMockExecutor()
			client := NewTestableClient(mockExec)

			mockExec.AddResponse("br", []string{"close", "hydra-abc123", "-m", tc.reason}, []byte(""), nil)

			err := client.CloseBead("hydra", "", "hydra-abc123", tc.reason)
			if err != nil {
				t.Fatalf("CloseBead failed: %v", err)
			}

			calls := mockExec.GetCalls()
			if len(calls) != 1 {
				t.Fatalf("Expected 1 call, got %d", len(calls))
			}

			if calls[0].Args[3] != tc.reason {
				t.Errorf("Expected reason '%s', got '%s'", tc.reason, calls[0].Args[3])
			}
		})
	}
}

// Helper function to create string pointers
func strPtr(s string) *string {
	return &s
}
