package pi

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// HealthResult represents the result of a health check
type HealthResult struct {
	Healthy       bool     `json:"healthy"`
	Available     int      `json:"available"`
	Missing       int      `json:"missing"`
	Total         int      `json:"total"`
	MissingSkills []string `json:"missing_skills,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// HealthChecker provides health check functionality for Pi components
type HealthChecker struct {
	configDir string
}

// NewHealthChecker creates a new HealthChecker instance
func NewHealthChecker(configDir string) *HealthChecker {
	return &HealthChecker{
		configDir: configDir,
	}
}

// CheckSkillHealth verifies skill availability before spawning workers
// It checks if the skill directory exists, is not empty, and verifies
// that each skill has a SKILL.md file.
//
// Parameters:
//   - configDir: The path to the configuration directory containing the skills folder
//   - minSkills: Minimum number of skills required (default: 1 if <= 0)
//
// Returns a HealthResult containing available, missing, and total counts
func (hc *HealthChecker) CheckSkillHealth(configDir string, minSkills int) *HealthResult {
	// Set default minimum if not specified or invalid
	if minSkills <= 0 {
		minSkills = 1
	}

	// Determine the skills directory path
	skillsDir := filepath.Join(configDir, "skills")

	log.Printf("[HealthCheck] Checking skill health in: %s", skillsDir)
	log.Printf("[HealthCheck] Minimum required skills: %d", minSkills)

	// Check if skills directory exists
	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[HealthCheck] ❌ Skills directory does not exist: %s", skillsDir)
			return &HealthResult{
				Healthy:   false,
				Available: 0,
				Missing:   0,
				Total:     0,
				Error:     fmt.Sprintf("Skills directory does not exist: %s", skillsDir),
			}
		}
		log.Printf("[HealthCheck] ❌ Error accessing skills directory: %v", err)
		return &HealthResult{
			Healthy:   false,
			Available: 0,
			Missing:   0,
			Total:     0,
			Error:     fmt.Sprintf("Error accessing skills directory: %v", err),
		}
	}

	// Check if it's a directory
	if !info.IsDir() {
		log.Printf("[HealthCheck] ❌ Skills path is not a directory: %s", skillsDir)
		return &HealthResult{
			Healthy:   false,
			Available: 0,
			Missing:   0,
			Total:     0,
			Error:     fmt.Sprintf("Skills path is not a directory: %s", skillsDir),
		}
	}

	// Read directory contents
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		log.Printf("[HealthCheck] ❌ Error reading skills directory: %v", err)
		return &HealthResult{
			Healthy:   false,
			Available: 0,
			Missing:   0,
			Total:     0,
			Error:     fmt.Sprintf("Error reading skills directory: %v", err),
		}
	}

	// Check if directory is empty
	if len(entries) == 0 {
		log.Printf("[HealthCheck] ⚠️ Skills directory is empty: %s", skillsDir)
		return &HealthResult{
			Healthy:       false,
			Available:     0,
			Missing:       0,
			Total:         0,
			MissingSkills: []string{},
			Error:         "Skills directory is empty",
		}
	}

	// Verify each skill directory has a SKILL.md file
	var availableSkills []string
	var missingSkills []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillName := entry.Name()
		skillPath := filepath.Join(skillsDir, skillName)
		skillMDPath := filepath.Join(skillPath, "SKILL.md")

		// Check if SKILL.md exists
		if _, err := os.Stat(skillMDPath); err != nil {
			if os.IsNotExist(err) {
				log.Printf("[HealthCheck] ⚠️ Skill '%s' missing SKILL.md file", skillName)
				missingSkills = append(missingSkills, skillName)
			} else {
				log.Printf("[HealthCheck] ❌ Error checking SKILL.md for skill '%s': %v", skillName, err)
				missingSkills = append(missingSkills, skillName)
			}
		} else {
			log.Printf("[HealthCheck] ✅ Skill '%s' verified (SKILL.md present)", skillName)
			availableSkills = append(availableSkills, skillName)
		}
	}

	// Calculate totals
	available := len(availableSkills)
	missing := len(missingSkills)
	total := available + missing

	// Determine health status
	healthy := available >= minSkills

	// Log summary
	if healthy {
		log.Printf("[HealthCheck] ✅ Health check PASSED: %d/%d skills available (min: %d)",
			available, total, minSkills)
	} else {
		log.Printf("[HealthCheck] ❌ Health check FAILED: %d/%d skills available (min: %d)",
			available, total, minSkills)
		if missing > 0 {
			log.Printf("[HealthCheck] Missing skills: %v", missingSkills)
		}
	}

	result := &HealthResult{
		Healthy:       healthy,
		Available:     available,
		Missing:       missing,
		Total:         total,
		MissingSkills: missingSkills,
	}

	if !healthy {
		result.Error = fmt.Sprintf("Only %d of %d required skills available (minimum: %d)",
			available, total, minSkills)
	}

	return result
}

// CheckSkillHealthWithDefaults performs a health check with default minimum threshold
func (hc *HealthChecker) CheckSkillHealthWithDefaults(configDir string) *HealthResult {
	return hc.CheckSkillHealth(configDir, 1)
}

// IsHealthy returns true if the health check result indicates healthy status
func (hr *HealthResult) IsHealthy() bool {
	return hr.Healthy
}

// String returns a human-readable representation of the health result
func (hr *HealthResult) String() string {
	status := "HEALTHY"
	if !hr.Healthy {
		status = "UNHEALTHY"
	}

	if hr.Error != "" {
		return fmt.Sprintf("[%s] Available: %d, Missing: %d, Total: %d, Error: %s",
			status, hr.Available, hr.Missing, hr.Total, hr.Error)
	}

	return fmt.Sprintf("[%s] Available: %d, Missing: %d, Total: %d",
		status, hr.Available, hr.Missing, hr.Total)
}
