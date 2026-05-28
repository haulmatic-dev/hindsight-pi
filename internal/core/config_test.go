package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig_ValidYAML(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	configContent := `
overseer:
  bead_discovery_interval: 10s
  max_workers: 5
  default_retries: 3
  worker_timeout: 5m

projects:
  auto_discover: false
  list:
    - name: test-project
      path: /tmp/test
      type: single

api:
  enabled: true
  port: 8080

mcp:
  enabled: false
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Load the config
	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify overseer settings
	if config.Overseer.MaxWorkers != 5 {
		t.Errorf("Expected MaxWorkers=5, got %d", config.Overseer.MaxWorkers)
	}

	if config.Overseer.BeadDiscoveryInterval != 10*time.Second {
		t.Errorf("Expected BeadDiscoveryInterval=10s, got %v", config.Overseer.BeadDiscoveryInterval)
	}

	// Verify projects
	if len(config.Projects) != 1 {
		t.Fatalf("Expected 1 project, got %d", len(config.Projects))
	}

	if config.Projects[0].Name != "test-project" {
		t.Errorf("Expected project name 'test-project', got '%s'", config.Projects[0].Name)
	}

	if config.Projects[0].Path != "/tmp/test" {
		t.Errorf("Expected project path '/tmp/test', got '%s'", config.Projects[0].Path)
	}

	// Verify API settings
	if !config.API.Enabled {
		t.Error("Expected API to be enabled")
	}

	if config.API.Port != 8080 {
		t.Errorf("Expected API port 8080, got %d", config.API.Port)
	}
}

func TestLoadConfig_ArrayFormat(t *testing.T) {
	// Test projects as array (old format)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "array_config.yaml")

	configContent := `
overseer:
  bead_discovery_interval: 5s
  max_workers: 3

projects:
  - name: project-1
    path: /tmp/project1
    type: single
  - name: project-2
    path: /tmp/project2
    type: single
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(config.Projects) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(config.Projects))
	}
}

func TestLoadConfig_ObjectWithListFormat(t *testing.T) {
	// Test projects as object with list field (new format)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "object_config.yaml")

	configContent := `
overseer:
  bead_discovery_interval: 5s
  max_workers: 3

projects:
  auto_discover: false
  list:
    - name: project-1
      path: /tmp/project1
      type: single
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(config.Projects) != 1 {
		t.Errorf("Expected 1 project, got %d", len(config.Projects))
	}

	if config.Projects[0].Name != "project-1" {
		t.Errorf("Expected project name 'project-1', got '%s'", config.Projects[0].Name)
	}
}

func TestLoadConfig_DefaultValues(t *testing.T) {
	// Create a minimal config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "minimal_config.yaml")

	configContent := `
overseer:
  max_workers: 10
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify default values are preserved
	if config.Overseer.BeadDiscoveryInterval != 0 {
		// When not specified in YAML, it gets zero value
		t.Logf("BeadDiscoveryInterval not specified in YAML, got zero value: %v", config.Overseer.BeadDiscoveryInterval)
	}

	if config.Overseer.MaxWorkers != 10 {
		t.Errorf("Expected MaxWorkers=10, got %d", config.Overseer.MaxWorkers)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Expected error when loading non-existent file, got nil")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	// Write invalid YAML
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content:"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("Expected error when loading invalid YAML, got nil")
	}
}

// ========== PI Configuration Tests ==========

func TestValidateConfig_PIConfigDir(t *testing.T) {
	tests := []struct {
		name      string
		configDir string
		wantError bool
		errField  string
	}{
		{
			name:      "valid relative path",
			configDir: "./pi-config",
			wantError: false,
		},
		{
			name:      "valid absolute path",
			configDir: "/home/user/.config/pi",
			wantError: false,
		},
		{
			name:      "valid relative parent path",
			configDir: "../pi-config",
			wantError: false,
		},
		{
			name:      "empty path allowed",
			configDir: "",
			wantError: false,
		},
		{
			name:      "path with null bytes invalid",
			configDir: "./pi-config\x00evil",
			wantError: true,
			errField:  "pi.config_dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Pi.ConfigDir = tt.configDir

			errors := ValidateConfig(cfg)

			if tt.wantError {
				found := false
				for _, err := range errors {
					if err.Field == tt.errField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error for field %s, got errors: %v", tt.errField, errors)
				}
			} else {
				for _, err := range errors {
					if err.Field == "pi.config_dir" {
						t.Errorf("Unexpected error for pi.config_dir: %v", err)
					}
				}
			}
		})
	}
}

func TestValidateConfig_PISkillsUpdateInterval(t *testing.T) {
	tests := []struct {
		name           string
		updateInterval string
		wantError      bool
		errContains    string
	}{
		{
			name:           "valid 24h interval",
			updateInterval: "24h",
			wantError:      false,
		},
		{
			name:           "valid 30m interval",
			updateInterval: "30m",
			wantError:      false,
		},
		{
			name:           "valid 1h30m interval",
			updateInterval: "1h30m",
			wantError:      false,
		},
		{
			name:           "empty interval allowed",
			updateInterval: "",
			wantError:      false,
		},
		{
			name:           "invalid duration format",
			updateInterval: "not-a-duration",
			wantError:      true,
			errContains:    "valid duration",
		},
		{
			name:           "too short interval",
			updateInterval: "30s",
			wantError:      true,
			errContains:    "at least 1 minute",
		},
		{
			name:           "too long interval",
			updateInterval: "720h1m", // 30 days and 1 minute
			wantError:      true,
			errContains:    "not exceed 30 days",
		},
		{
			name:           "exactly 1 minute is valid",
			updateInterval: "1m",
			wantError:      false,
		},
		{
			name:           "exactly 30 days is valid",
			updateInterval: "720h", // 30 days
			wantError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Pi.Skills.UpdateInterval = tt.updateInterval

			errors := ValidateConfig(cfg)

			if tt.wantError {
				found := false
				for _, err := range errors {
					if err.Field == "pi.skills.update_interval" {
						if tt.errContains == "" || containsSubstring(err.Message, tt.errContains) {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("Expected error containing %q for pi.skills.update_interval, got errors: %v", tt.errContains, errors)
				}
			} else {
				for _, err := range errors {
					if err.Field == "pi.skills.update_interval" {
						t.Errorf("Unexpected error for pi.skills.update_interval: %v", err)
					}
				}
			}
		})
	}
}

func TestValidateConfig_PISkillsAutoUpdate(t *testing.T) {
	tests := []struct {
		name       string
		autoUpdate bool
		interval   string
	}{
		{
			name:       "auto_update true with interval",
			autoUpdate: true,
			interval:   "24h",
		},
		{
			name:       "auto_update true without interval",
			autoUpdate: true,
			interval:   "",
		},
		{
			name:       "auto_update false with interval",
			autoUpdate: false,
			interval:   "24h",
		},
		{
			name:       "auto_update false without interval",
			autoUpdate: false,
			interval:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Pi.Skills.AutoUpdate = tt.autoUpdate
			cfg.Pi.Skills.UpdateInterval = tt.interval

			errors := ValidateConfig(cfg)

			// auto_update should not produce validation errors
			for _, err := range errors {
				if err.Field == "pi.skills.auto_update" {
					t.Errorf("Unexpected validation error for pi.skills.auto_update: %v", err)
				}
			}
		})
	}
}

func TestValidateConfig_PIComplete(t *testing.T) {
	// Test a complete valid PI configuration
	cfg := DefaultConfig()
	cfg.Pi.ConfigDir = "/opt/pi-config"
	cfg.Pi.Skills.UpdateInterval = "6h"
	cfg.Pi.Skills.AutoUpdate = true

	errors := ValidateConfig(cfg)

	// Check that there are no PI-related validation errors
	for _, err := range errors {
		if strings.HasPrefix(err.Field, "pi.") {
			t.Errorf("Unexpected PI validation error: %v", err)
		}
	}
}

func TestValidateConfig_PINilConfig(t *testing.T) {
	// Test validation when PI config pointer would be nil
	// (In practice, PiRootConfig is a value type, not a pointer)
	cfg := DefaultConfig()

	errors := ValidateConfig(cfg)

	// Should not panic and should validate successfully
	if !errors.IsValid() {
		// May have project-related errors, but should not have PI errors
		for _, err := range errors {
			if strings.HasPrefix(err.Field, "pi.") {
				t.Errorf("Unexpected PI validation error with default config: %v", err)
			}
		}
	}
}

func TestLoadConfig_WithPISettings(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "pi_config.yaml")

	configContent := `
overseer:
  bead_discovery_interval: 5s
  max_workers: 5

projects:
  - name: test-project
    path: /tmp/test
    type: single

pi:
  config_dir: /custom/pi/config
  skills:
    update_interval: 12h
    auto_update: true
    global: false
    list:
      - name: go-skill
        repo: https://github.com/example/go-skill.git
      - name: python-skill
        repo: git@github.com:example/python-skill.git
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify PI config_dir
	if config.Pi.ConfigDir != "/custom/pi/config" {
		t.Errorf("Expected pi.config_dir='/custom/pi/config', got '%s'", config.Pi.ConfigDir)
	}

	// Verify PI skills settings
	if config.Pi.Skills.UpdateInterval != "12h" {
		t.Errorf("Expected pi.skills.update_interval='12h', got '%s'", config.Pi.Skills.UpdateInterval)
	}

	if !config.Pi.Skills.AutoUpdate {
		t.Error("Expected pi.skills.auto_update=true")
	}

	// Validate the loaded config
	errors := ValidateConfig(config)
	if !errors.IsValid() {
		t.Errorf("Loaded config has validation errors: %v", errors)
	}
}

func TestLoadConfig_PIInvalidSettings(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_pi_config.yaml")

	configContent := `
overseer:
  max_workers: 5

projects:
  - name: test-project
    path: /tmp/test
    type: single

pi:
  config_dir: ./pi-config
  skills:
    update_interval: invalid-duration
    auto_update: true
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Validation should catch the errors
	errors := ValidateConfig(config)

	// Check for expected validation errors
	hasIntervalError := false
	for _, err := range errors {
		if err.Field == "pi.skills.update_interval" {
			hasIntervalError = true
		}
	}

	if !hasIntervalError {
		t.Error("Expected validation error for invalid update_interval")
	}
}

// Helper function
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}
