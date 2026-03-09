package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig_ValidYAML(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	configContent := `
overseer:
  poll_interval: 10s
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

	if config.Overseer.PollInterval != 10*time.Second {
		t.Errorf("Expected PollInterval=10s, got %v", config.Overseer.PollInterval)
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
  poll_interval: 5s
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
  poll_interval: 5s
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
	if config.Overseer.PollInterval != 0 {
		// When not specified in YAML, it gets zero value
		t.Logf("PollInterval not specified in YAML, got zero value: %v", config.Overseer.PollInterval)
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
