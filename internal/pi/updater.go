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

// SkillUpdater manages periodic skill updates via background goroutine
type SkillUpdater struct {
	setupManager   *SetupManager
	updateInterval time.Duration
	stopCh         chan struct{}
	wg             sync.WaitGroup
	running        bool
	mu             sync.RWMutex
}

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
			if err := su.runUpdate(); err != nil {
				log.Printf("[SkillUpdater] Scheduled update completed with errors: %v", err)
			} else {
				log.Println("[SkillUpdater] Scheduled update completed successfully")
			}
		}
	}
}

// runUpdate performs the actual skill update
// Re-installs all skills and handles partial failures gracefully
func (su *SkillUpdater) runUpdate() error {
	log.Printf("[SkillUpdater] Starting skill update for %d skills", len(DefaultSkills))

	var updatedSkills []string
	var failedSkills []string
	startTime := time.Now()

	for _, skill := range DefaultSkills {
		select {
		case <-su.stopCh:
			log.Println("[SkillUpdater] Update interrupted by stop signal")
			return fmt.Errorf("update interrupted")
		default:
		}

		log.Printf("[SkillUpdater] Updating skill: %s (%s)...", skill.Name, skill.Repo)

		if err := su.updateSkill(skill); err != nil {
			// Log structured failure event
			su.logSkillUpdate(skill.Name, "failed", err.Error())
			log.Printf("[SkillUpdater] ❌ Failed to update skill %s: %v", skill.Name, err)
			failedSkills = append(failedSkills, skill.Name)
		} else {
			// Log structured success event
			su.logSkillUpdate(skill.Name, "success", "")
			log.Printf("[SkillUpdater] ✅ Skill %s updated successfully", skill.Name)
			updatedSkills = append(updatedSkills, skill.Name)
		}
	}

	// Log summary
	totalSuccess := len(updatedSkills)
	totalFailed := len(failedSkills)
	su.logUpdateSummary(totalSuccess, totalFailed, startTime)

	// Update tracking file with new timestamp
	if err := su.updateTrackingFile(updatedSkills, failedSkills, startTime); err != nil {
		log.Printf("[SkillUpdater] Warning: failed to update tracking file: %v", err)
	}

	if len(failedSkills) > 0 {
		log.Printf("[SkillUpdater] ⚠️ Warning: Failed to update %d skills: %v", len(failedSkills), failedSkills)
		log.Printf("[SkillUpdater] Continuing with %d successfully updated skills", len(updatedSkills))
	} else {
		log.Printf("[SkillUpdater] Successfully updated all %d skills", len(updatedSkills))
	}

	return nil
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
