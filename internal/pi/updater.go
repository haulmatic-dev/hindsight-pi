// Package pi provides automatic setup and configuration for Pi agent ecosystem
package pi

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"
)

// UpdateCache stores cached update results to avoid redundant checks
type UpdateCache struct {
	LastUpdateTime time.Time `json:"last_update_time"`
	SuccessCount   int       `json:"success_count"`
	FailedCount    int       `json:"failed_count"`
	TotalSkills    int       `json:"total_skills"`
	mu             sync.RWMutex
}

// IsValid returns true if the cache is valid and recent (within update interval)
func (uc *UpdateCache) IsValid(interval time.Duration) bool {
	uc.mu.RLock()
	defer uc.mu.RUnlock()

	if uc.LastUpdateTime.IsZero() {
		return false
	}

	// Cache is valid if update was within the interval
	return time.Since(uc.LastUpdateTime) < interval
}

// Update updates the cache with new results
func (uc *UpdateCache) Update(successCount, failedCount, totalSkills int) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	uc.LastUpdateTime = time.Now()
	uc.SuccessCount = successCount
	uc.FailedCount = failedCount
	uc.TotalSkills = totalSkills
}

// GetStats returns cache statistics
func (uc *UpdateCache) GetStats() (time.Time, int, int, int) {
	uc.mu.RLock()
	defer uc.mu.RUnlock()

	return uc.LastUpdateTime, uc.SuccessCount, uc.FailedCount, uc.TotalSkills
}

// SkillUpdater manages periodic skill updates via background goroutine
type SkillUpdater struct {
	setupManager   *SetupManager
	updateInterval time.Duration
	stopCh         chan struct{}
	wg             sync.WaitGroup
	running        bool
	mu             sync.RWMutex

	// Optimization: Cache to prevent redundant updates
	cache *UpdateCache

	// Optimization: In-flight tracking to prevent concurrent updates
	updateInProgress bool
	updateMu         sync.Mutex

	// Optimization: Ticker control for dynamic reset
	tickerResetCh chan struct{}
}

// updateResult contains the result of a single skill update
type updateResult struct {
	skillName string
	success   bool
	err       error
}

// maxConcurrentUpdates limits parallel skill updates to prevent resource exhaustion
const maxConcurrentUpdates = 4

// NewSkillUpdater creates a new skill updater with the specified interval
// intervalStr should be a duration string like "24h", "1h", "30m", etc.
func NewSkillUpdater(setupManager *SetupManager, intervalStr string) (*SkillUpdater, error) {
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return nil, fmt.Errorf("invalid update_interval %q: %w", intervalStr, err)
	}

	if interval <= 0 {
		return nil, fmt.Errorf("update_interval must be positive, got %v", interval)
	}

	return &SkillUpdater{
		setupManager:   setupManager,
		updateInterval: interval,
		stopCh:         make(chan struct{}),
		cache:          &UpdateCache{},
		tickerResetCh:  make(chan struct{}, 1),
	}, nil
}

// Start begins the background skill update goroutine
func (su *SkillUpdater) Start() {
	su.mu.Lock()
	defer su.mu.Unlock()

	if su.running {
		log.Println("[SkillUpdater] Already running, skipping Start()")
		return
	}

	su.running = true
	su.wg.Add(1)

	log.Printf("[SkillUpdater] Starting with update interval: %v", su.updateInterval)

	go su.run()
}

// Stop gracefully shuts down the skill updater
func (su *SkillUpdater) Stop() {
	su.mu.Lock()
	if !su.running {
		su.mu.Unlock()
		return
	}
	su.running = false
	su.mu.Unlock()

	log.Println("[SkillUpdater] Stopping...")
	close(su.stopCh)

	// Wait for goroutine to finish with timeout
	done := make(chan struct{})
	go func() {
		su.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("[SkillUpdater] Stopped successfully")
	case <-time.After(5 * time.Second):
		log.Println("[SkillUpdater] Stop timed out, forcing exit")
	}
}

// IsRunning returns whether the updater is currently running
func (su *SkillUpdater) IsRunning() bool {
	su.mu.RLock()
	defer su.mu.RUnlock()
	return su.running
}

// GetUpdateInterval returns the configured update interval
func (su *SkillUpdater) GetUpdateInterval() time.Duration {
	su.mu.RLock()
	defer su.mu.RUnlock()
	return su.updateInterval
}

// run is the main goroutine that manages the update ticker
func (su *SkillUpdater) run() {
	defer su.wg.Done()

	// Perform initial update on startup
	log.Println("[SkillUpdater] Performing initial skill update...")
	if err := su.runUpdate(); err != nil {
		log.Printf("[SkillUpdater] Initial update completed with errors: %v", err)
	} else {
		log.Println("[SkillUpdater] Initial update completed successfully")
	}

	// Create ticker for periodic updates
	ticker := time.NewTicker(su.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-su.stopCh:
			log.Println("[SkillUpdater] Received stop signal, exiting")
			return
		case <-ticker.C:
			log.Println("[SkillUpdater] Running scheduled skill update...")
			duration := su.runUpdateWithTiming()

			// Optimization: Dynamically adjust ticker based on update duration
			// If update took longer than half the interval, reset ticker to prevent back-to-back runs
			if duration > su.updateInterval/2 {
				log.Printf("[SkillUpdater] Update took %v, resetting ticker", duration)
				ticker.Reset(su.updateInterval)
			}
		}
	}
}

// runUpdateWithTiming runs an update and returns the duration
func (su *SkillUpdater) runUpdateWithTiming() time.Duration {
	start := time.Now()
	if err := su.runUpdate(); err != nil {
		log.Printf("[SkillUpdater] Scheduled update completed with errors: %v", err)
	} else {
		log.Println("[SkillUpdater] Scheduled update completed successfully")
	}
	return time.Since(start)
}

// runUpdate performs the actual skill update with parallelization and caching
// Re-installs all skills and handles partial failures gracefully
func (su *SkillUpdater) runUpdate() error {
	// Optimization: Prevent concurrent update runs
	su.updateMu.Lock()
	if su.updateInProgress {
		su.updateMu.Unlock()
		log.Println("[SkillUpdater] Update already in progress, skipping")
		return nil
	}
	su.updateInProgress = true
	su.updateMu.Unlock()

	defer func() {
		su.updateMu.Lock()
		su.updateInProgress = false
		su.updateMu.Unlock()
	}()

	// Optimization: Check cache to avoid redundant updates
	if su.cache.IsValid(su.updateInterval) {
		lastUpdate, successCount, failedCount, totalSkills := su.cache.GetStats()
		log.Printf("[SkillUpdater] Using cached results from %v (success: %d, failed: %d, total: %d)",
			lastUpdate.Format(time.RFC3339), successCount, failedCount, totalSkills)
		return nil
	}

	log.Printf("[SkillUpdater] Starting parallel skill update for %d skills (max %d concurrent)",
		len(DefaultSkills), maxConcurrentUpdates)

	startTime := time.Now()

	// Optimization: Parallel skill updates with semaphore
	results := su.runParallelUpdates()

	// Collect results
	var updatedSkills, failedSkills []string
	for _, result := range results {
		if result.success {
			updatedSkills = append(updatedSkills, result.skillName)
			su.logSkillUpdate(result.skillName, "success", "")
			log.Printf("[SkillUpdater] ✅ Skill %s updated successfully", result.skillName)
		} else {
			failedSkills = append(failedSkills, result.skillName)
			su.logSkillUpdate(result.skillName, "failed", result.err.Error())
			log.Printf("[SkillUpdater] ❌ Failed to update skill %s: %v", result.skillName, result.err)
		}
	}

	// Log summary
	totalSuccess := len(updatedSkills)
	totalFailed := len(failedSkills)
	su.logUpdateSummary(totalSuccess, totalFailed, startTime)

	// Optimization: Update cache with results
	su.cache.Update(totalSuccess, totalFailed, len(DefaultSkills))

	// Update tracking file with new timestamp
	if err := su.updateTrackingFile(updatedSkills, failedSkills, startTime); err != nil {
		log.Printf("[SkillUpdater] Warning: failed to update tracking file: %v", err)
	}

	if len(failedSkills) > 0 {
		log.Printf("[SkillUpdater] ⚠️ Warning: Failed to update %d skills: %v", len(failedSkills), failedSkills)
		log.Printf("[SkillUpdater] Continuing with %d successfully updated skills", len(updatedSkills))
	} else {
		log.Printf("[SkillUpdater] Successfully updated all %d skills in %v", len(updatedSkills), time.Since(startTime))
	}

	return nil
}

// runParallelUpdates executes skill updates in parallel with semaphore control
func (su *SkillUpdater) runParallelUpdates() []updateResult {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrentUpdates)
	resultsChan := make(chan updateResult, len(DefaultSkills))

	for _, skill := range DefaultSkills {
		// Check for stop signal before starting new goroutine
		select {
		case <-su.stopCh:
			log.Println("[SkillUpdater] Update interrupted by stop signal, skipping remaining skills")
			resultsChan <- updateResult{skillName: skill.Name, success: false, err: fmt.Errorf("update interrupted")}
			continue
		default:
		}

		wg.Add(1)
		go func(s Skill) {
			defer wg.Done()

			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			log.Printf("[SkillUpdater] Updating skill: %s (%s)...", s.Name, s.Repo)

			if err := su.updateSkill(s); err != nil {
				resultsChan <- updateResult{skillName: s.Name, success: false, err: err}
			} else {
				resultsChan <- updateResult{skillName: s.Name, success: true, err: nil}
			}
		}(skill)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect all results
	var results []updateResult
	for result := range resultsChan {
		results = append(results, result)
	}

	return results
}

// logSkillUpdate logs a structured JSON event for skill update status
// Format: {"event": "skill_update", "skill": "name", "status": "success"}
func (su *SkillUpdater) logSkillUpdate(skillName string, status string, errorMsg string) {
	event := map[string]string{
		"event":  "skill_update",
		"skill":  skillName,
		"status": status,
	}
	if errorMsg != "" {
		event["error"] = errorMsg
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		log.Printf("[SkillUpdater] Failed to marshal skill update log: %v", err)
		return
	}

	log.Printf("[SkillUpdater] %s", string(jsonBytes))
}

// logUpdateSummary logs a structured JSON summary of the update operation
func (su *SkillUpdater) logUpdateSummary(successCount int, failedCount int, startTime time.Time) {
	duration := time.Since(startTime)
	summary := map[string]interface{}{
		"event":    "update_summary",
		"success":  successCount,
		"failed":   failedCount,
		"total":    len(DefaultSkills),
		"duration": duration.String(),
	}

	jsonBytes, err := json.Marshal(summary)
	if err != nil {
		log.Printf("[SkillUpdater] Failed to marshal update summary: %v", err)
		return
	}

	log.Printf("[SkillUpdater] %s", string(jsonBytes))
	log.Printf("[SkillUpdater] Update complete: %d succeeded, %d failed out of %d total (took %v)",
		successCount, failedCount, len(DefaultSkills), duration)
}

// updateTrackingFile updates the skills tracking file with the latest update results
func (su *SkillUpdater) updateTrackingFile(updatedSkills []string, failedSkills []string, startTime time.Time) error {
	// Create skill results for tracking
	skills := make([]SkillVerificationResult, 0, len(DefaultSkills))

	// Add successful skills
	for _, skillName := range updatedSkills {
		skills = append(skills, SkillVerificationResult{
			Name:   skillName,
			Status: "updated",
		})
	}

	// Add failed skills
	for _, skillName := range failedSkills {
		skills = append(skills, SkillVerificationResult{
			Name:   skillName,
			Status: "failed",
		})
	}

	// Build tracking data
	tracking := &SkillsVerificationTracking{
		LastUpdated: time.Now(),
		SkillsCount: len(updatedSkills),
		Skills:      skills,
	}

	// Update tracking file
	if err := su.setupManager.UpdateTrackingFile(tracking); err != nil {
		return fmt.Errorf("failed to update tracking file: %w", err)
	}

	log.Printf("[SkillUpdater] Tracking file updated with new timestamp and %d skill results", len(skills))
	return nil
}

// updateSkill updates a single skill using npx skills add
func (su *SkillUpdater) updateSkill(skill Skill) error {
	// Check if skills CLI is available
	if !su.isSkillsCLIAvailable() {
		return fmt.Errorf("skills CLI not available")
	}

	// Use npx to run skills add command
	cmd := exec.Command("npx", "skills", "add", fmt.Sprintf("%s/%s", skill.Repo, skill.Name))
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("skills add command failed: %w (output: %s)", err, string(output))
	}

	log.Printf("[SkillUpdater] Skill update output: %s", string(output))
	return nil
}

// isSkillsCLIAvailable checks if the skills CLI is available
func (su *SkillUpdater) isSkillsCLIAvailable() bool {
	// Check for global skills command
	cmd := exec.Command("which", "skills")
	if err := cmd.Run(); err == nil {
		return true
	}

	// Check if it's available via npx
	cmd = exec.Command("npx", "--yes", "skills", "--version")
	return cmd.Run() == nil
}

// UpdateSkillsNow triggers an immediate skill update (blocking call)
func (su *SkillUpdater) UpdateSkillsNow() error {
	su.mu.RLock()
	if !su.running {
		su.mu.RUnlock()
		return fmt.Errorf("updater not running")
	}
	su.mu.RUnlock()

	return su.runUpdate()
}

// GetCacheStatus returns the current cache status for monitoring
func (su *SkillUpdater) GetCacheStatus() map[string]interface{} {
	if su.cache == nil {
		return map[string]interface{}{
			"initialized": false,
			"valid":       false,
		}
	}

	lastUpdate, successCount, failedCount, totalSkills := su.cache.GetStats()
	return map[string]interface{}{
		"initialized":   true,
		"valid":         su.cache.IsValid(su.updateInterval),
		"last_update":   lastUpdate,
		"success_count": successCount,
		"failed_count":  failedCount,
		"total_skills":  totalSkills,
		"age_seconds":   time.Since(lastUpdate).Seconds(),
	}
}

// InvalidateCache clears the update cache to force a fresh update
func (su *SkillUpdater) InvalidateCache() {
	if su.cache != nil {
		su.cache.Update(0, 0, 0)
		su.cache.mu.Lock()
		su.cache.LastUpdateTime = time.Time{} // Zero time
		su.cache.mu.Unlock()
		log.Println("[SkillUpdater] Cache invalidated")
	}
}

// IsUpdateInProgress returns true if an update is currently running
func (su *SkillUpdater) IsUpdateInProgress() bool {
	su.updateMu.Lock()
	defer su.updateMu.Unlock()
	return su.updateInProgress
}

// CreateDefaultSkillUpdater creates a skill updater with default configuration
func CreateDefaultSkillUpdater(configDir string, piConfigDir string) (*SkillUpdater, error) {
	setupManager := NewSetupManager(configDir, piConfigDir)
	return NewSkillUpdater(setupManager, "24h")
}

// CreateSkillUpdaterFromConfig creates a skill updater from Pi config
func CreateSkillUpdaterFromConfig(configDir string, piConfigDir string, updateInterval string) (*SkillUpdater, error) {
	setupManager := NewSetupManager(configDir, piConfigDir)
	return NewSkillUpdater(setupManager, updateInterval)
}
