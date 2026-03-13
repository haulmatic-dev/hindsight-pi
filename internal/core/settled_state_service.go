package core

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// SettledStateConfig contains configuration for the settled-state detection service
type SettledStateConfig struct {
	Enabled                  bool          `yaml:"enabled"`
	CheckInterval            time.Duration `yaml:"check_interval"`
	SettlementDelay          time.Duration `yaml:"settlement_delay"`           // Time to wait after terminal state
	StabilityThreshold       float64       `yaml:"stability_threshold"`        // Minimum stability score to consider settled
	MaxConcurrentChecks      int           `yaml:"max_concurrent_checks"`      // Limit concurrent bead checks
	ExtractPatternsOnSettle  bool          `yaml:"extract_patterns_on_settle"` // Auto-trigger pattern extraction
	PatternExtractionTimeout time.Duration `yaml:"pattern_extraction_timeout"` // Timeout for extraction
	RetainUnsettledBeads     int           `yaml:"retain_unsettled_beads"`     // Max unsettled beads to track
	MinSettlementChecks      int           `json:"min_settlement_checks"`      // Minimum checks before settlement
}

// DefaultSettledStateConfig returns the default configuration
func DefaultSettledStateConfig() SettledStateConfig {
	return SettledStateConfig{
		Enabled:                  true,
		CheckInterval:            30 * time.Second,
		SettlementDelay:          2 * time.Minute,
		StabilityThreshold:       0.95,
		MaxConcurrentChecks:      10,
		ExtractPatternsOnSettle:  true,
		PatternExtractionTimeout: 5 * time.Minute,
		RetainUnsettledBeads:     1000,
		MinSettlementChecks:      3,
	}
}

// PatternExtractor defines the interface for extracting patterns from settled beads
type PatternExtractor interface {
	Extract(beadID types.BeadID, info *types.SettledBeadInfo) error
	IsEnabled() bool
}

// BeadChecker defines the interface for checking bead state
type BeadChecker interface {
	GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error)
	ListBeads(projectPath, repoName string) ([]types.Bead, error)
}

// SettledStateService monitors beads and determines when they are truly immutable
type SettledStateService struct {
	config           SettledStateConfig
	beadsClient      BeadChecker
	extractor        PatternExtractor
	overseer         *Overseer
	trackedBeads     map[string]*types.SettledBeadInfo
	trackedMutex     sync.RWMutex
	running          bool
	stopChan         chan struct{}
	checkChan        chan types.BeadID
	settledCallbacks []func(types.BeadID, *types.SettledBeadInfo)
	callbackMutex    sync.RWMutex
	wg               sync.WaitGroup
}

// NewSettledStateService creates a new settled-state detection service
func NewSettledStateService(config SettledStateConfig, beadsClient BeadChecker, overseer *Overseer) *SettledStateService {
	return &SettledStateService{
		config:           config,
		beadsClient:      beadsClient,
		overseer:         overseer,
		trackedBeads:     make(map[string]*types.SettledBeadInfo),
		stopChan:         make(chan struct{}),
		checkChan:        make(chan types.BeadID, 100),
		settledCallbacks: make([]func(types.BeadID, *types.SettledBeadInfo), 0),
	}
}

// SetPatternExtractor sets the pattern extractor to use when beads settle
func (s *SettledStateService) SetPatternExtractor(extractor PatternExtractor) {
	s.extractor = extractor
}

// OnSettled registers a callback to be called when a bead becomes settled
func (s *SettledStateService) OnSettled(callback func(types.BeadID, *types.SettledBeadInfo)) {
	s.callbackMutex.Lock()
	defer s.callbackMutex.Unlock()
	s.settledCallbacks = append(s.settledCallbacks, callback)
}

// Start begins the settled-state detection service
func (s *SettledStateService) Start(ctx context.Context) error {
	if !s.config.Enabled {
		log.Println("[SettledState] Service disabled, not starting")
		return nil
	}

	if s.running {
		return fmt.Errorf("settled-state service already running")
	}

	s.running = true
	log.Printf("[SettledState] Starting service (check interval: %v, settlement delay: %v)",
		s.config.CheckInterval, s.config.SettlementDelay)

	// Start the main detection loop
	s.wg.Add(1)
	go s.detectionLoop(ctx)

	// Start the check workers
	for i := 0; i < s.config.MaxConcurrentChecks; i++ {
		s.wg.Add(1)
		go s.checkWorker(ctx)
	}

	return nil
}

// Stop gracefully shuts down the settled-state detection service
func (s *SettledStateService) Stop() error {
	if !s.running {
		return nil
	}

	log.Println("[SettledState] Stopping service...")
	s.running = false
	close(s.stopChan)

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("[SettledState] Service stopped cleanly")
	case <-time.After(10 * time.Second):
		log.Println("[SettledState] Service stop timed out")
	}

	return nil
}

// TrackBead begins tracking a bead for settled-state detection
func (s *SettledStateService) TrackBead(beadID types.BeadID) error {
	if !s.config.Enabled {
		return nil
	}

	s.trackedMutex.Lock()
	defer s.trackedMutex.Unlock()

	beadIDStr := string(beadID)
	if _, exists := s.trackedBeads[beadIDStr]; exists {
		return nil // Already tracking
	}

	// Check if we're at capacity for unsettled beads
	if len(s.trackedBeads) >= s.config.RetainUnsettledBeads {
		// Remove oldest unsettled bead
		s.evictOldestUnsettled()
	}

	info := &types.SettledBeadInfo{
		BeadID:        beadID,
		State:         types.SettledStateUnsettled,
		LastCheckedAt: time.Now(),
		Metadata:      make(map[string]string),
	}

	s.trackedBeads[beadIDStr] = info
	log.Printf("[SettledState] Now tracking bead: %s", beadID)

	// Queue for immediate check
	select {
	case s.checkChan <- beadID:
	default:
		// Channel full, will be checked in next cycle
	}

	return nil
}

// TrackBeads begins tracking multiple beads
func (s *SettledStateService) TrackBeads(beadIDs []types.BeadID) {
	for _, beadID := range beadIDs {
		if err := s.TrackBead(beadID); err != nil {
			log.Printf("[SettledState] Failed to track bead %s: %v", beadID, err)
		}
	}
}

// GetSettledBeadInfo returns the settled-state info for a bead
func (s *SettledStateService) GetSettledBeadInfo(beadID types.BeadID) (*types.SettledBeadInfo, bool) {
	s.trackedMutex.RLock()
	defer s.trackedMutex.RUnlock()

	info, exists := s.trackedBeads[string(beadID)]
	return info, exists
}

// IsSettled returns true if the bead is in settled state
func (s *SettledStateService) IsSettled(beadID types.BeadID) bool {
	info, exists := s.GetSettledBeadInfo(beadID)
	if !exists {
		return false
	}
	return info.State == types.SettledStateSettled || info.State == types.SettledStateExtracted
}

// GetAllTrackedBeads returns all tracked beads
func (s *SettledStateService) GetAllTrackedBeads() []*types.SettledBeadInfo {
	s.trackedMutex.RLock()
	defer s.trackedMutex.RUnlock()

	result := make([]*types.SettledBeadInfo, 0, len(s.trackedBeads))
	for _, info := range s.trackedBeads {
		result = append(result, info)
	}
	return result
}

// GetSettledBeads returns only beads that have reached settled state
func (s *SettledStateService) GetSettledBeads() []*types.SettledBeadInfo {
	s.trackedMutex.RLock()
	defer s.trackedMutex.RUnlock()

	result := make([]*types.SettledBeadInfo, 0)
	for _, info := range s.trackedBeads {
		if info.State == types.SettledStateSettled || info.State == types.SettledStateExtracted {
			result = append(result, info)
		}
	}
	return result
}

// detectionLoop is the main loop that periodically scans for beads to check
func (s *SettledStateService) detectionLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	// Initial scan
	s.scanForBeadsToCheck()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.scanForBeadsToCheck()
		}
	}
}

// scanForBeadsToCheck finds all tracked beads that need checking and queues them
func (s *SettledStateService) scanForBeadsToCheck() {
	s.trackedMutex.RLock()
	beadsToCheck := make([]types.BeadID, 0, len(s.trackedBeads))

	for _, info := range s.trackedBeads {
		// Skip already extracted beads
		if info.State.IsTerminal() {
			continue
		}

		// Skip beads that were checked recently (except for unsettled)
		if info.State != types.SettledStateUnsettled {
			minInterval := s.config.CheckInterval
			if time.Since(info.LastCheckedAt) < minInterval {
				continue
			}
		}

		beadsToCheck = append(beadsToCheck, info.BeadID)
	}
	s.trackedMutex.RUnlock()

	// Queue beads for checking
	for _, beadID := range beadsToCheck {
		select {
		case s.checkChan <- beadID:
		case <-s.stopChan:
			return
		}
	}
}

// checkWorker processes bead checks from the queue
func (s *SettledStateService) checkWorker(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case beadID := <-s.checkChan:
			if err := s.checkBead(beadID); err != nil {
				log.Printf("[SettledState] Error checking bead %s: %v", beadID, err)
			}
		}
	}
}

// checkBead performs the settled-state check for a single bead
func (s *SettledStateService) checkBead(beadID types.BeadID) error {
	s.trackedMutex.Lock()
	info, exists := s.trackedBeads[string(beadID)]
	if !exists {
		s.trackedMutex.Unlock()
		return fmt.Errorf("bead %s not tracked", beadID)
	}

	// Update check metadata
	info.CheckCount++
	info.LastCheckedAt = time.Now()
	currentState := info.State
	s.trackedMutex.Unlock()

	// Skip if already in terminal state
	if currentState.IsTerminal() {
		return nil
	}

	// Fetch current bead state
	bead, err := s.fetchBeadState(beadID)
	if err != nil {
		return fmt.Errorf("failed to fetch bead state: %w", err)
	}

	// Check if bead is in terminal status
	if !bead.Status.IsTerminal() {
		// Bead is not yet complete - update stability score downward
		s.updateStabilityScore(beadID, 0.0)
		return nil
	}

	// Check for active dependencies
	if s.hasActiveDependencies(bead) {
		s.updateStabilityScore(beadID, 0.1)
		return nil
	}

	// Check for active workers on this bead
	if s.isBeadActive(beadID) {
		s.updateStabilityScore(beadID, 0.2)
		return nil
	}

	// Calculate stability score based on time since terminal state
	stabilityScore := s.calculateStabilityScore(bead)
	s.updateStabilityScore(beadID, stabilityScore)

	// Check if bead meets settlement criteria
	if s.canSettle(beadID, bead, stabilityScore) {
		if err := s.settleBead(beadID, bead); err != nil {
			return fmt.Errorf("failed to settle bead: %w", err)
		}
	}

	return nil
}

// fetchBeadState retrieves the current state of a bead from the beads client
func (s *SettledStateService) fetchBeadState(beadID types.BeadID) (*types.Bead, error) {
	// Find project path from overseer
	projectPath := s.findProjectPath(beadID)
	if projectPath == "" {
		return nil, fmt.Errorf("could not find project path for bead %s", beadID)
	}

	repoName := beadID.Repo()
	return s.beadsClient.GetBead(projectPath, repoName, beadID)
}

// findProjectPath finds the project path for a bead ID
func (s *SettledStateService) findProjectPath(beadID types.BeadID) string {
	if s.overseer == nil {
		return ""
	}

	// Try to find the project from overseer's projects
	for _, project := range s.overseer.GetProjects() {
		if project.Name == beadID.Project() {
			if beadID.Repo() != "" && project.Type == types.RepoTypeMulti {
				return project.Path + "/" + beadID.Repo()
			}
			return project.Path
		}
	}

	return ""
}

// hasActiveDependencies checks if a bead has any active (unsettled) dependencies
func (s *SettledStateService) hasActiveDependencies(bead *types.Bead) bool {
	for _, depID := range bead.Dependencies {
		// Check if dependency is tracked and settled
		if !s.IsSettled(depID) {
			// Check if dependency exists and is terminal
			depBead, err := s.fetchBeadState(depID)
			if err != nil {
				// If we can't check, assume it's active to be safe
				return true
			}
			if !depBead.Status.IsTerminal() {
				return true
			}
			// Dependency is terminal but not yet settled
			return true
		}
	}
	return false
}

// isBeadActive checks if there are active workers on this bead
func (s *SettledStateService) isBeadActive(beadID types.BeadID) bool {
	if s.overseer == nil {
		return false
	}

	// Check overseer's active workers
	for _, worker := range s.overseer.GetActiveWorkers() {
		if worker.BeadID == beadID {
			return true
		}
	}

	return false
}

// calculateStabilityScore calculates how stable a bead is based on time and state
func (s *SettledStateService) calculateStabilityScore(bead *types.Bead) float64 {
	if !bead.Status.IsTerminal() {
		return 0.0
	}

	// Calculate time since completion
	var completedAt time.Time
	if bead.CompletedAt != nil {
		completedAt = *bead.CompletedAt
	} else {
		// If no completion time, use updated at
		completedAt = bead.UpdatedAt
	}

	elapsed := time.Since(completedAt)
	if elapsed < 0 {
		elapsed = 0
	}

	// Score increases as time passes, capped at 1.0
	score := float64(elapsed) / float64(s.config.SettlementDelay)
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// updateStabilityScore updates the stability score for a bead
func (s *SettledStateService) updateStabilityScore(beadID types.BeadID, score float64) {
	s.trackedMutex.Lock()
	defer s.trackedMutex.Unlock()

	if info, exists := s.trackedBeads[string(beadID)]; exists {
		// Exponential moving average for stability score
		alpha := 0.3 // Smoothing factor
		info.StabilityScore = alpha*score + (1-alpha)*info.StabilityScore
	}
}

// canSettle determines if a bead meets all criteria for settlement
func (s *SettledStateService) canSettle(beadID types.BeadID, bead *types.Bead, stabilityScore float64) bool {
	s.trackedMutex.RLock()
	info, exists := s.trackedBeads[string(beadID)]
	s.trackedMutex.RUnlock()

	if !exists {
		return false
	}

	// Must have minimum number of checks
	if info.CheckCount < s.config.MinSettlementChecks {
		return false
	}

	// Must be in terminal state
	if !bead.Status.IsTerminal() {
		return false
	}

	// Must have sufficient stability score
	if stabilityScore < s.config.StabilityThreshold {
		return false
	}

	// Must have waited sufficient time
	var completedAt time.Time
	if bead.CompletedAt != nil {
		completedAt = *bead.CompletedAt
	} else {
		completedAt = bead.UpdatedAt
	}

	if time.Since(completedAt) < s.config.SettlementDelay {
		return false
	}

	return true
}

// settleBead marks a bead as settled and triggers callbacks
func (s *SettledStateService) settleBead(beadID types.BeadID, bead *types.Bead) error {
	s.trackedMutex.Lock()
	info, exists := s.trackedBeads[string(beadID)]
	if !exists {
		s.trackedMutex.Unlock()
		return fmt.Errorf("bead %s not tracked", beadID)
	}

	// Update state
	now := time.Now()
	info.State = types.SettledStateSettled
	info.SettledAt = &now
	info.StabilityScore = 1.0

	// Generate immutability hash from bead state
	info.ImmutabilityHash = s.generateImmutabilityHash(bead)

	s.trackedMutex.Unlock()

	log.Printf("[SettledState] Bead settled: %s (checks: %d, stability: %.2f)",
		beadID, info.CheckCount, info.StabilityScore)

	// Trigger callbacks
	s.callbackMutex.RLock()
	callbacks := make([]func(types.BeadID, *types.SettledBeadInfo), len(s.settledCallbacks))
	copy(callbacks, s.settledCallbacks)
	s.callbackMutex.RUnlock()

	for _, callback := range callbacks {
		go callback(beadID, info)
	}

	// Trigger pattern extraction if enabled
	if s.config.ExtractPatternsOnSettle && s.extractor != nil && s.extractor.IsEnabled() {
		go s.extractPatterns(beadID, info)
	}

	return nil
}

// generateImmutabilityHash creates a hash representing the bead's immutable state
func (s *SettledStateService) generateImmutabilityHash(bead *types.Bead) string {
	// Simple hash based on key immutable properties
	data := fmt.Sprintf("%s:%s:%s:%d:%s",
		bead.ID,
		bead.Status,
		bead.CompletedAt,
		bead.Priority,
		bead.Type,
	)
	return fmt.Sprintf("%x", data) // Simple string representation
}

// extractPatterns triggers pattern extraction for a settled bead
func (s *SettledStateService) extractPatterns(beadID types.BeadID, info *types.SettledBeadInfo) {
	log.Printf("[SettledState] Starting pattern extraction for bead: %s", beadID)

	done := make(chan error, 1)
	go func() {
		done <- s.extractor.Extract(beadID, info)
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Printf("[SettledState] Pattern extraction failed for bead %s: %v", beadID, err)
		} else {
			// Mark as extracted
			s.trackedMutex.Lock()
			if trackedInfo, exists := s.trackedBeads[string(beadID)]; exists {
				now := time.Now()
				trackedInfo.State = types.SettledStateExtracted
				trackedInfo.ExtractedAt = &now
			}
			s.trackedMutex.Unlock()
			log.Printf("[SettledState] Pattern extraction complete for bead: %s", beadID)
		}
	case <-time.After(s.config.PatternExtractionTimeout):
		log.Printf("[SettledState] Pattern extraction timed out for bead: %s", beadID)
	}
}

// evictOldestUnsettled removes the oldest unsettled bead when at capacity
func (s *SettledStateService) evictOldestUnsettled() {
	var oldest *types.SettledBeadInfo

	for _, info := range s.trackedBeads {
		if info.State == types.SettledStateUnsettled {
			if oldest == nil || info.LastCheckedAt.Before(oldest.LastCheckedAt) {
				oldest = info
			}
		}
	}

	if oldest != nil {
		delete(s.trackedBeads, string(oldest.BeadID))
		log.Printf("[SettledState] Evicted oldest unsettled bead: %s", oldest.BeadID)
	}
}

// UntrackBead removes a bead from tracking
func (s *SettledStateService) UntrackBead(beadID types.BeadID) {
	s.trackedMutex.Lock()
	defer s.trackedMutex.Unlock()

	delete(s.trackedBeads, string(beadID))
	log.Printf("[SettledState] Stopped tracking bead: %s", beadID)
}

// GetStats returns statistics about the settled-state service
func (s *SettledStateService) GetStats() SettledStateStats {
	s.trackedMutex.RLock()
	defer s.trackedMutex.RUnlock()

	stats := SettledStateStats{
		TotalTracked: len(s.trackedBeads),
	}

	for _, info := range s.trackedBeads {
		switch info.State {
		case types.SettledStateUnsettled:
			stats.UnsettledCount++
		case types.SettledStateSettling:
			stats.SettlingCount++
		case types.SettledStateSettled:
			stats.SettledCount++
		case types.SettledStateExtracted:
			stats.ExtractedCount++
		}
	}

	return stats
}

// SettledStateStats contains statistics about tracked beads
type SettledStateStats struct {
	TotalTracked   int `json:"total_tracked"`
	UnsettledCount int `json:"unsettled_count"`
	SettlingCount  int `json:"settling_count"`
	SettledCount   int `json:"settled_count"`
	ExtractedCount int `json:"extracted_count"`
}
