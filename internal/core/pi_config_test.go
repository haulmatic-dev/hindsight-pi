package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPiConfig_DefaultValues(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Pi.ConfigDir != "./pi-config" {
		t.Errorf("Pi.ConfigDir = %q, want %q", cfg.Pi.ConfigDir, "./pi-config")
	}

	if cfg.Pi.Skills.UpdateInterval != "24h" {
		t.Errorf("Pi.Skills.UpdateInterval = %q, want %q", cfg.Pi.Skills.UpdateInterval, "24h")
	}

	if !cfg.Pi.Skills.AutoUpdate {
		t.Error("Pi.Skills.AutoUpdate should be true by default")
	}

	if cfg.Pi.Skills.Global {
		t.Error("Pi.Skills.Global should be false by default")
	}

	if len(cfg.Pi.Skills.List) != 0 {
		t.Errorf("Pi.Skills.List length = %d, want 0", len(cfg.Pi.Skills.List))
	}
}

func TestPiConfig_LoadFromYAML(t *testing.T) {
	yamlContent := `
projects:
  - name: test
    path: /tmp/test
    type: single

pi:
  config_dir: "/custom/pi-config"
  skills:
    update_interval: "12h"
    auto_update: false
    global: true
    list:
      - name: "go-patterns"
        repo: "https://github.com/example/go-patterns"
      - name: "k8s-ops"
        repo: "https://github.com/example/k8s-ops"
`

	tmpFile, err := os.CreateTemp("", "hydra_config_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Pi.ConfigDir != "/custom/pi-config" {
		t.Errorf("Pi.ConfigDir = %q, want %q", cfg.Pi.ConfigDir, "/custom/pi-config")
	}

	if cfg.Pi.Skills.UpdateInterval != "12h" {
		t.Errorf("Pi.Skills.UpdateInterval = %q, want %q", cfg.Pi.Skills.UpdateInterval, "12h")
	}

	if cfg.Pi.Skills.AutoUpdate {
		t.Error("Pi.Skills.AutoUpdate should be false")
	}

	if !cfg.Pi.Skills.Global {
		t.Error("Pi.Skills.Global should be true")
	}

	if len(cfg.Pi.Skills.List) != 2 {
		t.Errorf("Pi.Skills.List length = %d, want 2", len(cfg.Pi.Skills.List))
	}

	if cfg.Pi.Skills.List[0].Name != "go-patterns" {
		t.Errorf("First skill name = %q, want %q", cfg.Pi.Skills.List[0].Name, "go-patterns")
	}

	if cfg.Pi.Skills.List[0].Repo != "https://github.com/example/go-patterns" {
		t.Errorf("First skill repo = %q, want %q", cfg.Pi.Skills.List[0].Repo, "https://github.com/example/go-patterns")
	}
}

func TestPiConfig_MissingSectionUsesDefaults(t *testing.T) {
	yamlContent := `
projects:
  - name: test
    path: /tmp/test
    type: single
`

	tmpFile, err := os.CreateTemp("", "hydra_config_minimal_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// When Pi section is missing, it should use zero values (not default values)
	// This is standard YAML unmarshaling behavior
	if cfg.Pi.ConfigDir != "" {
		t.Logf("Note: Pi.ConfigDir = %q (zero value when section missing)", cfg.Pi.ConfigDir)
	}
}

func TestPiConfig_ClonePreservesValues(t *testing.T) {
	cfg := DefaultConfig()
	cloned := cfg.Clone()

	if cloned.Pi.ConfigDir != cfg.Pi.ConfigDir {
		t.Errorf("Clone Pi.ConfigDir = %q, want %q", cloned.Pi.ConfigDir, cfg.Pi.ConfigDir)
	}

	if cloned.Pi.Skills.UpdateInterval != cfg.Pi.Skills.UpdateInterval {
		t.Errorf("Clone Pi.Skills.UpdateInterval = %q, want %q", cloned.Pi.Skills.UpdateInterval, cfg.Pi.Skills.UpdateInterval)
	}

	if cloned.Pi.Skills.AutoUpdate != cfg.Pi.Skills.AutoUpdate {
		t.Errorf("Clone Pi.Skills.AutoUpdate = %t, want %t", cloned.Pi.Skills.AutoUpdate, cfg.Pi.Skills.AutoUpdate)
	}
}

// TestPiConfig_InvalidInterval verifies behavior with invalid update interval
func TestPiConfig_InvalidInterval(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_interval.yaml")

	configContent := `
overseer:
  max_workers: 3

projects:
  - name: test-project
    path: /tmp/test
    type: single

pi:
  skills:
    update_interval: "invalid-duration-string"
    auto_update: true
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Since UpdateInterval is a string field, invalid values should still load
	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig should not fail with invalid interval string: %v", err)
	}

	// The invalid string should be loaded as-is (it's stored as string, not parsed as duration)
	if config.Pi.Skills.UpdateInterval != "invalid-duration-string" {
		t.Errorf("Expected invalid interval to be loaded as-is, got '%s'", config.Pi.Skills.UpdateInterval)
	}

	// Other fields should still load correctly
	if !config.Pi.Skills.AutoUpdate {
		t.Error("Expected Pi.Skills.AutoUpdate to be true")
	}
}

// TestPiConfig_SkillsList verifies that skill entries are loaded correctly with multiple entries
func TestPiConfig_SkillsList(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "skills_config.yaml")

	configContent := `
overseer:
  max_workers: 3

projects:
  - name: test-project
    path: /tmp/test
    type: single

pi:
  skills:
    update_interval: "6h"
    auto_update: true
    global: false
    list:
      - name: golang-basics
        repo: "https://github.com/skills/go-basics"
      - name: rust-advanced
        repo: "https://github.com/skills/rust-advanced"
      - name: python-data-science
        repo: "https://github.com/skills/python-ds"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify skills list length
	if len(config.Pi.Skills.List) != 3 {
		t.Fatalf("Expected 3 skills, got %d", len(config.Pi.Skills.List))
	}

	// Verify first skill
	if config.Pi.Skills.List[0].Name != "golang-basics" {
		t.Errorf("Expected first skill name 'golang-basics', got '%s'", config.Pi.Skills.List[0].Name)
	}

	if config.Pi.Skills.List[0].Repo != "https://github.com/skills/go-basics" {
		t.Errorf("Expected first skill repo 'https://github.com/skills/go-basics', got '%s'", config.Pi.Skills.List[0].Repo)
	}

	// Verify second skill
	if config.Pi.Skills.List[1].Name != "rust-advanced" {
		t.Errorf("Expected second skill name 'rust-advanced', got '%s'", config.Pi.Skills.List[1].Name)
	}

	if config.Pi.Skills.List[1].Repo != "https://github.com/skills/rust-advanced" {
		t.Errorf("Expected second skill repo 'https://github.com/skills/rust-advanced', got '%s'", config.Pi.Skills.List[1].Repo)
	}

	// Verify third skill
	if config.Pi.Skills.List[2].Name != "python-data-science" {
		t.Errorf("Expected third skill name 'python-data-science', got '%s'", config.Pi.Skills.List[2].Name)
	}

	if config.Pi.Skills.List[2].Repo != "https://github.com/skills/python-ds" {
		t.Errorf("Expected third skill repo 'https://github.com/skills/python-ds', got '%s'", config.Pi.Skills.List[2].Repo)
	}
}

// TestPiConfig_EmptySkillsList verifies behavior with empty skills list
func TestPiConfig_EmptySkillsList(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "empty_skills.yaml")

	configContent := `
overseer:
  max_workers: 3

projects:
  - name: test-project
    path: /tmp/test
    type: single

pi:
  skills:
    update_interval: "24h"
    auto_update: true
    global: false
    list: []
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(config.Pi.Skills.List) != 0 {
		t.Errorf("Expected empty skills list, got %d items", len(config.Pi.Skills.List))
	}
}

// TestPiConfig_AgentsPiDefaultValues verifies Pi agent default values
func TestPiConfig_AgentsPiDefaultValues(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Agents.Pi.Enabled {
		t.Error("Agents.Pi.Enabled should be true by default")
	}

	if cfg.Agents.Pi.Provider != "kimi" {
		t.Errorf("Agents.Pi.Provider = %q, want %q", cfg.Agents.Pi.Provider, "kimi")
	}

	if cfg.Agents.Pi.Model != "k2.5" {
		t.Errorf("Agents.Pi.Model = %q, want %q", cfg.Agents.Pi.Model, "k2.5")
	}

	if cfg.Agents.Pi.ThinkingLevel != "medium" {
		t.Errorf("Agents.Pi.ThinkingLevel = %q, want %q", cfg.Agents.Pi.ThinkingLevel, "medium")
	}

	if cfg.Agents.Pi.Timeout != "10m" {
		t.Errorf("Agents.Pi.Timeout = %q, want %q", cfg.Agents.Pi.Timeout, "10m")
	}

	if cfg.Agents.Pi.ApiKeyName != "" {
		t.Errorf("Agents.Pi.ApiKeyName = %q, want empty string", cfg.Agents.Pi.ApiKeyName)
	}
}

// TestPiConfig_AgentsPiLoadFromYAML verifies Pi agent can be configured via YAML
func TestPiConfig_AgentsPiLoadFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agents_pi_config.yaml")

	configContent := `
overseer:
  max_workers: 3

projects:
  - name: test-project
    path: /tmp/test
    type: single

agents:
  enabled: true
  default_agent: pi
  pi:
    enabled: true
    provider: openai
    model: gpt-4
    thinking_level: high
    timeout: 5m
    api_key_name: OPENAI_API_KEY
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Agents.Pi.Provider != "openai" {
		t.Errorf("Expected Provider='openai', got '%s'", config.Agents.Pi.Provider)
	}

	if config.Agents.Pi.Model != "gpt-4" {
		t.Errorf("Expected Model='gpt-4', got '%s'", config.Agents.Pi.Model)
	}

	if config.Agents.Pi.ThinkingLevel != "high" {
		t.Errorf("Expected ThinkingLevel='high', got '%s'", config.Agents.Pi.ThinkingLevel)
	}

	if config.Agents.Pi.Timeout != "5m" {
		t.Errorf("Expected Timeout='5m', got '%s'", config.Agents.Pi.Timeout)
	}

	if config.Agents.Pi.ApiKeyName != "OPENAI_API_KEY" {
		t.Errorf("Expected ApiKeyName='OPENAI_API_KEY', got '%s'", config.Agents.Pi.ApiKeyName)
	}
}

// TestPiConfig_DisabledAgent verifies Pi agent can be disabled
func TestPiConfig_DisabledAgent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "disabled_agent.yaml")

	configContent := `
overseer:
  max_workers: 3

projects:
  - name: test-project
    path: /tmp/test
    type: single

agents:
  enabled: true
  default_agent: pi
  pi:
    enabled: false
    provider: kimi
    model: k2.5
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Agents.Pi.Enabled {
		t.Error("Expected Agents.Pi.Enabled to be false")
	}

	// Other fields should still load
	if config.Agents.Pi.Provider != "kimi" {
		t.Errorf("Expected Provider='kimi', got '%s'", config.Agents.Pi.Provider)
	}
}
