// Package pi provides automatic setup and configuration for Pi agent ecosystem
package pi

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// SetupManager orchestrates the installation of Pi agent, LSP tools, and skills
type SetupManager struct {
	configDir     string
	piConfigDir   string
	trackingFile  string
	configManager *ConfigManager

	// Caching layer for installation status
	cacheMu   sync.RWMutex
	cache     *SetupCache
	cacheOnce sync.Once
}

// SetupCache stores cached installation status to avoid redundant checks
type SetupCache struct {
	PiInstalled     bool      `json:"pi_installed"`
	LSPInstalled    bool      `json:"lsp_installed"`
	SkillsInstalled bool      `json:"skills_installed"`
	CacheTime       time.Time `json:"cache_time"`
	CacheValid      bool      `json:"cache_valid"`
}

// SetupResult contains the results of setup operations
type SetupResult struct {
	PiInstalled     bool          `json:"pi_installed"`
	LSPInstalled    bool          `json:"lsp_installed"`
	SkillsInstalled bool          `json:"skills_installed"`
	Errors          []string      `json:"errors,omitempty"`
	Timestamp       time.Time     `json:"timestamp"`
	Duration        time.Duration `json:"duration_ms"`
}

// NewSetupManager creates a new setup manager with caching support
func NewSetupManager(configDir string, piConfigDir string) *SetupManager {
	if configDir == "" {
		homeDir, _ := os.UserHomeDir()
		configDir = filepath.Join(homeDir, ".config", "pi")
	}

	if piConfigDir == "" {
		piConfigDir = ".pi/hydra-config"
	}

	sm := &SetupManager{
		configDir:     configDir,
		piConfigDir:   piConfigDir,
		trackingFile:  filepath.Join(configDir, "skills-installed.json"),
		configManager: NewConfigManager(piConfigDir),
	}

	// Initialize cache
	sm.initCache()

	return sm
}

// initCache loads the cache from disk or creates a new one
func (sm *SetupManager) initCache() {
	sm.cacheOnce.Do(func() {
		sm.cache = &SetupCache{}
		sm.loadCacheFromDisk()
	})
}

// loadCacheFromDisk loads cached status from tracking file
func (sm *SetupManager) loadCacheFromDisk() {
	data, err := os.ReadFile(sm.trackingFile)
	if err != nil {
		// No cache file exists, will be created on save
		sm.cache.CacheValid = false
		return
	}

	var result SetupResult
	if err := json.Unmarshal(data, &result); err != nil {
		// Invalid cache file
		sm.cache.CacheValid = false
		return
	}

	sm.cache.PiInstalled = result.PiInstalled
	sm.cache.LSPInstalled = result.LSPInstalled
	sm.cache.SkillsInstalled = result.SkillsInstalled
	sm.cache.CacheTime = result.Timestamp

	// Cache is valid if it's less than 1 hour old
	if time.Since(result.Timestamp) < time.Hour {
		sm.cache.CacheValid = true
	}
}

// isCacheValid checks if cached status can be used
func (sm *SetupManager) isCacheValid() bool {
	sm.cacheMu.RLock()
	defer sm.cacheMu.RUnlock()

	if sm.cache == nil || !sm.cache.CacheValid {
		return false
	}

	// Cache expires after 1 hour
	return time.Since(sm.cache.CacheTime) < time.Hour
}

// updateCache updates the in-memory cache
func (sm *SetupManager) updateCache(piInstalled, lspInstalled, skillsInstalled bool) {
	sm.cacheMu.Lock()
	defer sm.cacheMu.Unlock()

	sm.cache.PiInstalled = piInstalled
	sm.cache.LSPInstalled = lspInstalled
	sm.cache.SkillsInstalled = skillsInstalled
	sm.cache.CacheTime = time.Now()
	sm.cache.CacheValid = true
}

// CreateDirectoryStructure creates the pi-config directory structure
// This includes:
// - pi-config/agent/ directory (0755 permissions)
// - pi-config/skills/ directory (0755 permissions)
// - pi-config/agent/mcp.json with LSP server configuration
// - pi-config/skills-installed.json tracking file
func (sm *SetupManager) CreateDirectoryStructure() error {
	if sm.configManager == nil {
		return fmt.Errorf("config manager not initialized")
	}

	return sm.configManager.CreateDirectoryStructure()
}

// RunSetup performs all setup operations with parallelization
func (sm *SetupManager) RunSetup() (*SetupResult, error) {
	startTime := time.Now()
	result := &SetupResult{
		Timestamp: time.Now(),
		Errors:    []string{},
	}

	log.Println("[PiSetup] Starting Pi ecosystem setup...")

	// Check if we can use cached results
	if sm.isCacheValid() && sm.cache.SkillsInstalled {
		log.Println("[PiSetup] Using cached installation status (last updated:", sm.cache.CacheTime.Format(time.RFC3339), ")")
		result.PiInstalled = sm.cache.PiInstalled
		result.LSPInstalled = sm.cache.LSPInstalled
		result.SkillsInstalled = sm.cache.SkillsInstalled
		result.Duration = time.Since(startTime)
		log.Printf("[PiSetup] ✅ Setup completed from cache in %v", result.Duration)
		return result, nil
	}

	// Ensure config directory exists
	if err := os.MkdirAll(sm.configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create pi-config directory structure
	log.Println("[PiSetup] Creating pi-config directory structure...")
	if err := sm.CreateDirectoryStructure(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Directory structure: %v", err))
		log.Printf("[PiSetup] ❌ Directory structure creation failed: %v", err)
	} else {
		log.Println("[PiSetup] ✅ Directory structure created successfully")
	}

	// Check and install Pi agent (sequential - must complete before others)
	log.Println("[PiSetup] Checking Pi agent installation...")
	if err := sm.CheckAndInstallPi(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Pi installation: %v", err))
		log.Printf("[PiSetup] ❌ Pi agent installation failed: %v", err)
	} else {
		result.PiInstalled = true
		log.Println("[PiSetup] ✅ Pi agent installed successfully")
	}

	// Parallelize LSP and skills checks (they're independent)
	var wg sync.WaitGroup
	var lspErr, skillsErr error
	var lspResult, skillsResult bool

	// Check and install LSP servers (concurrent)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[PiSetup] Checking LSP servers...")
		if err := sm.CheckAndInstallLSP(); err != nil {
			lspErr = err
			log.Printf("[PiSetup] ❌ LSP installation failed: %v", err)
		} else {
			lspResult = true
			log.Println("[PiSetup] ✅ LSP servers installed successfully")
		}
	}()

	// Check and install skills (concurrent, uses cached status)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[PiSetup] Checking skills installation...")
		if sm.AreSkillsInstalled() {
			log.Println("[PiSetup] ℹ️ Skills already installed, skipping")
			skillsResult = true
		} else {
			if err := sm.CheckAndInstallSkills(); err != nil {
				skillsErr = err
				log.Printf("[PiSetup] ❌ Skills installation failed: %v", err)
			} else {
				skillsResult = true
				log.Println("[PiSetup] ✅ Skills installed successfully")
			}
		}
	}()

	// Wait for both goroutines to complete
	wg.Wait()

	// Collect results
	if lspErr != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("LSP installation: %v", lspErr))
	}
	result.LSPInstalled = lspResult

	if skillsErr != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Skills installation: %v", skillsErr))
	}
	result.SkillsInstalled = skillsResult

	// Update cache with new results
	sm.updateCache(result.PiInstalled, result.LSPInstalled, result.SkillsInstalled)

	// Save results
	sm.saveResult(result)

	result.Duration = time.Since(startTime)

	if len(result.Errors) > 0 {
		log.Printf("[PiSetup] Setup completed with %d errors in %v", len(result.Errors), result.Duration)
	} else {
		log.Printf("[PiSetup] ✅ Setup completed successfully in %v!", result.Duration)
	}

	return result, nil
}

// RunSetupOptimized is a highly optimized version that uses aggressive caching
func (sm *SetupManager) RunSetupOptimized() (*SetupResult, error) {
	startTime := time.Now()
	result := &SetupResult{
		Timestamp: time.Now(),
		Errors:    []string{},
	}

	log.Println("[PiSetup] Starting optimized Pi ecosystem setup...")

	// Fast path: check if everything is already installed
	if sm.isCacheValid() && sm.cache.PiInstalled && sm.cache.LSPInstalled && sm.cache.SkillsInstalled {
		result.PiInstalled = sm.cache.PiInstalled
		result.LSPInstalled = sm.cache.LSPInstalled
		result.SkillsInstalled = sm.cache.SkillsInstalled
		result.Duration = time.Since(startTime)
		log.Printf("[PiSetup] ✅ Setup completed from cache in %v (all components already installed)", result.Duration)
		return result, nil
	}

	// Ensure config directory exists
	if err := os.MkdirAll(sm.configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create directory structure
	if err := sm.CreateDirectoryStructure(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Directory structure: %v", err))
	}

	// Run all checks in parallel with error channels
	type checkResult struct {
		name    string
		success bool
		err     error
	}

	results := make(chan checkResult, 3)
	var setupWg sync.WaitGroup

	// Pi check
	setupWg.Add(1)
	go func() {
		defer setupWg.Done()
		if err := sm.CheckAndInstallPi(); err != nil {
			results <- checkResult{"Pi", false, err}
		} else {
			results <- checkResult{"Pi", true, nil}
		}
	}()

	// LSP check
	setupWg.Add(1)
	go func() {
		defer setupWg.Done()
		if err := sm.CheckAndInstallLSP(); err != nil {
			results <- checkResult{"LSP", false, err}
		} else {
			results <- checkResult{"LSP", true, nil}
		}
	}()

	// Skills check
	setupWg.Add(1)
	go func() {
		defer setupWg.Done()
		if sm.AreSkillsInstalled() {
			results <- checkResult{"Skills", true, nil}
		} else {
			if err := sm.CheckAndInstallSkills(); err != nil {
				results <- checkResult{"Skills", false, err}
			} else {
				results <- checkResult{"Skills", true, nil}
			}
		}
	}()

	// Close results channel when all goroutines complete
	go func() {
		setupWg.Wait()
		close(results)
	}()

	// Collect results
	for r := range results {
		switch r.name {
		case "Pi":
			result.PiInstalled = r.success
			if r.err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Pi installation: %v", r.err))
			}
		case "LSP":
			result.LSPInstalled = r.success
			if r.err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("LSP installation: %v", r.err))
			}
		case "Skills":
			result.SkillsInstalled = r.success
			if r.err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Skills installation: %v", r.err))
			}
		}
	}

	// Update cache
	sm.updateCache(result.PiInstalled, result.LSPInstalled, result.SkillsInstalled)

	// Save results
	sm.saveResult(result)

	result.Duration = time.Since(startTime)

	if len(result.Errors) > 0 {
		log.Printf("[PiSetup] Setup completed with %d errors in %v", len(result.Errors), result.Duration)
	} else {
		log.Printf("[PiSetup] ✅ Setup completed successfully in %v!", result.Duration)
	}

	return result, nil
}

// CheckAndInstallPi checks if Pi is installed and installs it if missing
func (sm *SetupManager) CheckAndInstallPi() error {
	if sm.IsPiInstalled() {
		log.Println("[PiSetup] Pi agent already installed")
		return nil
	}

	log.Println("[PiSetup] Installing Pi agent via npm...")

	// Try global install first
	cmd := exec.Command("npm", "install", "-g", "@pi/agent")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try with npx if npm install fails
		log.Println("[PiSetup] Global install failed, trying npx...")
		cmd = exec.Command("npx", "@pi/agent", "--version")
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install Pi agent: %w (output: %s)", err, string(output))
		}
	}

	log.Printf("[PiSetup] Pi agent installation output: %s", string(output))
	return nil
}

// IsPiInstalled checks if Pi agent is installed (with caching)
func (sm *SetupManager) IsPiInstalled() bool {
	// Check cache first
	sm.cacheMu.RLock()
	if sm.cache != nil && sm.cache.PiInstalled {
		sm.cacheMu.RUnlock()
		return true
	}
	sm.cacheMu.RUnlock()

	// Check for pi command
	cmd := exec.Command("which", "pi")
	if err := cmd.Run(); err == nil {
		sm.cacheMu.Lock()
		sm.cache.PiInstalled = true
		sm.cacheMu.Unlock()
		return true
	}

	// Check for npx pi
	cmd = exec.Command("npx", "--yes", "@pi/agent", "--version")
	if err := cmd.Run(); err == nil {
		sm.cacheMu.Lock()
		sm.cache.PiInstalled = true
		sm.cacheMu.Unlock()
		return true
	}

	return false
}

// saveResult saves the setup result to the tracking file
func (sm *SetupManager) saveResult(result *SetupResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sm.trackingFile, data, 0644)
}

// AreSkillsInstalled checks if skills have been installed (uses caching)
func (sm *SetupManager) AreSkillsInstalled() bool {
	// Check cache first
	sm.cacheMu.RLock()
	if sm.cache != nil && sm.cache.SkillsInstalled {
		sm.cacheMu.RUnlock()
		return true
	}
	sm.cacheMu.RUnlock()

	// Check tracking file
	_, err := os.Stat(sm.trackingFile)
	installed := err == nil

	if installed {
		sm.cacheMu.Lock()
		sm.cache.SkillsInstalled = true
		sm.cacheMu.Unlock()
	}

	return installed
}

// GetLastSetupResult retrieves the last setup result
func (sm *SetupManager) GetLastSetupResult() (*SetupResult, error) {
	data, err := os.ReadFile(sm.trackingFile)
	if err != nil {
		return nil, err
	}

	var result SetupResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ClearCache invalidates the in-memory cache
func (sm *SetupManager) ClearCache() {
	sm.cacheMu.Lock()
	defer sm.cacheMu.Unlock()

	if sm.cache != nil {
		sm.cache.CacheValid = false
		sm.cache.PiInstalled = false
		sm.cache.LSPInstalled = false
		sm.cache.SkillsInstalled = false
	}
}

// GetCacheStatus returns the current cache status for debugging
func (sm *SetupManager) GetCacheStatus() map[string]interface{} {
	sm.cacheMu.RLock()
	defer sm.cacheMu.RUnlock()

	if sm.cache == nil {
		return map[string]interface{}{
			"initialized": false,
			"valid":       false,
		}
	}

	return map[string]interface{}{
		"initialized":      true,
		"valid":            sm.cache.CacheValid,
		"cache_time":       sm.cache.CacheTime,
		"pi_installed":     sm.cache.PiInstalled,
		"lsp_installed":    sm.cache.LSPInstalled,
		"skills_installed": sm.cache.SkillsInstalled,
		"age_seconds":      time.Since(sm.cache.CacheTime).Seconds(),
	}
}
