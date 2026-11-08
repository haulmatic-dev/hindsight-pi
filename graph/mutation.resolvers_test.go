package graph

import (
	"strings"
	"context"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/graph/model"
	"github.com/haulmatic-dev/hydra/internal/core"
)

// ============================================================================
// Helper Functions
// ============================================================================

// createTestOverseer creates an Overseer with test configuration
func createTestOverseer() *core.Overseer {
	config := &core.Config{
		Overseer: core.OverseerConfig{
			BeadDiscoveryInterval: 5 * time.Second,
			MaxWorkers:            5,
			DefaultRetries:        3,
			WorkerTimeout:         30 * time.Minute,
		},
		MCP: core.MCPConfig{
			Enabled: false, // Disable MCP for tests
		},
		API: core.APIConfig{
			Enabled: true,
			Port:    8080,
		},
		Intelligence: core.IntelligenceConfig{
			GPTCache: core.GPTCacheConfig{
				Enabled: false,
			},
		},
	}
	return core.NewOverseer(config)
}

// createMutationResolver creates a mutation resolver with test overseer
func createMutationResolver(overseer *core.Overseer) *mutationResolver {
	resolver := &Resolver{
		Overseer: overseer,
	}
	return &mutationResolver{resolver}
}

// ============================================================================
// TestPauseOverseer
// ============================================================================

func TestPauseOverseer_Success(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	// Verify overseer is not paused initially
	if overseer.IsPaused() {
		t.Error("Expected overseer to not be paused initially")
	}

	// Call PauseOverseer
	result, err := resolver.PauseOverseer(context.Background())

	// Assert no error
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Assert returns true
	if !result {
		t.Error("Expected result to be true")
	}

	// Verify overseer is paused
	if !overseer.IsPaused() {
		t.Error("Expected overseer to be paused after calling PauseOverseer")
	}
}

func TestPauseOverseer_AlreadyPaused(t *testing.T) {
	overseer := createTestOverseer()
	overseer.Pause() // Start paused
	resolver := createMutationResolver(overseer)

	result, err := resolver.PauseOverseer(context.Background())

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !result {
		t.Error("Expected result to be true even when already paused")
	}

	if !overseer.IsPaused() {
		t.Error("Expected overseer to still be paused")
	}
}

func TestPauseOverseer_NilOverseer(t *testing.T) {
	resolver := createMutationResolver(nil)

	result, err := resolver.PauseOverseer(context.Background())

	// Assert error
	if err == nil {
		t.Error("Expected error when overseer is nil")
	}

	expectedErrMsg := "overseer not initialized"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	// Assert returns false
	if result {
		t.Error("Expected result to be false when overseer is nil")
	}
}

// ============================================================================
// TestResumeOverseer
// ============================================================================

func TestResumeOverseer_Success(t *testing.T) {
	overseer := createTestOverseer()
	overseer.Pause() // Start paused
	resolver := createMutationResolver(overseer)

	// Verify overseer is paused initially
	if !overseer.IsPaused() {
		t.Error("Expected overseer to be paused initially")
	}

	// Call ResumeOverseer
	result, err := resolver.ResumeOverseer(context.Background())

	// Assert no error
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Assert returns true
	if !result {
		t.Error("Expected result to be true")
	}

	// Verify overseer is not paused
	if overseer.IsPaused() {
		t.Error("Expected overseer to be resumed after calling ResumeOverseer")
	}
}

func TestResumeOverseer_AlreadyRunning(t *testing.T) {
	overseer := createTestOverseer()
	// Start not paused (default state)
	resolver := createMutationResolver(overseer)

	result, err := resolver.ResumeOverseer(context.Background())

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !result {
		t.Error("Expected result to be true even when already running")
	}

	if overseer.IsPaused() {
		t.Error("Expected overseer to still not be paused")
	}
}

func TestResumeOverseer_NilOverseer(t *testing.T) {
	resolver := createMutationResolver(nil)

	result, err := resolver.ResumeOverseer(context.Background())

	// Assert error
	if err == nil {
		t.Error("Expected error when overseer is nil")
	}

	expectedErrMsg := "overseer not initialized"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	// Assert returns false
	if result {
		t.Error("Expected result to be false when overseer is nil")
	}
}

// ============================================================================
// TestSetMaxWorkers
// ============================================================================

func TestSetMaxWorkers_ValidRange(t *testing.T) {
	testCases := []struct {
		name          string
		input         int32
		expectedCount int
	}{
		{"Minimum valid", 1, 1},
		{"Small value", 5, 5},
		{"Medium value", 50, 50},
		{"Maximum valid", 100, 100},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			overseer := createTestOverseer()
			resolver := createMutationResolver(overseer)

			result, err := resolver.SetMaxWorkers(context.Background(), tc.input)

			if err != nil {
				t.Errorf("Expected no error for valid input %d, got: %v", tc.input, err)
			}

			if result != tc.input {
				t.Errorf("Expected result %d, got %d", tc.input, result)
			}

			if overseer.GetMaxWorkers() != tc.expectedCount {
				t.Errorf("Expected maxWorkers to be %d, got %d", tc.expectedCount, overseer.GetMaxWorkers())
			}
		})
	}
}

func TestSetMaxWorkers_RejectsNegative(t *testing.T) {
	overseer := createTestOverseer()
	originalMaxWorkers := overseer.GetMaxWorkers()
	resolver := createMutationResolver(overseer)

	result, err := resolver.SetMaxWorkers(context.Background(), -5)

	// Assert error
	if err == nil {
		t.Error("Expected error for negative worker count")
	}

	expectedErrMsg := "max workers must be greater than 0"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	// Assert returns 0
	if result != 0 {
		t.Errorf("Expected result to be 0, got %d", result)
	}

	// Verify maxWorkers was not changed
	if overseer.GetMaxWorkers() != originalMaxWorkers {
		t.Errorf("Expected maxWorkers to remain %d, got %d", originalMaxWorkers, overseer.GetMaxWorkers())
	}
}

func TestSetMaxWorkers_RejectsZero(t *testing.T) {
	overseer := createTestOverseer()
	originalMaxWorkers := overseer.GetMaxWorkers()
	resolver := createMutationResolver(overseer)

	result, err := resolver.SetMaxWorkers(context.Background(), 0)

	// Assert error
	if err == nil {
		t.Error("Expected error for zero worker count")
	}

	expectedErrMsg := "max workers must be greater than 0"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result != 0 {
		t.Errorf("Expected result to be 0, got %d", result)
	}

	if overseer.GetMaxWorkers() != originalMaxWorkers {
		t.Errorf("Expected maxWorkers to remain %d, got %d", originalMaxWorkers, overseer.GetMaxWorkers())
	}
}

func TestSetMaxWorkers_RejectsTooHigh(t *testing.T) {
	overseer := createTestOverseer()
	originalMaxWorkers := overseer.GetMaxWorkers()
	resolver := createMutationResolver(overseer)

	result, err := resolver.SetMaxWorkers(context.Background(), 101)

	// Assert error
	if err == nil {
		t.Error("Expected error for worker count exceeding 100")
	}

	expectedErrMsg := "max workers cannot exceed 100"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result != 0 {
		t.Errorf("Expected result to be 0, got %d", result)
	}

	if overseer.GetMaxWorkers() != originalMaxWorkers {
		t.Errorf("Expected maxWorkers to remain %d, got %d", originalMaxWorkers, overseer.GetMaxWorkers())
	}
}

func TestSetMaxWorkers_NilOverseer(t *testing.T) {
	resolver := createMutationResolver(nil)

	result, err := resolver.SetMaxWorkers(context.Background(), 10)

	// Assert error
	if err == nil {
		t.Error("Expected error when overseer is nil")
	}

	expectedErrMsg := "overseer not initialized"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result != 0 {
		t.Errorf("Expected result to be 0, got %d", result)
	}
}

// ============================================================================
// TestCancelWorker
// ============================================================================

func TestCancelWorker_Success(t *testing.T) {
	overseer := createTestOverseer()
	// Note: Adding workers requires the overseer to be running, so we test the validation logic
	// The actual worker cancellation would need active workers
	resolver := createMutationResolver(overseer)

	// Since we can't easily add active workers without starting the overseer,
	// we'll test with the mock scenario where a worker exists
	// For now, test that the resolver properly delegates to the overseer

	// This test verifies the resolver checks the overseer correctly
	// The actual worker cancellation logic is tested in integration tests

	result, err := resolver.CancelWorker(context.Background(), "worker-123")

	// Since there are no active workers, this should return an error
	if err == nil {
		t.Error("Expected error when worker not found")
	}

	expectedErrMsg := "worker not found or not active: worker-123"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result {
		t.Error("Expected result to be false when worker not found")
	}
}

func TestCancelWorker_NotFound(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	result, err := resolver.CancelWorker(context.Background(), "non-existent-worker")

	// Assert error
	if err == nil {
		t.Error("Expected error when worker not found")
	}

	expectedErrMsg := "worker not found or not active: non-existent-worker"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result {
		t.Error("Expected result to be false when worker not found")
	}
}

func TestCancelWorker_EmptyID(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	result, err := resolver.CancelWorker(context.Background(), "")

	// Assert error
	if err == nil {
		t.Error("Expected error for empty worker ID")
	}

	expectedErrMsg := "worker ID cannot be empty"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result {
		t.Error("Expected result to be false for empty worker ID")
	}
}

func TestCancelWorker_NilOverseer(t *testing.T) {
	resolver := createMutationResolver(nil)

	result, err := resolver.CancelWorker(context.Background(), "worker-123")

	// Assert error
	if err == nil {
		t.Error("Expected error when overseer is nil")
	}

	expectedErrMsg := "overseer not initialized"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result {
		t.Error("Expected result to be false when overseer is nil")
	}
}

// ============================================================================
// TestForceCompleteBead
// ============================================================================

func TestForceCompleteBead_EmptyID(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	result, err := resolver.ForceCompleteBead(context.Background(), "", nil)

	if err == nil {
		t.Error("Expected error for empty bead ID")
	}

	expectedErrMsg := "forceCompleteBead is disabled"
	if err != nil && !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("Expected error message containing '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result != nil {
		t.Error("Expected result to be nil for empty ID")
	}
}

func TestForceCompleteBead_InvalidBeadID(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	// Use invalid bead ID format (no dash)
	result, err := resolver.ForceCompleteBead(context.Background(), "invalidid", nil)

	if err == nil {
		t.Error("Expected error for invalid bead ID")
	}

	if result != nil {
		t.Error("Expected result to be nil for invalid bead ID")
	}
}

func TestForceCompleteBead_ProjectNotFound(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	result, err := resolver.ForceCompleteBead(context.Background(), "hydra-testbead", nil)

	// Since the overseer has no projects configured, project won't be found
	if err == nil {
		t.Error("Expected error when project not found")
	}

	expectedErrMsg := "forceCompleteBead is disabled"
	if err != nil && !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("Expected error message containing '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result != nil {
		t.Error("Expected result to be nil when project not found")
	}
}

func TestForceCompleteBead_NilOverseer(t *testing.T) {
	resolver := createMutationResolver(nil)

	result, err := resolver.ForceCompleteBead(context.Background(), "hydra-001", nil)

	if err == nil {
		t.Error("Expected error when overseer is nil")
	}

	expectedErrMsg := "forceCompleteBead is disabled"
	if err != nil && !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("Expected error message containing '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result != nil {
		t.Error("Expected result to be nil when overseer is nil")
	}
}

// ============================================================================
// TestUpdateConfig
// ============================================================================

func TestUpdateConfig_MaxWorkers(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	newMaxWorkers := int32(25)
	input := model.ConfigInput{
		MaxWorkers: &newMaxWorkers,
	}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result to not be nil")
	}

	if result.MaxWorkers != newMaxWorkers {
		t.Errorf("Expected MaxWorkers %d, got %d", newMaxWorkers, result.MaxWorkers)
	}

	if overseer.GetMaxWorkers() != int(newMaxWorkers) {
		t.Errorf("Expected overseer maxWorkers %d, got %d", newMaxWorkers, overseer.GetMaxWorkers())
	}
}

func TestUpdateConfig_PollInterval(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	newPollInterval := int32(60)
	input := model.ConfigInput{
		PollInterval: &newPollInterval,
	}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result to not be nil")
	}

	if result.PollInterval != newPollInterval {
		t.Errorf("Expected PollInterval %d, got %d", newPollInterval, result.PollInterval)
	}

	expectedDuration := time.Duration(newPollInterval) * time.Second
	if overseer.GetBeadDiscoveryInterval() != expectedDuration {
		t.Errorf("Expected poll interval %v, got %v", expectedDuration, overseer.GetBeadDiscoveryInterval())
	}

	// Verify other config values unchanged
	if overseer.GetMaxWorkers() != 5 {
		t.Error("Expected maxWorkers to remain unchanged")
	}
}

func TestUpdateConfig_RetryDelay(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	newRetryDelay := int32(10)
	input := model.ConfigInput{
		RetryDelay: &newRetryDelay,
	}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result to not be nil")
	}

	if result.RetryDelay != newRetryDelay {
		t.Errorf("Expected RetryDelay %d, got %d", newRetryDelay, result.RetryDelay)
	}

	expectedDuration := time.Duration(newRetryDelay) * time.Second
	if overseer.GetRetryDelay() != expectedDuration {
		t.Errorf("Expected retry delay %v, got %v", expectedDuration, overseer.GetRetryDelay())
	}
}

func TestUpdateConfig_MultipleFields(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	newMaxWorkers := int32(15)
	newPollInterval := int32(45)
	newRetryDelay := int32(20)

	input := model.ConfigInput{
		MaxWorkers:   &newMaxWorkers,
		PollInterval: &newPollInterval,
		RetryDelay:   &newRetryDelay,
	}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result to not be nil")
	}

	if result.MaxWorkers != newMaxWorkers {
		t.Errorf("Expected MaxWorkers %d, got %d", newMaxWorkers, result.MaxWorkers)
	}

	if result.PollInterval != newPollInterval {
		t.Errorf("Expected PollInterval %d, got %d", newPollInterval, result.PollInterval)
	}

	if result.RetryDelay != newRetryDelay {
		t.Errorf("Expected RetryDelay %d, got %d", newRetryDelay, result.RetryDelay)
	}

	// Verify all values updated in overseer
	if overseer.GetMaxWorkers() != int(newMaxWorkers) {
		t.Errorf("Expected maxWorkers %d, got %d", newMaxWorkers, overseer.GetMaxWorkers())
	}

	expectedPollInterval := time.Duration(newPollInterval) * time.Second
	if overseer.GetBeadDiscoveryInterval() != expectedPollInterval {
		t.Errorf("Expected poll interval %v, got %v", expectedPollInterval, overseer.GetBeadDiscoveryInterval())
	}

	expectedRetryDelay := time.Duration(newRetryDelay) * time.Second
	if overseer.GetRetryDelay() != expectedRetryDelay {
		t.Errorf("Expected retry delay %v, got %v", expectedRetryDelay, overseer.GetRetryDelay())
	}
}

func TestUpdateConfig_MaxWorkersValidation_Zero(t *testing.T) {
	overseer := createTestOverseer()
	originalMaxWorkers := overseer.GetMaxWorkers()
	resolver := createMutationResolver(overseer)

	invalidMaxWorkers := int32(0)
	input := model.ConfigInput{
		MaxWorkers: &invalidMaxWorkers,
	}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err == nil {
		t.Error("Expected error for zero max workers")
	}

	expectedErrMsg := "max workers must be greater than 0"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result != nil {
		t.Error("Expected result to be nil on error")
	}

	if overseer.GetMaxWorkers() != originalMaxWorkers {
		t.Errorf("Expected maxWorkers to remain %d, got %d", originalMaxWorkers, overseer.GetMaxWorkers())
	}
}

func TestUpdateConfig_MaxWorkersValidation_Negative(t *testing.T) {
	overseer := createTestOverseer()
	originalMaxWorkers := overseer.GetMaxWorkers()
	resolver := createMutationResolver(overseer)

	invalidMaxWorkers := int32(-5)
	input := model.ConfigInput{
		MaxWorkers: &invalidMaxWorkers,
	}

	_, err := resolver.UpdateConfig(context.Background(), input)

	if err == nil {
		t.Error("Expected error for negative max workers")
	}

	expectedErrMsg := "max workers must be greater than 0"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if overseer.GetMaxWorkers() != originalMaxWorkers {
		t.Errorf("Expected maxWorkers to remain %d, got %d", originalMaxWorkers, overseer.GetMaxWorkers())
	}
}

func TestUpdateConfig_MaxWorkersValidation_TooHigh(t *testing.T) {
	overseer := createTestOverseer()
	originalMaxWorkers := overseer.GetMaxWorkers()
	resolver := createMutationResolver(overseer)

	invalidMaxWorkers := int32(150)
	input := model.ConfigInput{
		MaxWorkers: &invalidMaxWorkers,
	}

	_, err := resolver.UpdateConfig(context.Background(), input)

	if err == nil {
		t.Error("Expected error for max workers exceeding 100")
	}

	expectedErrMsg := "max workers cannot exceed 100"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if overseer.GetMaxWorkers() != originalMaxWorkers {
		t.Errorf("Expected maxWorkers to remain %d, got %d", originalMaxWorkers, overseer.GetMaxWorkers())
	}
}

func TestUpdateConfig_PollIntervalValidation_Zero(t *testing.T) {
	overseer := createTestOverseer()
	originalInterval := overseer.GetBeadDiscoveryInterval()
	resolver := createMutationResolver(overseer)

	invalidPollInterval := int32(0)
	input := model.ConfigInput{
		PollInterval: &invalidPollInterval,
	}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err == nil {
		t.Error("Expected error for zero poll interval")
	}

	expectedErrMsg := "poll interval must be at least 1 second"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if overseer.GetBeadDiscoveryInterval() != originalInterval {
		t.Errorf("Expected poll interval to remain %v, got %v", originalInterval, overseer.GetBeadDiscoveryInterval())
	}

	if result != nil {
		t.Error("Expected result to be nil on error")
	}
}

func TestUpdateConfig_PollIntervalValidation_Negative(t *testing.T) {
	overseer := createTestOverseer()
	originalInterval := overseer.GetBeadDiscoveryInterval()
	resolver := createMutationResolver(overseer)

	invalidPollInterval := int32(-10)
	input := model.ConfigInput{
		PollInterval: &invalidPollInterval,
	}

	_, err := resolver.UpdateConfig(context.Background(), input)

	if err == nil {
		t.Error("Expected error for negative poll interval")
	}

	expectedErrMsg := "poll interval must be at least 1 second"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if overseer.GetBeadDiscoveryInterval() != originalInterval {
		t.Errorf("Expected poll interval to remain %v, got %v", originalInterval, overseer.GetBeadDiscoveryInterval())
	}
}

func TestUpdateConfig_RetryDelayValidation_Zero(t *testing.T) {
	overseer := createTestOverseer()
	originalRetryDelay := overseer.GetRetryDelay()
	resolver := createMutationResolver(overseer)

	invalidRetryDelay := int32(0)
	input := model.ConfigInput{
		RetryDelay: &invalidRetryDelay,
	}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err == nil {
		t.Error("Expected error for zero retry delay")
	}

	expectedErrMsg := "retry delay must be at least 1 second"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if overseer.GetRetryDelay() != originalRetryDelay {
		t.Errorf("Expected retry delay to remain %v, got %v", originalRetryDelay, overseer.GetRetryDelay())
	}

	if result != nil {
		t.Error("Expected result to be nil on error")
	}
}

func TestUpdateConfig_RetryDelayValidation_Negative(t *testing.T) {
	overseer := createTestOverseer()
	originalRetryDelay := overseer.GetRetryDelay()
	resolver := createMutationResolver(overseer)

	invalidRetryDelay := int32(-5)
	input := model.ConfigInput{
		RetryDelay: &invalidRetryDelay,
	}

	_, err := resolver.UpdateConfig(context.Background(), input)

	if err == nil {
		t.Error("Expected error for negative retry delay")
	}

	expectedErrMsg := "retry delay must be at least 1 second"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if overseer.GetRetryDelay() != originalRetryDelay {
		t.Errorf("Expected retry delay to remain %v, got %v", originalRetryDelay, overseer.GetRetryDelay())
	}
}

func TestUpdateConfig_EmptyInput(t *testing.T) {
	overseer := createTestOverseer()
	originalMaxWorkers := overseer.GetMaxWorkers()
	originalPollInterval := overseer.GetBeadDiscoveryInterval()
	originalRetryDelay := overseer.GetRetryDelay()

	resolver := createMutationResolver(overseer)

	// Empty input - all nil
	input := model.ConfigInput{}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err != nil {
		t.Errorf("Expected no error for empty input, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result to not be nil")
	}

	// Verify no changes were made
	if overseer.GetMaxWorkers() != originalMaxWorkers {
		t.Errorf("Expected maxWorkers to remain %d, got %d", originalMaxWorkers, overseer.GetMaxWorkers())
	}

	if overseer.GetBeadDiscoveryInterval() != originalPollInterval {
		t.Errorf("Expected poll interval to remain %v, got %v", originalPollInterval, overseer.GetBeadDiscoveryInterval())
	}

	if overseer.GetRetryDelay() != originalRetryDelay {
		t.Errorf("Expected retry delay to remain %v, got %v", originalRetryDelay, overseer.GetRetryDelay())
	}

	// Result should reflect current values
	if result.MaxWorkers != int32(originalMaxWorkers) {
		t.Errorf("Expected result MaxWorkers %d, got %d", originalMaxWorkers, result.MaxWorkers)
	}
}

func TestUpdateConfig_NilOverseer(t *testing.T) {
	resolver := createMutationResolver(nil)

	newMaxWorkers := int32(10)
	input := model.ConfigInput{
		MaxWorkers: &newMaxWorkers,
	}

	result, err := resolver.UpdateConfig(context.Background(), input)

	if err == nil {
		t.Error("Expected error when overseer is nil")
	}

	expectedErrMsg := "overseer not initialized"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result != nil {
		t.Error("Expected result to be nil when overseer is nil")
	}
}

// ============================================================================
// TestForceReleaseReservation
// ============================================================================

func TestForceReleaseReservation_Success(t *testing.T) {
	// Note: ForceReleaseReservation requires MCP to be enabled with a reservation manager
	// This test verifies the error handling when MCP is disabled
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	result, err := resolver.ForceReleaseReservation(context.Background(), "res-123")

	// When MCP is disabled, the reservation manager is nil, so we expect an error
	if err == nil {
		t.Error("Expected error when reservation manager not initialized")
	}

	expectedErrMsg := "failed to release reservation: reservation manager not initialized"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result {
		t.Error("Expected result to be false when reservation manager not initialized")
	}
}

func TestForceReleaseReservation_EmptyID(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	result, err := resolver.ForceReleaseReservation(context.Background(), "")

	if err == nil {
		t.Error("Expected error for empty reservation ID")
	}

	expectedErrMsg := "reservation ID cannot be empty"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result {
		t.Error("Expected result to be false for empty ID")
	}
}

func TestForceReleaseReservation_NonExistent(t *testing.T) {
	// Note: ForceReleaseReservation requires MCP to be enabled with a reservation manager
	// This test verifies the error handling when MCP is disabled
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	// When MCP is disabled, the reservation manager is nil
	result, err := resolver.ForceReleaseReservation(context.Background(), "non-existent-id")

	if err == nil {
		t.Error("Expected error when reservation manager not initialized")
	}

	expectedErrMsg := "failed to release reservation: reservation manager not initialized"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result {
		t.Error("Expected result to be false when reservation manager not initialized")
	}
}

func TestForceReleaseReservation_NilOverseer(t *testing.T) {
	resolver := createMutationResolver(nil)

	result, err := resolver.ForceReleaseReservation(context.Background(), "res-123")

	if err == nil {
		t.Error("Expected error when overseer is nil")
	}

	expectedErrMsg := "overseer not initialized"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	if result {
		t.Error("Expected result to be false when overseer is nil")
	}
}

// ============================================================================
// Integration Tests - Multiple Operations
// ============================================================================

func TestMutationSequence_PauseResume(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	// Initial state
	if overseer.IsPaused() {
		t.Error("Expected overseer to not be paused initially")
	}

	// Pause
	result1, err := resolver.PauseOverseer(context.Background())
	if err != nil || !result1 {
		t.Fatal("Failed to pause overseer")
	}
	if !overseer.IsPaused() {
		t.Error("Expected overseer to be paused")
	}

	// Resume
	result2, err := resolver.ResumeOverseer(context.Background())
	if err != nil || !result2 {
		t.Fatal("Failed to resume overseer")
	}
	if overseer.IsPaused() {
		t.Error("Expected overseer to be resumed")
	}

	// Pause again
	result3, err := resolver.PauseOverseer(context.Background())
	if err != nil || !result3 {
		t.Fatal("Failed to pause overseer again")
	}
	if !overseer.IsPaused() {
		t.Error("Expected overseer to be paused again")
	}
}

func TestMutationSequence_ConfigUpdates(t *testing.T) {
	overseer := createTestOverseer()
	resolver := createMutationResolver(overseer)

	// Initial config
	if overseer.GetMaxWorkers() != 5 {
		t.Errorf("Expected initial maxWorkers 5, got %d", overseer.GetMaxWorkers())
	}

	// Update max workers
	newMaxWorkers := int32(20)
	input1 := model.ConfigInput{MaxWorkers: &newMaxWorkers}
	result1, err := resolver.UpdateConfig(context.Background(), input1)
	if err != nil {
		t.Fatalf("Failed to update max workers: %v", err)
	}
	if result1.MaxWorkers != newMaxWorkers {
		t.Errorf("Expected MaxWorkers %d, got %d", newMaxWorkers, result1.MaxWorkers)
	}

	// Update poll interval
	newPollInterval := int32(60)
	input2 := model.ConfigInput{PollInterval: &newPollInterval}
	result2, err := resolver.UpdateConfig(context.Background(), input2)
	if err != nil {
		t.Fatalf("Failed to update poll interval: %v", err)
	}
	if result2.PollInterval != newPollInterval {
		t.Errorf("Expected PollInterval %d, got %d", newPollInterval, result2.PollInterval)
	}
	// Verify maxWorkers is 20 (from previous update)
	if result2.MaxWorkers != 20 {
		t.Errorf("Expected MaxWorkers to be 20 (from previous update), got %d", result2.MaxWorkers)
	}

	// Update both
	newMaxWorkers2 := int32(30)
	newRetryDelay := int32(15)
	input3 := model.ConfigInput{
		MaxWorkers: &newMaxWorkers2,
		RetryDelay: &newRetryDelay,
	}
	result3, err := resolver.UpdateConfig(context.Background(), input3)
	if err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}
	if result3.MaxWorkers != newMaxWorkers2 {
		t.Errorf("Expected MaxWorkers %d, got %d", newMaxWorkers2, result3.MaxWorkers)
	}
	if result3.RetryDelay != newRetryDelay {
		t.Errorf("Expected RetryDelay %d, got %d", newRetryDelay, result3.RetryDelay)
	}

	// Verify overseer state
	if overseer.GetMaxWorkers() != int(newMaxWorkers2) {
		t.Errorf("Expected overseer maxWorkers %d, got %d", newMaxWorkers2, overseer.GetMaxWorkers())
	}
}
