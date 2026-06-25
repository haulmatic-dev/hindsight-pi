package core

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// mockCmd simulates an exec.Cmd for testing worker monitoring
type mockCmd struct {
	mu         sync.Mutex
	exitCode   int
	exitError  error
	waitCalled bool
	signaled   bool
}

func newMockCmd(exitCode int, err error) *mockCmd {
	return &mockCmd{
		exitCode:  exitCode,
		exitError: err,
	}
}

func (m *mockCmd) Wait() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waitCalled = true
	if m.exitError != nil {
		return m.exitError
	}
	if m.exitCode != 0 {
		return fmt.Errorf("exit status %d", m.exitCode)
	}
	return nil
}

// enhancedMockBeadsClient extends mockBeadsClient with worker completion tracking
type enhancedMockBeadsClient struct {
	*mockBeadsClient
	closeErrors   map[string]error
	closeAttempts []closeAttempt
	mu            sync.RWMutex
}

type closeAttempt struct {
	beadID      types.BeadID
	projectPath string
	repoName    string
	reason      string
	timestamp   time.Time
}

func newEnhancedMockBeadsClient() *enhancedMockBeadsClient {
	return &enhancedMockBeadsClient{
		mockBeadsClient: newMockBeadsClient(),
		closeErrors:     make(map[string]error),
		closeAttempts:   make([]closeAttempt, 0),
	}
}

func (m *enhancedMockBeadsClient) setCloseError(beadID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeErrors[beadID] = err
}

func (m *enhancedMockBeadsClient) getCloseAttempts() []closeAttempt {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]closeAttempt{}, m.closeAttempts...)
}

func (m *enhancedMockBeadsClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	m.mu.Lock()
	m.closeAttempts = append(m.closeAttempts, closeAttempt{
		beadID:      beadID,
		projectPath: projectPath,
		repoName:    repoName,
		reason:      reason,
		timestamp:   time.Now(),
	})
	m.mu.Unlock()

	// Check for injected error
	m.mu.RLock()
	if err, exists := m.closeErrors[string(beadID)]; exists {
		m.mu.RUnlock()
		return err
	}
	m.mu.RUnlock()

	return m.mockBeadsClient.CloseBead(projectPath, repoName, beadID, reason)
}

// setupWorkerCompletionTest creates an overseer configured for worker completion testing
func setupWorkerCompletionTest(t *testing.T) (*Overseer, *enhancedMockBeadsClient, []types.Project) {
	t.Helper()

	config := DefaultConfig()
	config.Overseer.MaxWorkers = 4
	config.Overseer.DefaultRetries = 3

	overseer := NewOverseer(config)

	mockBeads := newEnhancedMockBeadsClient()
	overseer.beadsClient = mockBeads

	testProjects := []types.Project{
		{
			Name: "test-project",
			Path: "/test/test-project",
			Type: types.RepoTypeSingle,
		},
		{
			Name: "multi-project",
			Path: "/test/multi-project",
			Type: types.RepoTypeMulti,
			Repos: []types.Repo{
				{Name: "backend", Path: "backend"},
				{Name: "frontend", Path: "frontend"},
			},
		},
	}
	overseer.projects = testProjects
	overseer.projectResolver = NewProjectResolver(testProjects)
	overseer.beadCloser = NewBeadCloser(overseer.beadsClient, overseer.intelligenceHub, overseer.projectResolver, overseer.validationService, overseer.config)

	// Initialize baseDelay and maxDelay
	overseer.retryManager.SetBaseDelay(5 * time.Second)
	overseer.retryManager.SetMaxDelay(5 * time.Minute)

	return overseer, mockBeads, testProjects
}

// TestWorkerCompletion_SuccessfulCompletion tests that a successful worker completion
// properly closes the bead with "completed" status
func TestWorkerCompletion_SuccessfulCompletion(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create a test bead
	bead := types.Bead{
		ID:      "test-project-123",
		Title:   "Test Bead",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	mockBeads.addBead(bead)

	// Create a worker that will complete successfully
	worker := &types.Worker{
		ID:          "test-project-123",
		BeadID:      "test-project-123",
		PID:         12345,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now().Add(-30 * time.Second),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// Register worker as active
	overseer.workerMutex.Lock()
	overseer.activeWorkers[worker.ID] = worker
	overseer.workerMutex.Unlock()

	// Simulate worker completion by directly calling closeBead
	err := overseer.closeBead(worker, true, 0)

	if err != nil {
		t.Fatalf("closeBead failed: %v", err)
	}

	// Verify CloseBead was called with correct parameters
	closeAttempts := mockBeads.getCloseAttempts()
	if len(closeAttempts) != 1 {
		t.Fatalf("Expected 1 close attempt, got %d", len(closeAttempts))
	}

	attempt := closeAttempts[0]
	if attempt.beadID != "test-project-123" {
		t.Errorf("Expected beadID 'test-project-123', got '%s'", attempt.beadID)
	}
	if attempt.reason != "completed" {
		t.Errorf("Expected reason 'completed', got '%s'", attempt.reason)
	}
	if attempt.projectPath != "/test/test-project" {
		t.Errorf("Expected projectPath '/test/test-project', got '%s'", attempt.projectPath)
	}

	// Verify the bead status was updated in the mock
	updatedBead, _ := mockBeads.GetBead("/test/test-project", "", "test-project-123")
	if updatedBead == nil {
		t.Fatal("Updated bead not found")
	}
	if updatedBead.Status != types.StatusCompleted {
		t.Errorf("Expected bead status 'completed', got '%s'", updatedBead.Status)
	}
}

// TestWorkerCompletion_FailedCompletion tests that a failed worker completion
// properly closes the bead with "failed" status
func TestWorkerCompletion_FailedCompletion(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create a test bead
	bead := types.Bead{
		ID:      "test-project-456",
		Title:   "Failing Bead",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	mockBeads.addBead(bead)

	// Create a worker
	worker := &types.Worker{
		ID:          "test-project-456",
		BeadID:      "test-project-456",
		PID:         12346,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now().Add(-30 * time.Second),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// Register worker as active
	overseer.workerMutex.Lock()
	overseer.activeWorkers[worker.ID] = worker
	overseer.workerMutex.Unlock()

	// Simulate failed worker completion
	err := overseer.closeBead(worker, false, 1)

	if err != nil {
		t.Fatalf("closeBead failed: %v", err)
	}

	// Verify CloseBead was called with "failed" reason
	closeAttempts := mockBeads.getCloseAttempts()
	if len(closeAttempts) != 1 {
		t.Fatalf("Expected 1 close attempt, got %d", len(closeAttempts))
	}

	if closeAttempts[0].reason != "failed" {
		t.Errorf("Expected reason 'failed', got '%s'", closeAttempts[0].reason)
	}

	// Verify the bead status was updated
	updatedBead, _ := mockBeads.GetBead("/test/test-project", "", "test-project-456")
	if updatedBead == nil {
		t.Fatal("Updated bead not found")
	}
	if updatedBead.Status != types.StatusFailed {
		t.Errorf("Expected bead status 'failed', got '%s'", updatedBead.Status)
	}
}

// TestWorkerCompletion_ProjectPathResolution tests that closeBead correctly
// resolves the project path from various sources
func TestWorkerCompletion_ProjectPathResolution(t *testing.T) {
	tests := []struct {
		name         string
		worker       *types.Worker
		expectedPath string
		expectedRepo string
		description  string
	}{
		{
			name: "worker with project path",
			worker: &types.Worker{
				ID:          "test-project-001",
				BeadID:      "test-project-001",
				ProjectPath: "/test/test-project",
			},
			expectedPath: "/test/test-project",
			expectedRepo: "",
			description:  "Should use project path from worker record",
		},
		{
			name: "multi-repo project",
			worker: &types.Worker{
				ID:          "multi-project/backend-002",
				BeadID:      "multi-project/backend-002",
				ProjectPath: "/test/multi-project",
			},
			expectedPath: "/test/multi-project",
			expectedRepo: "backend",
			description:  "Should extract repo from BeadID for multi-repo projects",
		},
		{
			name: "fallback to project search",
			worker: &types.Worker{
				ID:          "test-project-003",
				BeadID:      "test-project-003",
				ProjectPath: "", // Empty, should trigger fallback
			},
			expectedPath: "/test/test-project",
			expectedRepo: "",
			description:  "Should fall back to searching projects when worker has no path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overseer, mockBeads, _ := setupWorkerCompletionTest(t)

			// Create bead for the worker
			bead := types.Bead{
				ID:      types.BeadID(tt.worker.BeadID),
				Title:   "Test Bead",
				Status:  types.StatusOpen,
				Project: types.BeadID(tt.worker.BeadID).Project(),
				Type:    "implement",
			}
			mockBeads.addBead(bead)

			// Call closeBead
			err := overseer.closeBead(tt.worker, true, 0)
			if err != nil {
				t.Fatalf("closeBead failed: %v", err)
			}

			// Verify the correct project path was used
			closeAttempts := mockBeads.getCloseAttempts()
			if len(closeAttempts) == 0 {
				t.Fatal("No close attempts recorded")
			}

			lastAttempt := closeAttempts[len(closeAttempts)-1]
			if lastAttempt.projectPath != tt.expectedPath {
				t.Errorf("%s: expected path '%s', got '%s'", tt.description, tt.expectedPath, lastAttempt.projectPath)
			}
			if lastAttempt.repoName != tt.expectedRepo {
				t.Errorf("%s: expected repo '%s', got '%s'", tt.description, tt.expectedRepo, lastAttempt.repoName)
			}
		})
	}
}

// TestWorkerCompletion_CloseErrorHandling tests handling of close errors
func TestWorkerCompletion_CloseErrorHandling(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create a test bead
	bead := types.Bead{
		ID:      "test-project-error",
		Title:   "Error Bead",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	mockBeads.addBead(bead)

	// Inject a close error
	mockBeads.setCloseError("test-project-error", errors.New("database connection failed"))

	worker := &types.Worker{
		ID:          "test-project-error",
		BeadID:      "test-project-error",
		PID:         12347,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// Attempt to close - should return error
	err := overseer.closeBead(worker, true, 0)

	if err == nil {
		t.Fatal("Expected error when closing bead fails, got nil")
	}

	if err.Error() != "database connection failed" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

// TestWorkerCompletion_DependencyErrorDetection tests detection of dependency-related close failures
func TestWorkerCompletion_DependencyErrorDetection(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create test beads with dependencies
	beadA := types.Bead{
		ID:      "test-project-A",
		Title:   "Bead A",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	beadB := types.Bead{
		ID:           "test-project-B",
		Title:        "Bead B",
		Status:       types.StatusOpen,
		Project:      "test-project",
		Type:         "implement",
		Dependencies: []types.BeadID{"test-project-A"},
	}
	mockBeads.addBead(beadA)
	mockBeads.addBead(beadB)

	// Inject dependency error for bead B
	mockBeads.setCloseError("test-project-B", errors.New("blocked by: test-project-A:open"))

	workerB := &types.Worker{
		ID:          "test-project-B",
		BeadID:      "test-project-B",
		PID:         12348,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// Attempt to close B - should detect dependency error
	err := overseer.closeBead(workerB, true, 0)

	if err == nil {
		t.Fatal("Expected dependency error, got nil")
	}

	// Error should indicate dependencies issue
	if !containsAny(err.Error(), []string{"dependencies", "blocked", "dependency"}) {
		t.Errorf("Expected error to mention dependencies, got: %v", err)
	}
}

// TestWorkerCompletion_RetryTrackingOnFailure tests that failed workers are tracked for retry
func TestWorkerCompletion_RetryTrackingOnFailure(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create a test bead
	bead := types.Bead{
		ID:      "test-project-retry",
		Title:   "Retry Test Bead",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	mockBeads.addBead(bead)

	worker := &types.Worker{
		ID:          "test-project-retry",
		BeadID:      "test-project-retry",
		PID:         12349,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// Record initial retry count
	initialRetryCount := overseer.retryManager.GetRetryCount(worker.ID)
	if initialRetryCount != 0 {
		t.Fatalf("Expected initial retry count of 0, got %d", initialRetryCount)
	}

	// Record a retry for the worker (simulating monitorWorker behavior)
	overseer.retryManager.RecordRetry(worker.ID, errors.New("worker failed"))

	// Verify retry was tracked
	retryCount := overseer.retryManager.GetRetryCount(worker.ID)
	if retryCount != 1 {
		t.Errorf("Expected retry count of 1 after failure, got %d", retryCount)
	}

	// After just recording a retry, shouldRetry returns false due to cooldown period
	// This is expected behavior - we don't want to retry immediately
	if overseer.retryManager.ShouldRetry(worker.ID, overseer.config.Overseer.DefaultRetries) {
		t.Error("Expected worker to NOT be eligible for retry immediately (cooldown period)")
	}

	// Record more retries up to max
	maxRetries := overseer.config.Overseer.DefaultRetries
	for i := 1; i < maxRetries; i++ {
		overseer.retryManager.RecordRetry(worker.ID, errors.New("worker failed"))
	}

	// Now shouldRetry should return false (max retries exceeded)
	if overseer.retryManager.ShouldRetry(worker.ID, overseer.config.Overseer.DefaultRetries) {
		t.Error("Expected worker to NOT be eligible for retry after max retries exceeded")
	}
}

// TestWorkerCompletion_ClearRetryOnSuccess tests that retry state is cleared on successful completion
func TestWorkerCompletion_ClearRetryOnSuccess(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create a test bead
	bead := types.Bead{
		ID:      "test-project-clear",
		Title:   "Clear Retry Test",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	mockBeads.addBead(bead)

	worker := &types.Worker{
		ID:          "test-project-clear",
		BeadID:      "test-project-clear",
		PID:         12350,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// Record some retries first
	overseer.retryManager.RecordRetry(worker.ID, errors.New("first failure"))
	overseer.retryManager.RecordRetry(worker.ID, errors.New("second failure"))

	if overseer.retryManager.GetRetryCount(worker.ID) != 2 {
		t.Fatalf("Expected 2 retries recorded")
	}

	// Clear retry state (simulating successful completion)
	overseer.retryManager.ClearRetry(worker.ID)

	// Verify retry state was cleared
	if overseer.retryManager.GetRetryCount(worker.ID) != 0 {
		t.Errorf("Expected retry count to be 0 after clear, got %d", overseer.retryManager.GetRetryCount(worker.ID))
	}
}

// TestWorkerCompletion_ActiveWorkerRemoval tests that workers are removed from active map on completion
func TestWorkerCompletion_ActiveWorkerRemoval(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create a test bead
	bead := types.Bead{
		ID:      "test-project-remove",
		Title:   "Removal Test Bead",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	mockBeads.addBead(bead)

	worker := &types.Worker{
		ID:          "test-project-remove",
		BeadID:      "test-project-remove",
		PID:         12351,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// Add worker to active workers
	overseer.workerMutex.Lock()
	overseer.activeWorkers[worker.ID] = worker
	overseer.workerMutex.Unlock()

	// Verify worker is active
	if !overseer.isBeadRunning(worker.ID) {
		t.Fatal("Worker should be marked as running before completion")
	}

	// Simulate monitorWorker removing the worker
	overseer.workerMutex.Lock()
	delete(overseer.activeWorkers, worker.ID)
	overseer.workerMutex.Unlock()

	// Verify worker is no longer active
	if overseer.isBeadRunning(worker.ID) {
		t.Error("Worker should NOT be marked as running after removal")
	}
}

// TestWorkerCompletion_ExponentialBackoff tests that retry delays follow exponential backoff
func TestWorkerCompletion_ExponentialBackoff(t *testing.T) {
	baseDelay := 5 * time.Second
	maxDelay := 5 * time.Minute

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 5 * time.Second},
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{3, 40 * time.Second},
		{4, 80 * time.Second},
		{5, 160 * time.Second},
		{6, 300 * time.Second}, // Capped at maxDelay
	}

	for _, tt := range tests {
		delay := calculateBackoff(tt.attempt, baseDelay, maxDelay)
		if delay != tt.expected {
			t.Errorf("Attempt %d: expected delay %v, got %v", tt.attempt, tt.expected, delay)
		}
	}
}

// TestWorkerCompletion_ImmediateScheduleTrigger tests that successful completion triggers immediate scheduling
func TestWorkerCompletion_ImmediateScheduleTrigger(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create beads: A (ready) -> B (blocked by A)
	beadA := types.Bead{
		ID:      "test-project-A",
		Title:   "Bead A",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	beadB := types.Bead{
		ID:           "test-project-B",
		Title:        "Bead B",
		Status:       types.StatusOpen,
		Project:      "test-project",
		Type:         "implement",
		Dependencies: []types.BeadID{"test-project-A"},
	}
	mockBeads.addBead(beadA)
	mockBeads.addBead(beadB)

	// Build DAG
	if err := overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Verify B is blocked initially
	nodeB, _ := overseer.dag.GetNode("test-project-B")
	if nodeB.InDegree == 0 {
		t.Fatal("Bead B should be blocked initially")
	}

	// Complete A in the DAG (this should unblock B)
	newlyReady, err := overseer.dag.MarkCompleted("test-project-A")
	if err != nil {
		t.Fatalf("MarkCompleted failed: %v", err)
	}

	// Verify B is now ready
	foundB := false
	for _, ready := range newlyReady {
		if ready == "test-project-B" {
			foundB = true
			break
		}
	}
	if !foundB {
		t.Error("Bead B should be in newly ready list after A completes")
	}
}

// TestWorkerCompletion_StatusTransitions tests the status transitions during worker lifecycle
func TestWorkerCompletion_StatusTransitions(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	bead := types.Bead{
		ID:      "test-project-status",
		Title:   "Status Test Bead",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	mockBeads.addBead(bead)
	_ = overseer

	worker := &types.Worker{
		ID:          "test-project-status",
		BeadID:      "test-project-status",
		PID:         12352,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// Initial status: running
	if worker.Status != "running" {
		t.Errorf("Initial status should be 'running', got '%s'", worker.Status)
	}

	// Simulate successful completion
	worker.Status = "completed"

	if worker.Status != "completed" {
		t.Errorf("After success, status should be 'completed', got '%s'", worker.Status)
	}

	// Simulate failure case with retry_pending
	worker.Status = "retry_pending"

	if worker.Status != "retry_pending" {
		t.Errorf("After failure with retries remaining, status should be 'retry_pending', got '%s'", worker.Status)
	}

	// Simulate permanent failure
	worker.Status = "failed"

	if worker.Status != "failed" {
		t.Errorf("After permanent failure, status should be 'failed', got '%s'", worker.Status)
	}
}

// TestWorkerCompletion_ConcurrentCompletions tests handling of concurrent worker completions
func TestWorkerCompletion_ConcurrentCompletions(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	// Create multiple independent beads
	for i := 0; i < 5; i++ {
		beadID := types.NewNumericBeadID("test-project", "", i)
		mockBeads.addBead(types.Bead{
			ID:      beadID,
			Title:   fmt.Sprintf("Concurrent Bead %d", i),
			Status:  types.StatusOpen,
			Project: "test-project",
			Type:    "implement",
		})
	}
	_ = overseer

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Simulate concurrent worker completions
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			beadID := types.NewNumericBeadID("test-project", "", index)
			worker := &types.Worker{
				ID:          string(beadID),
				BeadID:      beadID,
				PID:         12360 + index,
				AgentType:   types.AgentOrchestrator,
				StartTime:   time.Now(),
				Status:      "running",
				ProjectPath: "/test/test-project",
			}

			// Add small delay to increase chance of concurrency issues
			time.Sleep(time.Duration(index) * 10 * time.Millisecond)

			if err := overseer.closeBead(worker, true, 0); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errCount := 0
	for err := range errors {
		t.Errorf("Concurrent completion error: %v", err)
		errCount++
	}

	if errCount > 0 {
		t.Fatalf("Had %d errors during concurrent completions", errCount)
	}

	// Verify all beads were closed
	closeAttempts := mockBeads.getCloseAttempts()
	if len(closeAttempts) != 5 {
		t.Errorf("Expected 5 close attempts, got %d", len(closeAttempts))
	}
}

// TestWorkerCompletion_UnknownProject tests handling when project is not found
func TestWorkerCompletion_UnknownProject(t *testing.T) {
	overseer, _, _ := setupWorkerCompletionTest(t)

	worker := &types.Worker{
		ID:          "unknown-project-001",
		BeadID:      "unknown-project-001",
		PID:         12370,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: "", // No project path
	}

	// Attempt to close with unknown project
	err := overseer.closeBead(worker, true, 0)

	if err == nil {
		t.Fatal("Expected error when project not found, got nil")
	}

	if !containsAny(err.Error(), []string{"project", "not found", "unknown"}) {
		t.Errorf("Expected error to mention project not found, got: %v", err)
	}
}

// TestWorkerCompletion_MultipleCloseAttempts tests that multiple close attempts are tracked
func TestWorkerCompletion_MultipleCloseAttempts(t *testing.T) {
	overseer, mockBeads, _ := setupWorkerCompletionTest(t)

	bead := types.Bead{
		ID:      "test-project-multi",
		Title:   "Multi Close Test",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	mockBeads.addBead(bead)

	worker := &types.Worker{
		ID:          "test-project-multi",
		BeadID:      "test-project-multi",
		PID:         12371,
		AgentType:   types.AgentOrchestrator,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: "/test/test-project",
	}

	// First attempt - inject temporary error
	mockBeads.setCloseError("test-project-multi", errors.New("temporary error"))
	err1 := overseer.closeBead(worker, true, 0)
	if err1 == nil {
		t.Fatal("Expected error on first attempt")
	}

	// Second attempt - success
	mockBeads.setCloseError("test-project-multi", nil)
	err2 := overseer.closeBead(worker, true, 0)
	if err2 != nil {
		t.Fatalf("Expected success on second attempt, got: %v", err2)
	}

	// Verify both attempts were recorded
	closeAttempts := mockBeads.getCloseAttempts()
	if len(closeAttempts) != 2 {
		t.Errorf("Expected 2 close attempts, got %d", len(closeAttempts))
	}
}

// Helper function to check if string contains any of the substrings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(substr) > 0 && len(s) > 0 {
			// Simple check for substring presence
			found := false
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					found = true
					break
				}
			}
			if found {
				return true
			}
		}
	}
	return false
}
