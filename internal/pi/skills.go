package pi

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Skill represents a Pi skill to install
type Skill struct {
	Name        string `json:"name"`
	Repo        string `json:"repo"`
	Description string `json:"description,omitempty"`
}

// DefaultSkills contains the 13 required skills to install
var DefaultSkills = []Skill{
	{Name: "golang-pro", Repo: "jeffallan/claude-skills", Description: "Go professional development skills"},
	{Name: "golang-testing", Repo: "affaan-m/everything-claude-code", Description: "Go testing patterns and practices"},
	{Name: "golang-patterns", Repo: "affaan-m/everything-claude-code", Description: "Go design patterns"},
	{Name: "angular-component", Repo: "analogjs/angular-skills", Description: "Angular component development"},
	{Name: "angular-signals", Repo: "analogjs/angular-skills", Description: "Angular signals state management"},
	{Name: "angular-testing", Repo: "analogjs/angular-skills", Description: "Angular testing strategies"},
	{Name: "typescript-advanced-types", Repo: "wshobson/agents", Description: "Advanced TypeScript type patterns"},
	{Name: "vitest", Repo: "antfu/skills", Description: "Vitest testing framework"},
	{Name: "implement-design", Repo: "figma/mcp-server-guide", Description: "Design implementation from Figma"},
	{Name: "systematic-debugging", Repo: "obra/superpowers", Description: "Systematic debugging methodology"},
	{Name: "test-driven-development", Repo: "obra/superpowers", Description: "TDD best practices"},
	{Name: "code-review", Repo: "supercent-io/skills-template", Description: "Code review excellence"},
	{Name: "lsp-code-analysis", Repo: "lsp-client/lsp-skill", Description: "LSP-based code analysis"},
}

// SkillsTrackingFile represents the structure of the skills tracking file
type SkillsTrackingFile struct {
	InstalledAt time.Time `json:"installed_at"`
	Skills      []string  `json:"skills"`
	Version     string    `json:"version"`
}

// SkillVerificationResult represents the status of a single skill verification
type SkillVerificationResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// skillFlagsCacheEntry represents a cached skill flags result
type skillFlagsCacheEntry struct {
	result    *SkillFlagsResult
	timestamp time.Time
	configDir string
}

// skillFlagsCache provides thread-safe caching of skill flags with TTL
var (
	skillFlagsCache     = make(map[string]*skillFlagsCacheEntry)
	skillFlagsCacheMu   sync.RWMutex
	skillFlagsCacheTTL  = 1 * time.Minute
	skillFlagsCacheHits int64
	skillFlagsCacheMiss int64
)

// GetSkillFlagsCacheStats returns cache statistics for monitoring
func GetSkillFlagsCacheStats() (hits, misses int64, hitRate float64) {
	skillFlagsCacheMu.RLock()
	defer skillFlagsCacheMu.RUnlock()
	total := skillFlagsCacheHits + skillFlagsCacheMiss
	if total > 0 {
		hitRate = float64(skillFlagsCacheHits) / float64(total) * 100
	}
	return skillFlagsCacheHits, skillFlagsCacheMiss, hitRate
}

// ResetSkillFlagsCache clears the cache and resets stats (useful for testing)
func ResetSkillFlagsCache() {
	skillFlagsCacheMu.Lock()
	defer skillFlagsCacheMu.Unlock()
	skillFlagsCache = make(map[string]*skillFlagsCacheEntry)
	skillFlagsCacheHits = 0
	skillFlagsCacheMiss = 0
}

// SkillsVerificationTracking represents the updated tracking file structure
type SkillsVerificationTracking struct {
	LastUpdated time.Time                 `json:"last_updated"`
	SkillsCount int                       `json:"skills_count"`
	Skills      []SkillVerificationResult `json:"skills"`
}

// CheckAndInstallSkills installs all required skills if not already installed (parallel version)
// Part 1: Install skills concurrently
// Part 2: Verify installation and update tracking (via VerifySkillInstallation)
func (sm *SetupManager) CheckAndInstallSkills() error {
	if sm.AreSkillsInstalled() {
		log.Println("[PiSetup] Skills already installed, verifying...")
		// Even if tracking file exists, verify skills are actually present
		return sm.VerifySkillInstallation()
	}

	log.Printf("[PiSetup] Installing %d skills...", len(DefaultSkills))

	// Concurrent installation with semaphore to limit parallelism
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4) // Max 4 concurrent installations

	installedChan := make(chan string, len(DefaultSkills))
	failedChan := make(chan string, len(DefaultSkills))

	for _, skill := range DefaultSkills {
		wg.Add(1)
		go func(s Skill) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("[PiSetup] Installing skill: %s (%s)...", s.Name, s.Repo)

			if err := sm.installSkill(s); err != nil {
				log.Printf("[PiSetup] ❌ Failed to install skill %s: %v", s.Name, err)
				failedChan <- s.Name
			} else {
				log.Printf("[PiSetup] ✅ Skill %s installed successfully", s.Name)
				installedChan <- s.Name
			}
		}(skill)
	}

	// Close channels when all goroutines complete
	go func() {
		wg.Wait()
		close(installedChan)
		close(failedChan)
	}()

	// Collect results
	var installedSkills []string
	var failedSkills []string

	for name := range installedChan {
		installedSkills = append(installedSkills, name)
	}
	for name := range failedChan {
		failedSkills = append(failedSkills, name)
	}

	// Create tracking file even if some skills failed (backward compatibility)
	tracking := SkillsTrackingFile{
		InstalledAt: time.Now(),
		Skills:      installedSkills,
		Version:     "1.0.0",
	}

	if err := sm.saveSkillsTracking(tracking); err != nil {
		return fmt.Errorf("failed to save skills tracking: %w", err)
	}

	// Part 2: Verify installation and update tracking with detailed results
	log.Println("[PiSetup] Verifying installations...")

	// Create tracking with both successful and failed skills
	verificationResults := &SkillsVerificationTracking{
		LastUpdated: time.Now(),
		Skills:      make([]SkillVerificationResult, 0, len(DefaultSkills)),
	}

	// Add successfully installed skills
	for _, skillName := range installedSkills {
		verificationResults.Skills = append(verificationResults.Skills, SkillVerificationResult{
			Name:   skillName,
			Status: "installed",
		})
	}

	// Add failed skills with "failed" status
	for _, skillName := range failedSkills {
		verificationResults.Skills = append(verificationResults.Skills, SkillVerificationResult{
			Name:   skillName,
			Status: "failed",
		})
	}

	verificationResults.SkillsCount = len(installedSkills)

	// Save detailed tracking
	if err := sm.UpdateTrackingFile(verificationResults); err != nil {
		log.Printf("[PiSetup] Warning: failed to update tracking file: %v", err)
	}

	// Also run verification to check actual filesystem state
	if err := sm.VerifySkillInstallation(); err != nil {
		log.Printf("[PiSetup] ⚠️ Verification warning: %v", err)
		// Don't fail the entire operation if verification has issues
	}

	if len(failedSkills) > 0 {
		log.Printf("[PiSetup] ⚠️ Warning: Failed to install %d skills: %v", len(failedSkills), failedSkills)
		log.Printf("[PiSetup] Continuing with %d successfully installed skills", len(installedSkills))
	} else {
		log.Printf("[PiSetup] Successfully installed and verified all %d skills", len(installedSkills))
	}

	return nil
}

// installSkill installs a single skill using npx skills add
func (sm *SetupManager) installSkill(skill Skill) error {
	// Check if skills CLI is available
	if !sm.isSkillsCLIAvailable() {
		return fmt.Errorf("skills CLI not available")
	}

	// Use npx to run skills add command
	cmd := exec.Command("npx", "skills", "add", fmt.Sprintf("%s/%s", skill.Repo, skill.Name))
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("skills add command failed: %w (output: %s)", err, string(output))
	}

	log.Printf("[PiSetup] Skill installation output: %s", string(output))
	return nil
}

// isSkillsCLIAvailable checks if the skills CLI is available
func (sm *SetupManager) isSkillsCLIAvailable() bool {
	// Check for global skills command
	cmd := exec.Command("which", "skills")
	if err := cmd.Run(); err == nil {
		return true
	}

	// Check if it's available via npx
	cmd = exec.Command("npx", "--yes", "skills", "--version")
	return cmd.Run() == nil
}

// saveSkillsTracking saves the skills installation tracking information
func (sm *SetupManager) saveSkillsTracking(tracking SkillsTrackingFile) error {
	// Ensure the directory exists
	if err := os.MkdirAll(sm.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	trackingPath := sm.getSkillsTrackingPath()
	data, err := json.MarshalIndent(tracking, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tracking data: %w", err)
	}

	if err := os.WriteFile(trackingPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tracking file: %w", err)
	}

	return nil
}

// getSkillsTrackingPath returns the path to the skills tracking file
func (sm *SetupManager) getSkillsTrackingPath() string {
	return filepath.Join(sm.configDir, "skills-installed.json")
}

// GetInstalledSkills returns the list of installed skills from tracking file
func (sm *SetupManager) GetInstalledSkills() ([]string, error) {
	trackingPath := sm.getSkillsTrackingPath()

	data, err := os.ReadFile(trackingPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var tracking SkillsTrackingFile
	if err := json.Unmarshal(data, &tracking); err != nil {
		return nil, err
	}

	return tracking.Skills, nil
}

// ForceReinstallSkills forces reinstallation of all skills
func (sm *SetupManager) ForceReinstallSkills() error {
	// Remove tracking file
	trackingPath := sm.getSkillsTrackingPath()
	if err := os.Remove(trackingPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove tracking file: %w", err)
	}

	// Reinstall skills
	return sm.CheckAndInstallSkills()
}

// GetSkillInfo returns information about a skill
func GetSkillInfo(name string) (*Skill, error) {
	for _, skill := range DefaultSkills {
		if skill.Name == name {
			return &skill, nil
		}
	}
	return nil, fmt.Errorf("skill %s not found", name)
}

// ListDefaultSkills returns all default skills
func ListDefaultSkills() []Skill {
	return DefaultSkills
}

// VerifySkills checks if all skills are properly installed and have SKILL.md files (parallel version)
// Returns verification results and any error encountered
func (sm *SetupManager) VerifySkills() (*SkillsVerificationTracking, error) {
	log.Println("[PiSetup] Verifying skill installations...")

	skillsDir := sm.getSkillsDir()
	results := &SkillsVerificationTracking{
		LastUpdated: time.Now(),
		Skills:      make([]SkillVerificationResult, 0, len(DefaultSkills)),
	}

	var wg sync.WaitGroup
	resultChan := make(chan SkillVerificationResult, len(DefaultSkills))

	for _, skill := range DefaultSkills {
		wg.Add(1)
		go func(s Skill) {
			defer wg.Done()

			skillPath := filepath.Join(skillsDir, s.Name)
			skillMDPath := filepath.Join(skillPath, "SKILL.md")

			result := SkillVerificationResult{
				Name:   s.Name,
				Status: "missing",
			}

			// Check if skill directory exists
			if _, err := os.Stat(skillPath); os.IsNotExist(err) {
				log.Printf("[PiSetup] ❌ Skill %s: directory not found", s.Name)
			} else if err != nil {
				log.Printf("[PiSetup] ❌ Skill %s: error checking directory: %v", s.Name, err)
			} else {
				// Check if SKILL.md exists
				if _, err := os.Stat(skillMDPath); os.IsNotExist(err) {
					log.Printf("[PiSetup] ⚠️ Skill %s: directory exists but SKILL.md not found", s.Name)
					result.Status = "incomplete"
				} else if err != nil {
					log.Printf("[PiSetup] ❌ Skill %s: error checking SKILL.md: %v", s.Name, err)
					result.Status = "error"
				} else {
					result.Status = "verified"
					log.Printf("[PiSetup] ✅ Skill %s: verified", s.Name)
				}
			}

			resultChan <- result
		}(skill)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	verifiedCount := 0
	for result := range resultChan {
		if result.Status == "verified" {
			verifiedCount++
		}
		results.Skills = append(results.Skills, result)
	}

	results.SkillsCount = verifiedCount
	log.Printf("[PiSetup] Verification complete: %d/%d skills verified", verifiedCount, len(DefaultSkills))

	return results, nil
}

// UpdateTrackingFile writes the verification results to the tracking file
// Uses the new JSON structure with last_updated, skills_count, and skills array
func (sm *SetupManager) UpdateTrackingFile(results *SkillsVerificationTracking) error {
	if results == nil {
		return fmt.Errorf("verification results cannot be nil")
	}

	trackingPath := sm.getSkillsTrackingPath()

	// Ensure the directory exists
	if err := os.MkdirAll(sm.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tracking data: %w", err)
	}

	if err := os.WriteFile(trackingPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tracking file: %w", err)
	}

	log.Printf("[PiSetup] Updated tracking file: %s (verified: %d skills)", trackingPath, results.SkillsCount)
	return nil
}

// VerifySkillInstallation performs full verification and updates tracking
// This is the main entry point for Part 2 of skill installation verification
func (sm *SetupManager) VerifySkillInstallation() error {
	log.Println("[PiSetup] Starting skill installation verification...")

	// Verify all skills
	results, err := sm.VerifySkills()
	if err != nil {
		return fmt.Errorf("skill verification failed: %w", err)
	}

	// Update tracking file with results
	if err := sm.UpdateTrackingFile(results); err != nil {
		return fmt.Errorf("failed to update tracking file: %w", err)
	}

	// Log summary
	missingCount := 0
	incompleteCount := 0
	for _, skill := range results.Skills {
		switch skill.Status {
		case "missing":
			missingCount++
		case "incomplete":
			incompleteCount++
		}
	}

	if missingCount > 0 || incompleteCount > 0 {
		log.Printf("[PiSetup] ⚠️ Verification completed with issues: %d missing, %d incomplete", missingCount, incompleteCount)
	} else {
		log.Printf("[PiSetup] ✅ All %d skills verified successfully", results.SkillsCount)
	}

	return nil
}

// getSkillsDir returns the skills directory path
func (sm *SetupManager) getSkillsDir() string {
	return filepath.Join(sm.piConfigDir, "skills")
}

// SkillFlagsResult contains the result of building skill flags
// including the flags themselves and any warnings about missing skills
type SkillFlagsResult struct {
	Flags         []string // The generated --skill flags
	Warnings      []string // Warnings about missing skills
	Found         []string // Names of skills that were found
	Missing       []string // Names of skills that were missing
	FailOnMissing bool     // Whether to fail if skills are missing
}

// BuildSkillFlags builds --skill command-line flags for all default skills
// Returns a slice of arguments like: ["--skill", "/path/to/pi-config/skills/golang-pro", "--skill", "/path/to/pi-config/skills/golang-testing", ...]
// Skills are sorted alphabetically for reproducible ordering
// Missing skill directories are logged as warnings but don't cause errors (unless failOnMissing is true)
func BuildSkillFlags(configDir string) (*SkillFlagsResult, error) {
	return BuildSkillFlagsWithOptions(configDir, false)
}

// BuildSkillFlagsWithOptions builds skill flags with configurable options
// failOnMissing: if true, returns an error if any skill directories are missing
// Uses caching to avoid repeated filesystem operations
func BuildSkillFlagsWithOptions(configDir string, failOnMissing bool) (*SkillFlagsResult, error) {
	// Ensure configDir is absolute for consistent cache keys
	absConfigDir, err := filepath.Abs(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for config dir: %w", err)
	}

	// Create cache key based on config directory and failOnMissing flag
	cacheKey := fmt.Sprintf("%s:%v", absConfigDir, failOnMissing)

	// Check cache first
	skillFlagsCacheMu.RLock()
	if entry, ok := skillFlagsCache[cacheKey]; ok {
		// Check if cache entry is still valid
		if time.Since(entry.timestamp) < skillFlagsCacheTTL {
			skillFlagsCacheMu.RUnlock()
			skillFlagsCacheMu.Lock()
			skillFlagsCacheHits++
			skillFlagsCacheMu.Unlock()
			log.Printf("[BuildSkillFlags] Cache hit for %s (%d hits, %d misses)",
				absConfigDir, skillFlagsCacheHits, skillFlagsCacheMiss)
			return entry.result, nil
		}
	}
	skillFlagsCacheMu.RUnlock()

	// Cache miss - build skill flags
	skillFlagsCacheMu.Lock()
	skillFlagsCacheMiss++
	skillFlagsCacheMu.Unlock()

	result, err := buildSkillFlagsUncached(absConfigDir, failOnMissing)
	// Only cache successful results to avoid caching error states
	if err == nil {
		skillFlagsCacheMu.Lock()
		skillFlagsCache[cacheKey] = &skillFlagsCacheEntry{
			result:    result,
			timestamp: time.Now(),
			configDir: absConfigDir,
		}
		skillFlagsCacheMu.Unlock()
	}

	return result, err
}

// buildSkillFlagsUncached performs the actual skill flag building without caching
func buildSkillFlagsUncached(absConfigDir string, failOnMissing bool) (*SkillFlagsResult, error) {
	result := &SkillFlagsResult{
		Flags:         make([]string, 0, len(DefaultSkills)*2),
		Warnings:      make([]string, 0),
		Found:         make([]string, 0, len(DefaultSkills)),
		Missing:       make([]string, 0),
		FailOnMissing: failOnMissing,
	}

	skillsDir := filepath.Join(absConfigDir, "skills")

	// Sort skills alphabetically by name for reproducibility
	sortedSkills := make([]Skill, len(DefaultSkills))
	copy(sortedSkills, DefaultSkills)
	sort.Slice(sortedSkills, func(i, j int) bool {
		return sortedSkills[i].Name < sortedSkills[j].Name
	})

	// Process skills concurrently for faster checking
	var wg sync.WaitGroup
	type skillCheck struct {
		skill  Skill
		exists bool
		valid  bool
	}
	checkChan := make(chan skillCheck, len(sortedSkills))

	for _, skill := range sortedSkills {
		wg.Add(1)
		go func(s Skill) {
			defer wg.Done()

			skillPath := filepath.Join(skillsDir, s.Name)
			skillMDPath := filepath.Join(skillPath, "SKILL.md")

			check := skillCheck{skill: s}

			// Check if skill directory exists
			if _, err := os.Stat(skillPath); os.IsNotExist(err) {
				check.exists = false
			} else if err != nil {
				check.exists = false
			} else {
				check.exists = true
				// Check if SKILL.md exists
				if _, err := os.Stat(skillMDPath); err == nil {
					check.valid = true
				}
			}

			checkChan <- check
		}(skill)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(checkChan)
	}()

	// Collect results into a map for deterministic ordering
	type skillResult struct {
		skillPath string
		exists    bool
		valid     bool
	}
	results := make(map[string]skillResult, len(sortedSkills))

	for check := range checkChan {
		skillPath := filepath.Join(skillsDir, check.skill.Name)
		results[check.skill.Name] = skillResult{
			skillPath: skillPath,
			exists:    check.exists,
			valid:     check.valid,
		}
	}

	// Process results in alphabetical order for reproducibility
	sortedNames := make([]string, 0, len(results))
	for name := range results {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

	for _, name := range sortedNames {
		sr := results[name]
		skillPath := sr.skillPath

		if !sr.exists {
			warning := fmt.Sprintf("Skill directory not found: %s", name)
			result.Warnings = append(result.Warnings, warning)
			result.Missing = append(result.Missing, name)
			log.Printf("[BuildSkillFlags] ⚠️ %s (expected at: %s)", warning, skillPath)
			continue
		}

		// Check if SKILL.md exists (directory exists but might be incomplete)
		if !sr.valid {
			warning := fmt.Sprintf("Skill %s directory exists but SKILL.md is missing", name)
			result.Warnings = append(result.Warnings, warning)
			skillMDPath := filepath.Join(skillPath, "SKILL.md")
			log.Printf("[BuildSkillFlags] ⚠️ %s (at: %s)", warning, skillMDPath)
		}

		// Add the skill flag
		result.Flags = append(result.Flags, "--skill", skillPath)
		result.Found = append(result.Found, name)
		log.Printf("[BuildSkillFlags] ✅ Added skill flag: %s", name)
	}

	// Log summary
	if len(result.Missing) > 0 {
		log.Printf("[BuildSkillFlags] Summary: %d/%d skills available (%d missing)",
			len(result.Found), len(DefaultSkills), len(result.Missing))
		if failOnMissing {
			return result, fmt.Errorf("%d skill(s) missing and failOnMissing is enabled: %v",
				len(result.Missing), result.Missing)
		}
	} else {
		log.Printf("[BuildSkillFlags] All %d skills available", len(DefaultSkills))
	}

	return result, nil
}

// WarnMissingSkills logs warnings for missing skills
// Returns true if any skills are missing
func (r *SkillFlagsResult) WarnMissingSkills() bool {
	if len(r.Missing) == 0 {
		return false
	}

	log.Printf("[SkillFlags] ⚠️ Missing %d skill(s): %v", len(r.Missing), r.Missing)
	for _, warning := range r.Warnings {
		log.Printf("[SkillFlags] ⚠️ %s", warning)
	}

	return true
}

// GetFlags returns just the flags slice for backward compatibility
func (r *SkillFlagsResult) GetFlags() []string {
	return r.Flags
}

// HasWarnings returns true if there are any warnings
func (r *SkillFlagsResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// HasMissingSkills returns true if any skills are missing
func (r *SkillFlagsResult) HasMissingSkills() bool {
	return len(r.Missing) > 0
}
