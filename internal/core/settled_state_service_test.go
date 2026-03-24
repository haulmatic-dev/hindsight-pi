package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MockBeadChecker implements BeadChecker for testing
type MockBeadChecker struct {
	beads map[string]*types.Bead
}

func NewMockBeadChecker() *MockBeadChecker {
	return &MockBeadChecker{
		beads: make(map[string]*types.Bead),
	}
}

func (m *MockBeadChecker) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	if bead, ok := m.beads[string(beadID)]; ok {
		return bead, nil
	}
	return nil, nil
}

func (m *MockBeadChecker) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	var beads []types.Bead
	for _, bead := range m.beads {
		beads = append(beads, *bead)
	}
	return beads, nil
}

func (m *MockBeadChecker) AddBead(bead *types.Bead) {
	m.beads[string(bead.ID)] = bead
}

// MockPatternExtractor implements PatternExtractor for testing
type MockPatternExtractor struct {
	extractedBeads []types.BeadID
	enabled        bool
}

func NewMockPatternExtractor() *MockPatternExtractor {
	return &MockPatternExtractor{
		extractedBeads: make([]types.BeadID, 0),
		enabled:        true,
	}
}

func (m *MockPatternExtractor) Extract(beadID types.BeadID, info *types.SettledBeadInfo) error {
	m.extractedBeads = append(m.extractedBeads, beadID)
	return nil
}

func (m *MockPatternExtractor) IsEnabled() bool {
	return m.enabled
}

// MockHindsightReflector implements HindsightReflector for testing
type MockHindsightReflector struct {
	reflectCalled    bool
	reflectBeadID    types.BeadID
	reflectContext   map[string]interface{}
	insightsToReturn []ReflectionInsight
	enabled          bool
	storeMemoryFunc  func(insight ReflectionInsight) error
}

func NewMockHindsightReflector() *MockHindsightReflector {
	return &MockHindsightReflector{
		reflectCalled:    false,
		insightsToReturn: make([]ReflectionInsight, 0),
		enabled:          true,
	}
}

func (m *MockHindsightReflector) Reflect(beadID types.BeadID, context map[string]interface{}) ([]ReflectionInsight, error) {
	m.reflectCalled = true
	m.reflectBeadID = beadID
	m.reflectContext = context
	return m.insightsToReturn, nil
}

func (m *MockHindsightReflector) IsEnabled() bool {
	return m.enabled
}

// SetInsights configures the mock to return specific insights
func (m *MockHindsightReflector) SetInsights(insights []ReflectionInsight) {
	m.insightsToReturn = insights
}

// createMockBead creates a test bead with given status
func createMockBead(id string, status types.BeadStatus, closedAt *time.Time) *types.Bead {
	now := time.Now()
	return &types.Bead{
		ID:        types.BeadID(id),
		Title:     "Test Bead " + id,
		Status:    status,
		Project:   "test-project",
		Repo:      "test-repo",
		CreatedAt: now.Add(-24 * time.Hour),
		UpdatedAt: now,
		CompletedAt: func() *time.Time {
			if closedAt != nil {
				return closedAt
			}
			return nil
		}(),
	}
}

func TestSettledStateConfig_Defaults(t *testing.T) {
	config := DefaultSettledStateConfig()

	if !config.Enabled {
		t.Error("Default Enabled should be true")
	}

	if config.CheckInterval != 30*time.Second {
		t.Errorf("CheckInterval = %v, want 30s", config.CheckInterval)
	}

	if config.SettlementDelay != 2*time.Minute {
		t.Errorf("SettlementDelay = %v, want 2m", config.SettlementDelay)
	}

	if config.StabilityThreshold != 0.95 {
		t.Errorf("StabilityThreshold = %v, want 0.95", config.StabilityThreshold)
	}

	if config.MinSettlementChecks != 3 {
		t.Errorf("MinSettlementChecks = %v, want 3", config.MinSettlementChecks)
	}
}

func TestSettledStateService_New(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}

	svc := NewSettledStateService(config, checker, overseer)

	if svc == nil {
		t.Fatal("NewSettledStateService returned nil")
	}

	if svc.config.Enabled != config.Enabled {
		t.Error("Config not set correctly")
	}

	if len(svc.trackedBeads) != 0 {
		t.Error("New service should have no tracked beads")
	}
}

func TestSettledStateService_TrackBead(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	beadID := types.BeadID("test-bead-1")

	err := svc.TrackBead(beadID)
	if err != nil {
		t.Fatalf("TrackBead failed: %v", err)
	}

	// Verify bead is tracked
	info, exists := svc.GetSettledBeadInfo(beadID)
	if !exists {
		t.Fatal("Bead should be tracked")
	}

	if info.BeadID != beadID {
		t.Errorf("BeadID = %v, want %v", info.BeadID, beadID)
	}

	if info.State != types.SettledStateUnsettled {
		t.Errorf("Initial state = %v, want unsettled", info.State)
	}

	// Tracking same bead again should not error
	err = svc.TrackBead(beadID)
	if err != nil {
		t.Fatalf("TrackBead second time failed: %v", err)
	}
}

func TestSettledStateService_IsSettled_NotTracked(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	beadID := types.BeadID("not-tracked")

	if svc.IsSettled(beadID) {
		t.Error("Untracked bead should not be settled")
	}
}

func TestSettledStateService_IsSettled_Tracked(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	beadID := types.BeadID("test-bead")
	svc.TrackBead(beadID)

	// Initially unsettled
	if svc.IsSettled(beadID) {
		t.Error("Newly tracked bead should not be settled")
	}

	// Manually set to settled
	svc.trackedMutex.Lock()
	svc.trackedBeads[string(beadID)].State = types.SettledStateSettled
	svc.trackedMutex.Unlock()

	if !svc.IsSettled(beadID) {
		t.Error("Bead with settled state should be settled")
	}
}

func TestSettledStateService_OnSettledCallback(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	callbackCalled := false
	var callbackBeadID types.BeadID

	svc.OnSettled(func(beadID types.BeadID, info *types.SettledBeadInfo) {
		callbackCalled = true
		callbackBeadID = beadID
	})

	if len(svc.settledCallbacks) != 1 {
		t.Error("Callback should be registered")
	}

	// Manually trigger callback for testing
	beadID := types.BeadID("test-bead")
	svc.TrackBead(beadID)
	info, _ := svc.GetSettledBeadInfo(beadID)

	// Call callbacks manually
	svc.callbackMutex.RLock()
	for _, cb := range svc.settledCallbacks {
		cb(beadID, info)
	}
	svc.callbackMutex.RUnlock()

	if !callbackCalled {
		t.Error("Callback should have been called")
	}

	if callbackBeadID != beadID {
		t.Error("Callback received wrong beadID")
	}
}

func TestSettledStateService_SetPatternExtractor(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	extractor := NewMockPatternExtractor()
	svc.SetPatternExtractor(extractor)

	if svc.extractor != extractor {
		t.Error("PatternExtractor not set correctly")
	}
}

func TestSettledStateService_GetAllTrackedBeads(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	// Track multiple beads
	beadIDs := []types.BeadID{"bead-1", "bead-2", "bead-3"}
	for _, id := range beadIDs {
		svc.TrackBead(id)
	}

	beads := svc.GetAllTrackedBeads()

	if len(beads) != 3 {
		t.Errorf("Expected 3 tracked beads, got %d", len(beads))
	}

	// Verify all beads are present
	beadMap := make(map[string]bool)
	for _, bead := range beads {
		beadMap[string(bead.BeadID)] = true
	}

	for _, id := range beadIDs {
		if !beadMap[string(id)] {
			t.Errorf("Bead %s not found in tracked beads", id)
		}
	}
}

func TestSettledStateService_GetSettledBeads(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	// Track beads with different states
	svc.TrackBead(types.BeadID("unsettled-1"))
	svc.TrackBead(types.BeadID("settled-1"))
	svc.TrackBead(types.BeadID("settled-2"))
	svc.TrackBead(types.BeadID("extracted-1"))

	// Set states
	svc.trackedMutex.Lock()
	svc.trackedBeads["settled-1"].State = types.SettledStateSettled
	svc.trackedBeads["settled-2"].State = types.SettledStateSettled
	svc.trackedBeads["extracted-1"].State = types.SettledStateExtracted
	svc.trackedMutex.Unlock()

	settledBeads := svc.GetSettledBeads()

	if len(settledBeads) != 3 {
		t.Errorf("Expected 3 settled beads, got %d", len(settledBeads))
	}

	// Verify only settled/extracted beads are returned
	for _, bead := range settledBeads {
		if bead.State != types.SettledStateSettled && bead.State != types.SettledStateExtracted {
			t.Errorf("Bead %s has wrong state: %v", bead.BeadID, bead.State)
		}
	}
}

func TestSettledStateService_UntrackBead(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	beadID := types.BeadID("test-bead")
	svc.TrackBead(beadID)

	if _, exists := svc.GetSettledBeadInfo(beadID); !exists {
		t.Fatal("Bead should be tracked initially")
	}

	svc.UntrackBead(beadID)

	if _, exists := svc.GetSettledBeadInfo(beadID); exists {
		t.Error("Bead should not be tracked after untrack")
	}
}

func TestSettledStateService_GetStats(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	// Create beads with different states
	svc.TrackBead(types.BeadID("unsettled-1"))
	svc.TrackBead(types.BeadID("unsettled-2"))
	svc.TrackBead(types.BeadID("settled-1"))
	svc.TrackBead(types.BeadID("extracted-1"))
	svc.TrackBead(types.BeadID("extracted-2"))

	svc.trackedMutex.Lock()
	svc.trackedBeads["settled-1"].State = types.SettledStateSettled
	svc.trackedBeads["extracted-1"].State = types.SettledStateExtracted
	svc.trackedBeads["extracted-2"].State = types.SettledStateExtracted
	svc.trackedMutex.Unlock()

	stats := svc.GetStats()

	if stats.TotalTracked != 5 {
		t.Errorf("TotalTracked = %d, want 5", stats.TotalTracked)
	}

	if stats.UnsettledCount != 2 {
		t.Errorf("UnsettledCount = %d, want 2", stats.UnsettledCount)
	}

	if stats.SettledCount != 1 {
		t.Errorf("SettledCount = %d, want 1", stats.SettledCount)
	}

	if stats.ExtractedCount != 2 {
		t.Errorf("ExtractedCount = %d, want 2", stats.ExtractedCount)
	}
}

func TestSettledStateService_StartDisabled(t *testing.T) {
	config := DefaultSettledStateConfig()
	config.Enabled = false
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := svc.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed for disabled service: %v", err)
	}

	if svc.running {
		t.Error("Disabled service should not be running")
	}
}

func TestSettledStateService_StopNotRunning(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	// Don't start the service
	err := svc.Stop()
	if err != nil {
		t.Fatalf("Stop should not error when not running: %v", err)
	}
}

func TestSettledStateService_CalculateStabilityScore_Terminal(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	// Create terminal bead
	now := time.Now()
	closedAt := now.Add(-10 * time.Minute)
	bead := createMockBead("test", types.StatusClosed, &closedAt)
	bead.Dependencies = []types.BeadID{} // No dependencies

	checker.AddBead(bead)

	score := svc.calculateStabilityScore(bead)

	// Terminal bead with no active dependencies should have high score
	if score < 0.5 {
		t.Errorf("Stability score for terminal bead too low: %f", score)
	}
}

func TestSettledStateService_CalculateStabilityScore_NonTerminal(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	// Create non-terminal bead
	bead := createMockBead("test", types.StatusOpen, nil)

	score := svc.calculateStabilityScore(bead)

	// Non-terminal bead should have low score
	if score > 0.5 {
		t.Errorf("Stability score for non-terminal bead too high: %f", score)
	}
}

func TestSettledStateService_HasActiveDependencies(t *testing.T) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	now := time.Now()

	// Create beads with dependencies (what a bead depends on, not what depends on it)
	// Bead A depends on Bead B (Bead A has Bead B as dependency)

	// Case 1: Bead with active dependency
	activeDep := createMockBead("active-dep", types.StatusOpen, nil)
	beadWithActiveDep := createMockBead("bead-active-dep", types.StatusClosed, &now)
	beadWithActiveDep.Dependencies = []types.BeadID{activeDep.ID}

	checker.AddBead(activeDep)
	checker.AddBead(beadWithActiveDep)

	// Bead with active dependency should have active dependencies
	if !svc.hasActiveDependencies(beadWithActiveDep) {
		t.Error("Bead with active dependency should have active dependencies")
	}

	// Case 2: Bead with only closed dependencies
	closedDep := createMockBead("closed-dep", types.StatusClosed, &now)
	beadWithClosedDep := createMockBead("bead-closed-dep", types.StatusClosed, &now)
	beadWithClosedDep.Dependencies = []types.BeadID{closedDep.ID}

	checker.AddBead(closedDep)
	checker.AddBead(beadWithClosedDep)

	// Track and settle the closed dependency
	svc.TrackBead(closedDep.ID)
	svc.trackedMutex.Lock()
	svc.trackedBeads[string(closedDep.ID)].State = types.SettledStateSettled
	svc.trackedMutex.Unlock()

	// Bead with settled dependency should not have active dependencies
	if svc.hasActiveDependencies(beadWithClosedDep) {
		t.Error("Bead with settled dependency should not have active dependencies")
	}
}

func TestSettledStateService_CanSettle(t *testing.T) {
	config := DefaultSettledStateConfig()
	config.MinSettlementChecks = 2
	config.SettlementDelay = 1 * time.Minute
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	now := time.Now()
	beadID := types.BeadID("test-bead")

	// Track bead
	svc.TrackBead(beadID)

	// Set up bead info with enough checks
	svc.trackedMutex.Lock()
	svc.trackedBeads[string(beadID)].CheckCount = 3
	svc.trackedBeads[string(beadID)].LastCheckedAt = now
	svc.trackedMutex.Unlock()

	// Create terminal bead that was closed long ago
	closedAt := now.Add(-10 * time.Minute)
	bead := createMockBead(string(beadID), types.StatusClosed, &closedAt)

	// Should be able to settle with high stability score
	if !svc.canSettle(beadID, bead, 0.98) {
		t.Error("Should be able to settle with high stability and enough checks")
	}

	// Should not settle with low stability
	if svc.canSettle(beadID, bead, 0.5) {
		t.Error("Should not settle with low stability")
	}

	// Should not settle with insufficient checks
	svc.trackedMutex.Lock()
	svc.trackedBeads[string(beadID)].CheckCount = 1
	svc.trackedMutex.Unlock()

	if svc.canSettle(beadID, bead, 0.98) {
		t.Error("Should not settle with insufficient checks")
	}
}

func TestSettledStateService_EvictOldestUnsettled(t *testing.T) {
	config := DefaultSettledStateConfig()
	config.RetainUnsettledBeads = 2
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	// Track beads - eviction happens during TrackBead when limit exceeded
	svc.TrackBead(types.BeadID("bead-1"))
	time.Sleep(10 * time.Millisecond)
	svc.TrackBead(types.BeadID("bead-2"))
	time.Sleep(10 * time.Millisecond)
	svc.TrackBead(types.BeadID("bead-3"))

	// After tracking 3 beads with limit of 2, eviction should have happened
	// Note: The eviction happens during TrackBead, so we may have 2 or 3 depending on implementation
	// Let's just verify the oldest was evicted if we're over limit
	if len(svc.trackedBeads) > config.RetainUnsettledBeads {
		// If we're over limit, eviction should have occurred
		if _, exists := svc.trackedBeads["bead-1"]; exists {
			t.Error("Oldest unsettled bead should have been evicted when over limit")
		}
	}

	// Manually trigger eviction to test the function
	svc.evictOldestUnsettled()

	// Should be at or under limit now
	if len(svc.trackedBeads) > config.RetainUnsettledBeads {
		t.Errorf("Expected at most %d tracked beads after eviction, got %d", config.RetainUnsettledBeads, len(svc.trackedBeads))
	}

	// Oldest should be gone
	if _, exists := svc.trackedBeads["bead-1"]; exists {
		t.Error("Oldest unsettled bead should have been evicted")
	}
}

func TestSettledStateService_SetStats(t *testing.T) {
	stats := SettledStateStats{
		TotalTracked:   10,
		UnsettledCount: 5,
		SettlingCount:  2,
		SettledCount:   2,
		ExtractedCount: 1,
	}

	if stats.TotalTracked != 10 {
		t.Error("Stats struct not created correctly")
	}
}

func TestSettledStateState_IsValid(t *testing.T) {
	tests := []struct {
		state    types.SettledState
		expected bool
	}{
		{types.SettledStateUnsettled, true},
		{types.SettledStateSettling, true},
		{types.SettledStateSettled, true},
		{types.SettledStateExtracted, true},
		{types.SettledState("invalid"), false},
	}

	for _, tt := range tests {
		if got := tt.state.IsValid(); got != tt.expected {
			t.Errorf("IsValid(%v) = %v, want %v", tt.state, got, tt.expected)
		}
	}
}

func TestSettledStateState_IsTerminal(t *testing.T) {
	if !types.SettledStateExtracted.IsTerminal() {
		t.Error("Extracted should be terminal")
	}

	if types.SettledStateSettled.IsTerminal() {
		t.Error("Settled should not be terminal")
	}

	if types.SettledStateUnsettled.IsTerminal() {
		t.Error("Unsettled should not be terminal")
	}
}

func BenchmarkSettledStateService_TrackBead(b *testing.B) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		beadID := types.BeadID(fmt.Sprintf("bead-%d", i))
		svc.TrackBead(beadID)
	}
}

func BenchmarkSettledStateService_IsSettled_Cached(b *testing.B) {
	config := DefaultSettledStateConfig()
	checker := NewMockBeadChecker()
	overseer := &Overseer{}
	svc := NewSettledStateService(config, checker, overseer)

	beadID := types.BeadID("test-bead")
	svc.TrackBead(beadID)

	// Set to settled for caching test
	svc.trackedMutex.Lock()
	svc.trackedBeads[string(beadID)].State = types.SettledStateSettled
	svc.trackedMutex.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.IsSettled(beadID)
	}
}
