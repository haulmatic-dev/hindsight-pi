// Package pi provides automatic setup and configuration for Pi agent ecosystem
package pi

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// SetupManager orchestrates the installation of Pi agent, LSP tools, and skills
type SetupManager struct {
	configDir       string
	piConfigDir     string
	skillsInstalled bool
	trackingFile    string
	configManager   *ConfigManager
}

// SetupResult contains the results of setup operations
type SetupResult struct {
	PiInstalled     bool      `json:"pi_installed"`
	LSPInstalled    bool      `json:"lsp_installed"`
	SkillsInstalled bool      `json:"skills_installed"`
	Errors          []string  `json:"errors,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// NewSetupManager creates a new setup manager
func NewSetupManager(configDir string, piConfigDir string) *SetupManager {
	if configDir == "" {
		homeDir, _ := os.UserHomeDir()
		configDir = filepath.Join(homeDir, ".config", "pi")
	}

	if piConfigDir == "" {
		piConfigDir = "pi-config"
	}

	return &SetupManager{
		configDir:     configDir,
		piConfigDir:   piConfigDir,
		trackingFile:  filepath.Join(configDir, "skills-installed.json"),
		configManager: NewConfigManager(piConfigDir),
	}
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

// RunSetup performs all setup operations
func (sm *SetupManager) RunSetup() (*SetupResult, error) {
	result := &SetupResult{
		Timestamp: time.Now(),
		Errors:    []string{},
	}

	log.Println("[PiSetup] Starting Pi ecosystem setup...")

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

	// Check and install Pi agent
	log.Println("[PiSetup] Checking Pi agent installation...")
	if err := sm.CheckAndInstallPi(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Pi installation: %v", err))
		log.Printf("[PiSetup] ❌ Pi agent installation failed: %v", err)
	} else {
		result.PiInstalled = true
		log.Println("[PiSetup] ✅ Pi agent installed successfully")
	}

	// Check and install LSP servers
	log.Println("[PiSetup] Checking LSP servers...")
	if err := sm.CheckAndInstallLSP(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("LSP installation: %v", err))
		log.Printf("[PiSetup] ❌ LSP installation failed: %v", err)
	} else {
		result.LSPInstalled = true
		log.Println("[PiSetup] ✅ LSP servers installed successfully")
	}

	// Check and install skills (only on first run)
	log.Println("[PiSetup] Checking skills installation...")
	if sm.AreSkillsInstalled() {
		log.Println("[PiSetup] ℹ️ Skills already installed, skipping")
		result.SkillsInstalled = true
	} else {
		if err := sm.CheckAndInstallSkills(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Skills installation: %v", err))
			log.Printf("[PiSetup] ❌ Skills installation failed: %v", err)
		} else {
			result.SkillsInstalled = true
			log.Println("[PiSetup] ✅ Skills installed successfully")
		}
	}

	// Save results
	sm.saveResult(result)

	if len(result.Errors) > 0 {
		log.Printf("[PiSetup] Setup completed with %d errors", len(result.Errors))
	} else {
		log.Println("[PiSetup] ✅ Setup completed successfully!")
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

// IsPiInstalled checks if Pi agent is installed
func (sm *SetupManager) IsPiInstalled() bool {
	// Check for pi command
	cmd := exec.Command("which", "pi")
	if err := cmd.Run(); err == nil {
		return true
	}

	// Check for npx pi
	cmd = exec.Command("npx", "--yes", "@pi/agent", "--version")
	if err := cmd.Run(); err == nil {
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

// AreSkillsInstalled checks if skills have been installed
func (sm *SetupManager) AreSkillsInstalled() bool {
	_, err := os.Stat(sm.trackingFile)
	return err == nil
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
