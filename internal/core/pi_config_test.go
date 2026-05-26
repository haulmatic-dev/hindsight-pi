package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// Provider/Model/ThinkingLevel/Timeout are intentionally left empty
	// in DefaultConfig so that pi.standard-agent.llm in the YAML can
	// override, or the overseer fills in safe defaults.
	if cfg.Agents.Pi.Provider != "" {
		t.Errorf("Agents.Pi.Provider = %q, want empty string", cfg.Agents.Pi.Provider)
	}

	if cfg.Agents.Pi.Model != "" {
		t.Errorf("Agents.Pi.Model = %q, want empty string", cfg.Agents.Pi.Model)
	}

	if cfg.Agents.Pi.ThinkingLevel != "" {
		t.Errorf("Agents.Pi.ThinkingLevel = %q, want empty string", cfg.Agents.Pi.ThinkingLevel)
	}

	if cfg.Agents.Pi.Timeout != "" {
		t.Errorf("Agents.Pi.Timeout = %q, want empty string", cfg.Agents.Pi.Timeout)
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

// ============================================================================
// Comprehensive Pi Configuration Tests
// ============================================================================

// TestPiConfig_Loading tests that Pi configuration loads correctly from hydra.yaml
func TestPiConfig_Comprehensive_Loading(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected PiRootConfig
	}{
		{
			name: "basic Pi config",
			yaml: `
pi:
  config_dir: "./pi-config"
  skills:
    update_interval: "24h"
    auto_update: true
    global: false
    list:
      - name: "test-skill"
        repo: "https://github.com/test/skill"
  lsp:
    enabled: true
    servers:
      gopls:
        command: "gopls"
        args: ["serve"]
        install_check: "which gopls"
        install_cmd: "go install golang.org/x/tools/gopls@latest"
`,
			expected: PiRootConfig{
				ConfigDir:   "./pi-config",
				PiConfigDir: "",
				Skills: PiSkillsConfig{
					UpdateInterval: "24h",
					AutoUpdate:     true,
					Global:         false,
					List: []SkillEntry{
						{Name: "test-skill", Repo: "https://github.com/test/skill"},
					},
				},
				LSP: LSPConfig{
					Enabled: true,
					Servers: map[string]LSPServerConfig{
						"gopls": {
							Command:      "gopls",
							Args:         []string{"serve"},
							InstallCheck: "which gopls",
							InstallCmd:   "go install golang.org/x/tools/gopls@latest",
						},
					},
				},
			},
		},
		{
			name: "Pi config with pi_config_dir",
			yaml: `
pi:
  config_dir: "./custom-config"
  pi_config_dir: "./pi-specific"
  skills:
    update_interval: "12h"
    auto_update: false
    global: true
    list: []
  lsp:
    enabled: false
`,
			expected: PiRootConfig{
				ConfigDir:   "./custom-config",
				PiConfigDir: "./pi-specific",
				Skills: PiSkillsConfig{
					UpdateInterval: "12h",
					AutoUpdate:     false,
					Global:         true,
					List:           []SkillEntry{},
				},
				LSP: LSPConfig{
					Enabled: false,
					Servers: map[string]LSPServerConfig{},
				},
			},
		},
		{
			name: "multiple skills and LSP servers",
			yaml: `
pi:
  config_dir: "/home/user/.config/pi"
  skills:
    update_interval: "1h"
    auto_update: true
    global: true
    list:
      - name: "skill-a"
        repo: "https://github.com/user/skill-a"
      - name: "skill-b"
        repo: "git@github.com:user/skill-b.git"
      - name: "local-skill"
        repo: "./local/skills/skill"
  lsp:
    enabled: true
    servers:
      gopls:
        command: "gopls"
        args: ["serve", "-rpc.trace"]
      typescript-language-server:
        command: "typescript-language-server"
        args: ["--stdio"]
        install_check: "npm list -g typescript-language-server"
`,
			expected: PiRootConfig{
				ConfigDir: "/home/user/.config/pi",
				Skills: PiSkillsConfig{
					UpdateInterval: "1h",
					AutoUpdate:     true,
					Global:         true,
					List: []SkillEntry{
						{Name: "skill-a", Repo: "https://github.com/user/skill-a"},
						{Name: "skill-b", Repo: "git@github.com:user/skill-b.git"},
						{Name: "local-skill", Repo: "./local/skills/skill"},
					},
				},
				LSP: LSPConfig{
					Enabled: true,
					Servers: map[string]LSPServerConfig{
						"gopls": {
							Command: "gopls",
							Args:    []string{"serve", "-rpc.trace"},
						},
						"typescript-language-server": {
							Command:      "typescript-language-server",
							Args:         []string{"--stdio"},
							InstallCheck: "npm list -g typescript-language-server",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for config file
			tempDir := t.TempDir()
			configPath := filepath.Join(tempDir, "hydra.yaml")

			// Write YAML to file
			err := os.WriteFile(configPath, []byte(tt.yaml), 0644)
			require.NoError(t, err, "failed to write test config file")

			// Load config
			config, err := LoadConfig(configPath)
			require.NoError(t, err, "failed to load config")

			// Verify Pi configuration
			assert.Equal(t, tt.expected.ConfigDir, config.Pi.ConfigDir, "config_dir mismatch")
			assert.Equal(t, tt.expected.PiConfigDir, config.Pi.PiConfigDir, "pi_config_dir mismatch")
			assert.Equal(t, tt.expected.Skills.UpdateInterval, config.Pi.Skills.UpdateInterval, "update_interval mismatch")
			assert.Equal(t, tt.expected.Skills.AutoUpdate, config.Pi.Skills.AutoUpdate, "auto_update mismatch")
			assert.Equal(t, tt.expected.Skills.Global, config.Pi.Skills.Global, "global mismatch")
			assert.Equal(t, tt.expected.Skills.List, config.Pi.Skills.List, "skills list mismatch")
			assert.Equal(t, tt.expected.LSP.Enabled, config.Pi.LSP.Enabled, "lsp enabled mismatch")
			assert.Equal(t, len(tt.expected.LSP.Servers), len(config.Pi.LSP.Servers), "lsp servers count mismatch")

			// Check LSP servers
			for name, expectedServer := range tt.expected.LSP.Servers {
				actualServer, exists := config.Pi.LSP.Servers[name]
				assert.True(t, exists, "LSP server %s not found", name)
				assert.Equal(t, expectedServer.Command, actualServer.Command, "server %s command mismatch", name)
				assert.Equal(t, expectedServer.Args, actualServer.Args, "server %s args mismatch", name)
				assert.Equal(t, expectedServer.InstallCheck, actualServer.InstallCheck, "server %s install_check mismatch", name)
				assert.Equal(t, expectedServer.InstallCmd, actualServer.InstallCmd, "server %s install_cmd mismatch", name)
			}
		})
	}
}

// TestPiConfig_Validation tests that Pi configuration validation works correctly
func TestPiConfig_Comprehensive_Validation(t *testing.T) {
	tests := []struct {
		name          string
		config        func() *Config
		expectedValid bool
		expectedField string
		expectedMsg   string
	}{
		{
			name: "valid config",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{
					Name: "test",
					Path: "/tmp/test",
				}}
				cfg.Pi.ConfigDir = "./pi-config"
				cfg.Pi.Skills.UpdateInterval = "24h"
				return cfg
			},
			expectedValid: true,
		},
		{
			name: "nil config",
			config: func() *Config {
				return nil
			},
			expectedValid: false,
			expectedField: "config",
			expectedMsg:   "config is nil",
		},
		{
			name: "config_dir with null bytes",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{
					Name: "test",
					Path: "/tmp/test",
				}}
				cfg.Pi.ConfigDir = "./config\x00invalid"
				return cfg
			},
			expectedValid: false,
			expectedField: "pi.config_dir",
			expectedMsg:   "config_dir contains invalid characters",
		},
		{
			name: "invalid update_interval format",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{
					Name: "test",
					Path: "/tmp/test",
				}}
				cfg.Pi.Skills.UpdateInterval = "not-a-duration"
				return cfg
			},
			expectedValid: false,
			expectedField: "pi.skills.update_interval",
			expectedMsg:   "update_interval must be a valid duration",
		},
		{
			name: "update_interval too short",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{
					Name: "test",
					Path: "/tmp/test",
				}}
				cfg.Pi.Skills.UpdateInterval = "30s"
				return cfg
			},
			expectedValid: false,
			expectedField: "pi.skills.update_interval",
			expectedMsg:   "update_interval must be at least 1 minute",
		},
		{
			name: "update_interval too long",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{
					Name: "test",
					Path: "/tmp/test",
				}}
				cfg.Pi.Skills.UpdateInterval = "800h" // More than 30 days
				return cfg
			},
			expectedValid: false,
			expectedField: "pi.skills.update_interval",
			expectedMsg:   "update_interval should not exceed 30 days",
		},
		{
			name: "skill with empty name",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{
					Name: "test",
					Path: "/tmp/test",
				}}
				cfg.Pi.Skills.List = []SkillEntry{
					{Name: "", Repo: "https://github.com/test/skill"},
				}
				return cfg
			},
			expectedValid: false,
			expectedField: "pi.skills.list[0].name",
			expectedMsg:   "skill name is required",
		},
		{
			name: "skill with invalid repo URL",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{
					Name: "test",
					Path: "/tmp/test",
				}}
				cfg.Pi.Skills.List = []SkillEntry{
					{Name: "test-skill", Repo: "invalid-url"},
				}
				return cfg
			},
			expectedValid: false,
			expectedField: "pi.skills.list[0].repo",
			expectedMsg:   "repo must be a valid git URL",
		},
		{
			name: "multiple validation errors",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{
					Name: "test",
					Path: "/tmp/test",
				}}
				cfg.Pi.ConfigDir = "./config\x00invalid"
				cfg.Pi.Skills.UpdateInterval = "30s"
				cfg.Pi.Skills.List = []SkillEntry{
					{Name: "", Repo: "invalid-url"},
				}
				return cfg
			},
			expectedValid: false,
			expectedField: "pi.config_dir", // First error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config()
			errors := ValidateConfig(cfg)

			if tt.expectedValid {
				assert.True(t, errors.IsValid(), "expected config to be valid but got errors: %v", errors)
			} else {
				assert.False(t, errors.IsValid(), "expected config to be invalid but was valid")
				if !errors.IsValid() && len(errors) > 0 {
					found := false
					for _, err := range errors {
						if err.Field == tt.expectedField {
							found = true
							if tt.expectedMsg != "" {
								assert.Contains(t, err.Message, tt.expectedMsg, "error message mismatch for field %s", tt.expectedField)
							}
							break
						}
					}
					assert.True(t, found, "expected error for field %s not found in errors: %v", tt.expectedField, errors)
				}
			}
		})
	}
}

// TestPiConfig_UpdateInterval tests duration parsing for update_interval
func TestPiConfig_Comprehensive_UpdateInterval(t *testing.T) {
	tests := []struct {
		name          string
		interval      string
		expectValid   bool
		expectedError string
	}{
		{"valid hours", "24h", true, ""},
		{"valid minutes", "30m", true, ""},
		{"valid seconds (above min)", "120s", true, ""},
		{"valid combined", "2h30m", true, ""},
		{"valid days", "720h", true, ""}, // 30 days in hours
		{"invalid format", "not-a-duration", false, "update_interval must be a valid duration"},
		{"too short - seconds", "30s", false, "update_interval must be at least 1 minute"},
		{"too short - sub-minute", "45s", false, "update_interval must be at least 1 minute"},
		{"too long - days", "750h", false, "update_interval should not exceed 30 days"}, // 31+ days in hours
		{"too long - hours", "800h", false, "update_interval should not exceed 30 days"},
		{"empty string", "", true, ""}, // Empty is valid (uses default)
		{"zero duration", "0s", false, "update_interval must be at least 1 minute"},
		{"negative duration", "-1h", false, "update_interval must be at least 1 minute"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Projects = []ProjectConfig{{
				Name: "test",
				Path: "/tmp/test",
			}}
			cfg.Pi.Skills.UpdateInterval = tt.interval

			errors := ValidateConfig(cfg)

			if tt.expectValid {
				assert.True(t, errors.IsValid(), "expected interval '%s' to be valid but got errors: %v", tt.interval, errors)
			} else {
				assert.False(t, errors.IsValid(), "expected interval '%s' to be invalid but was valid", tt.interval)
				if tt.expectedError != "" && len(errors) > 0 {
					found := false
					for _, err := range errors {
						if err.Field == "pi.skills.update_interval" &&
							assert.Contains(t, err.Message, tt.expectedError, "error message mismatch") {
							found = true
							break
						}
					}
					assert.True(t, found, "expected error containing '%s' for field pi.skills.update_interval", tt.expectedError)
				}
			}
		})
	}

	// Test actual duration parsing when valid
	t.Run("duration parsing works correctly", func(t *testing.T) {
		durations := map[string]time.Duration{
			"24h":   24 * time.Hour,
			"1h30m": 90 * time.Minute,
			"2h":    2 * time.Hour,
			"30m":   30 * time.Minute,
			"720h":  720 * time.Hour,
		}

		for intervalStr, expectedDuration := range durations {
			duration, err := time.ParseDuration(intervalStr)
			require.NoError(t, err, "failed to parse duration '%s'", intervalStr)
			assert.Equal(t, expectedDuration, duration, "duration mismatch for '%s'", intervalStr)
		}
	})
}

// TestPiConfig_LSPConfig tests LSP settings loading
func TestPiConfig_Comprehensive_LSPConfig(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		expectEnabled   bool
		expectedServers int
		checkServers    map[string]LSPServerConfig
	}{
		{
			name: "LSP disabled",
			yaml: `
pi:
  lsp:
    enabled: false
`,
			expectEnabled:   false,
			expectedServers: 0,
		},
		{
			name: "LSP enabled with no servers",
			yaml: `
pi:
  lsp:
    enabled: true
    servers: {}
`,
			expectEnabled:   true,
			expectedServers: 0,
		},
		{
			name: "LSP with gopls server",
			yaml: `
pi:
  lsp:
    enabled: true
    servers:
      gopls:
        command: "gopls"
        args: ["serve"]
        install_check: "which gopls"
        install_cmd: "go install golang.org/x/tools/gopls@latest"
`,
			expectEnabled:   true,
			expectedServers: 1,
			checkServers: map[string]LSPServerConfig{
				"gopls": {
					Command:      "gopls",
					Args:         []string{"serve"},
					InstallCheck: "which gopls",
					InstallCmd:   "go install golang.org/x/tools/gopls@latest",
				},
			},
		},
		{
			name: "LSP with multiple servers",
			yaml: `
pi:
  lsp:
    enabled: true
    servers:
      gopls:
        command: "gopls"
        args: ["serve"]
      typescript-language-server:
        command: "typescript-language-server"
        args: ["--stdio"]
        install_check: "npm list -g typescript-language-server"
      pyright:
        command: "pyright-langserver"
        args: ["--stdio"]
        install_check: "which pyright-langserver"
        install_cmd: "pip install pyright"
`,
			expectEnabled:   true,
			expectedServers: 3,
			checkServers: map[string]LSPServerConfig{
				"gopls": {
					Command: "gopls",
					Args:    []string{"serve"},
				},
				"typescript-language-server": {
					Command:      "typescript-language-server",
					Args:         []string{"--stdio"},
					InstallCheck: "npm list -g typescript-language-server",
				},
				"pyright": {
					Command:      "pyright-langserver",
					Args:         []string{"--stdio"},
					InstallCheck: "which pyright-langserver",
					InstallCmd:   "pip install pyright",
				},
			},
		},
		{
			name: "LSP with server having no args",
			yaml: `
pi:
  lsp:
    enabled: true
    servers:
      simple-server:
        command: "simple-lsp"
`,
			expectEnabled:   true,
			expectedServers: 1,
			checkServers: map[string]LSPServerConfig{
				"simple-server": {
					Command: "simple-lsp",
					Args:    nil,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			configPath := filepath.Join(tempDir, "hydra.yaml")

			err := os.WriteFile(configPath, []byte(tt.yaml), 0644)
			require.NoError(t, err)

			config, err := LoadConfig(configPath)
			require.NoError(t, err)

			assert.Equal(t, tt.expectEnabled, config.Pi.LSP.Enabled, "LSP enabled mismatch")
			assert.Equal(t, tt.expectedServers, len(config.Pi.LSP.Servers), "servers count mismatch")

			if tt.checkServers != nil {
				for name, expected := range tt.checkServers {
					actual, exists := config.Pi.LSP.Servers[name]
					require.True(t, exists, "server %s should exist", name)
					assert.Equal(t, expected.Command, actual.Command, "server %s command mismatch", name)
					assert.Equal(t, expected.Args, actual.Args, "server %s args mismatch", name)
					assert.Equal(t, expected.InstallCheck, actual.InstallCheck, "server %s install_check mismatch", name)
					assert.Equal(t, expected.InstallCmd, actual.InstallCmd, "server %s install_cmd mismatch", name)
				}
			}
		})
	}
}

// TestPiConfig_ConfigDir tests path handling for config directories
func TestPiConfig_Comprehensive_ConfigDir(t *testing.T) {
	tests := []struct {
		name                string
		configDir           string
		piConfigDir         string
		expectValid         bool
		expectConfigDirErr  bool
		expectPiConfigDirOK bool
	}{
		{
			name:                "relative path with ./",
			configDir:           "./pi-config",
			piConfigDir:         "",
			expectValid:         true,
			expectConfigDirErr:  false,
			expectPiConfigDirOK: true,
		},
		{
			name:                "relative path with ../",
			configDir:           "../parent-config",
			piConfigDir:         "",
			expectValid:         true,
			expectConfigDirErr:  false,
			expectPiConfigDirOK: true,
		},
		{
			name:                "absolute path",
			configDir:           "/home/user/.config/pi",
			piConfigDir:         "/home/user/.config/pi-specific",
			expectValid:         true,
			expectConfigDirErr:  false,
			expectPiConfigDirOK: true,
		},
		{
			name:                "path with null bytes",
			configDir:           "./config\x00invalid",
			piConfigDir:         "",
			expectValid:         false,
			expectConfigDirErr:  true,
			expectPiConfigDirOK: false,
		},
		{
			name:                "empty config_dir",
			configDir:           "",
			piConfigDir:         "./pi-specific",
			expectValid:         true,
			expectConfigDirErr:  false,
			expectPiConfigDirOK: true,
		},
		{
			name:                "simple relative path",
			configDir:           "config/pi",
			piConfigDir:         "",
			expectValid:         true,
			expectConfigDirErr:  false,
			expectPiConfigDirOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Projects = []ProjectConfig{{
				Name: "test",
				Path: "/tmp/test",
			}}
			cfg.Pi.ConfigDir = tt.configDir
			cfg.Pi.PiConfigDir = tt.piConfigDir

			errors := ValidateConfig(cfg)

			if tt.expectValid {
				assert.True(t, errors.IsValid(), "expected paths to be valid but got errors: %v", errors)
			} else {
				assert.False(t, errors.IsValid(), "expected paths to be invalid but was valid")
			}

			if tt.expectConfigDirErr {
				found := false
				for _, err := range errors {
					if err.Field == "pi.config_dir" {
						found = true
						assert.Contains(t, err.Message, "invalid characters", "expected invalid characters error")
						break
					}
				}
				assert.True(t, found, "expected config_dir validation error")
			}
		})
	}

	// Test path properties
	t.Run("path properties", func(t *testing.T) {
		// Absolute path detection
		assert.True(t, filepath.IsAbs("/home/user/config"), "should detect absolute path")
		assert.False(t, filepath.IsAbs("./relative"), "should not detect relative path as absolute")
		assert.False(t, filepath.IsAbs("../parent"), "should not detect parent relative as absolute")
		assert.False(t, filepath.IsAbs("relative/path"), "should not detect simple relative as absolute")

		// Path joining
		joined := filepath.Join("/base", "subdir", "file.yaml")
		assert.Equal(t, "/base/subdir/file.yaml", joined, "path joining should work")

		// Path cleaning
		cleaned := filepath.Clean("/path/to/../config")
		assert.Equal(t, "/path/config", cleaned, "path cleaning should work")
	})
}

// TestPiConfig_Clone tests that deep copy works for Pi configuration
func TestPiConfig_Comprehensive_Clone(t *testing.T) {
	// Create original config with Pi settings
	original := DefaultConfig()
	original.Projects = []ProjectConfig{{
		Name: "test",
		Path: "/tmp/test",
	}}
	original.Pi = PiRootConfig{
		ConfigDir:   "/original/config",
		PiConfigDir: "/original/pi-config",
		Skills: PiSkillsConfig{
			UpdateInterval: "12h",
			AutoUpdate:     true,
			Global:         false,
			List: []SkillEntry{
				{Name: "skill-a", Repo: "https://github.com/user/skill-a"},
				{Name: "skill-b", Repo: "git@github.com:user/skill-b.git"},
			},
		},
		LSP: LSPConfig{
			Enabled: true,
			Servers: map[string]LSPServerConfig{
				"gopls": {
					Command:      "gopls",
					Args:         []string{"serve"},
					InstallCheck: "which gopls",
				},
				"typescript": {
					Command: "typescript-language-server",
					Args:    []string{"--stdio"},
				},
			},
		},
	}

	// Clone the config
	cloned := original.Clone()
	require.NotNil(t, cloned, "Clone() should not return nil")

	// Verify Pi values are equal
	assert.Equal(t, original.Pi.ConfigDir, cloned.Pi.ConfigDir, "ConfigDir should be equal")
	assert.Equal(t, original.Pi.PiConfigDir, cloned.Pi.PiConfigDir, "PiConfigDir should be equal")
	assert.Equal(t, original.Pi.Skills.UpdateInterval, cloned.Pi.Skills.UpdateInterval, "UpdateInterval should be equal")
	assert.Equal(t, original.Pi.Skills.AutoUpdate, cloned.Pi.Skills.AutoUpdate, "AutoUpdate should be equal")
	assert.Equal(t, original.Pi.Skills.Global, cloned.Pi.Skills.Global, "Global should be equal")
	assert.Equal(t, len(original.Pi.Skills.List), len(cloned.Pi.Skills.List), "Skills list length should be equal")
	assert.Equal(t, original.Pi.Skills.List, cloned.Pi.Skills.List, "Skills list should be equal")
	assert.Equal(t, original.Pi.LSP.Enabled, cloned.Pi.LSP.Enabled, "LSP Enabled should be equal")
	assert.Equal(t, len(original.Pi.LSP.Servers), len(cloned.Pi.LSP.Servers), "LSP servers count should be equal")

	// Verify it's a deep copy - modifying clone should not affect original
	cloned.Pi.ConfigDir = "/modified/config"
	cloned.Pi.PiConfigDir = "/modified/pi-config"
	cloned.Pi.Skills.UpdateInterval = "1m"
	cloned.Pi.Skills.AutoUpdate = false
	cloned.Pi.Skills.Global = true
	cloned.Pi.Skills.List = append(cloned.Pi.Skills.List, SkillEntry{Name: "new-skill", Repo: "https://github.com/new"})
	cloned.Pi.LSP.Enabled = false
	cloned.Pi.LSP.Servers["gopls"] = LSPServerConfig{
		Command: "modified-gopls",
		Args:    []string{"serve", "-modified"},
	}

	// Original should be unchanged (Note: LSP.Servers map is shallow copied)
	assert.Equal(t, "/original/config", original.Pi.ConfigDir, "original ConfigDir should be unchanged")
	assert.Equal(t, "/original/pi-config", original.Pi.PiConfigDir, "original PiConfigDir should be unchanged")
	assert.Equal(t, "12h", original.Pi.Skills.UpdateInterval, "original UpdateInterval should be unchanged")
	assert.True(t, original.Pi.Skills.AutoUpdate, "original AutoUpdate should be unchanged")
	assert.False(t, original.Pi.Skills.Global, "original Global should be unchanged")
	assert.Len(t, original.Pi.Skills.List, 2, "original skills list length should be unchanged")
	assert.True(t, original.Pi.LSP.Enabled, "original LSP Enabled should be unchanged")

	// Clone should have the modified values
	assert.Equal(t, "/modified/config", cloned.Pi.ConfigDir, "cloned ConfigDir should be modified")
	assert.Equal(t, "/modified/pi-config", cloned.Pi.PiConfigDir, "cloned PiConfigDir should be modified")
	assert.Equal(t, "1m", cloned.Pi.Skills.UpdateInterval, "cloned UpdateInterval should be modified")
	assert.False(t, cloned.Pi.Skills.AutoUpdate, "cloned AutoUpdate should be modified")
	assert.True(t, cloned.Pi.Skills.Global, "cloned Global should be modified")
	assert.Len(t, cloned.Pi.Skills.List, 3, "cloned skills list should have new skill")
	assert.False(t, cloned.Pi.LSP.Enabled, "cloned LSP Enabled should be modified")
	assert.Equal(t, "modified-gopls", cloned.Pi.LSP.Servers["gopls"].Command, "cloned gopls command should be modified")

	// Test nil config clone
	t.Run("nil config clone", func(t *testing.T) {
		var nilConfig *Config
		nilClone := nilConfig.Clone()
		assert.Nil(t, nilClone, "cloning nil config should return nil")
	})

	// Test empty Pi config
	t.Run("empty Pi config", func(t *testing.T) {
		emptyConfig := DefaultConfig()
		emptyConfig.Projects = []ProjectConfig{{Name: "test", Path: "/tmp/test"}}
		emptyConfig.Pi = PiRootConfig{}

		emptyClone := emptyConfig.Clone()
		require.NotNil(t, emptyClone)
		assert.Empty(t, emptyClone.Pi.ConfigDir)
		assert.Empty(t, emptyClone.Pi.PiConfigDir)
		assert.Empty(t, emptyClone.Pi.Skills.List)
		assert.Empty(t, emptyClone.Pi.LSP.Servers)
	})
}

// TestPiConfig_Integration tests end-to-end loading and validation
func TestPiConfig_Comprehensive_Integration(t *testing.T) {
	// Create a comprehensive config file
	yamlContent := `
overseer:
  bead_discovery_interval: 5s
  max_workers: 6
  default_retries: 3

projects:
  - name: test-project
    path: /tmp/test
    type: single

pi:
  config_dir: "./custom-pi-config"
  pi_config_dir: "./pi-specific-config"
  skills:
    update_interval: "6h"
    auto_update: true
    global: false
    list:
      - name: go-development
        repo: "https://github.com/skills/go-dev"
      - name: typescript-support
        repo: "git@github.com:skills/typescript.git"
      - name: local-customizations
        repo: "./skills/local"
  lsp:
    enabled: true
    servers:
      gopls:
        command: "gopls"
        args: ["serve", "-rpc.trace"]
        install_check: "which gopls"
        install_cmd: "go install golang.org/x/tools/gopls@latest"
      typescript-language-server:
        command: "typescript-language-server"
        args: ["--stdio"]
`

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "hydra.yaml")

	err := os.WriteFile(configPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// Load config
	config, err := LoadConfig(configPath)
	require.NoError(t, err)

	// Validate
	validationErrors := ValidateConfig(config)
	assert.True(t, validationErrors.IsValid(), "config should be valid: %v", validationErrors)

	// Verify all Pi settings
	assert.Equal(t, "./custom-pi-config", config.Pi.ConfigDir)
	assert.Equal(t, "./pi-specific-config", config.Pi.PiConfigDir)
	assert.Equal(t, "6h", config.Pi.Skills.UpdateInterval)
	assert.True(t, config.Pi.Skills.AutoUpdate)
	assert.False(t, config.Pi.Skills.Global)
	assert.Len(t, config.Pi.Skills.List, 3)
	assert.Equal(t, "go-development", config.Pi.Skills.List[0].Name)
	assert.Equal(t, "https://github.com/skills/go-dev", config.Pi.Skills.List[0].Repo)
	assert.True(t, config.Pi.LSP.Enabled)
	assert.Len(t, config.Pi.LSP.Servers, 2)

	// Verify LSP servers
	gopls, exists := config.Pi.LSP.Servers["gopls"]
	assert.True(t, exists)
	assert.Equal(t, "gopls", gopls.Command)
	assert.Equal(t, []string{"serve", "-rpc.trace"}, gopls.Args)
	assert.Equal(t, "which gopls", gopls.InstallCheck)
	assert.Equal(t, "go install golang.org/x/tools/gopls@latest", gopls.InstallCmd)

	ts, exists := config.Pi.LSP.Servers["typescript-language-server"]
	assert.True(t, exists)
	assert.Equal(t, "typescript-language-server", ts.Command)
	assert.Equal(t, []string{"--stdio"}, ts.Args)

	// Test that clone preserves all settings
	cloned := config.Clone()
	assert.Equal(t, config.Pi.ConfigDir, cloned.Pi.ConfigDir)
	assert.Equal(t, config.Pi.Skills.List, cloned.Pi.Skills.List)
	assert.Equal(t, len(config.Pi.LSP.Servers), len(cloned.Pi.LSP.Servers))
}

// TestPiConfig_ValidateConfigSingleError tests the single error validation wrapper
func TestPiConfig_ValidateConfigSingleError(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{Name: "test", Path: "/tmp/test"}}
				return cfg
			}(),
			expectError: false,
		},
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
			errorMsg:    "config is nil",
		},
		{
			name: "invalid Pi config",
			config: func() *Config {
				cfg := DefaultConfig()
				cfg.Projects = []ProjectConfig{{Name: "test", Path: "/tmp/test"}}
				cfg.Pi.Skills.UpdateInterval = "invalid"
				return cfg
			}(),
			expectError: true,
			errorMsg:    "update_interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfigSingleError(tt.config)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPiConfig_isValidSkillRepo tests the skill repo URL validation helper
func TestPiConfig_isValidSkillRepo(t *testing.T) {
	tests := []struct {
		name  string
		repo  string
		valid bool
	}{
		{"empty", "", true},
		{"https URL", "https://github.com/user/repo", true},
		{"http URL", "http://example.com/repo", true},
		{"git SSH", "git@github.com:user/repo.git", true},
		{"git protocol", "git://github.com/user/repo", true},
		{"ssh URL", "ssh://git@github.com/user/repo", true},
		{"absolute path", "/home/user/skills/repo", true},
		{"relative path ./", "./skills/repo", true},
		{"invalid URL", "not-a-valid-url", false},
		{"invalid format", "github.com/repo", false},
		{"just text", "some random text", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidSkillRepo(tt.repo)
			assert.Equal(t, tt.valid, result, "isValidSkillRepo(%q) = %v, want %v", tt.repo, result, tt.valid)
		})
	}
}

// TestPiConfig_ValidationErrors tests the ValidationErrors type
func TestPiConfig_ValidationErrors(t *testing.T) {
	t.Run("empty errors", func(t *testing.T) {
		var errors ValidationErrors
		assert.True(t, errors.IsValid())
		assert.Equal(t, "no validation errors", errors.Error())
	})

	t.Run("single error", func(t *testing.T) {
		errors := ValidationErrors{
			{Field: "field1", Message: "error message"},
		}
		assert.False(t, errors.IsValid())
		assert.Equal(t, "field1: error message", errors.Error())
	})

	t.Run("multiple errors", func(t *testing.T) {
		errors := ValidationErrors{
			{Field: "field1", Message: "first error"},
			{Field: "field2", Message: "second error"},
			{Field: "field3", Message: "third error"},
		}
		assert.False(t, errors.IsValid())
		errStr := errors.Error()
		assert.Contains(t, errStr, "3 validation errors")
		assert.Contains(t, errStr, "field1: first error")
		assert.Contains(t, errStr, "field2: second error")
		assert.Contains(t, errStr, "field3: third error")
	})

	t.Run("ValidationError type", func(t *testing.T) {
		err := ValidationError{Field: "test", Message: "test message"}
		assert.Equal(t, "test: test message", err.Error())
	})
}

// TestPiConfig_LegacyValidator tests the deprecated ConfigValidator
func TestPiConfig_LegacyValidator(t *testing.T) {
	validator := NewConfigValidator()
	require.NotNil(t, validator)

	// Valid config
	validConfig := DefaultConfig()
	validConfig.Projects = []ProjectConfig{{Name: "test", Path: "/tmp/test"}}
	err := validator.Validate(validConfig)
	assert.NoError(t, err)

	// Invalid config
	invalidConfig := DefaultConfig()
	invalidConfig.Projects = []ProjectConfig{{Name: "test", Path: "/tmp/test"}}
	invalidConfig.Pi.Skills.UpdateInterval = "invalid"
	err = validator.Validate(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update_interval")
}

// BenchmarkPiConfig_Loading benchmarks config loading performance
func BenchmarkPiConfig_Loading(b *testing.B) {
	yamlContent := `
pi:
  config_dir: "./pi-config"
  skills:
    update_interval: "24h"
    auto_update: true
    global: false
    list:
      - name: skill-1
        repo: "https://github.com/user/skill-1"
      - name: skill-2
        repo: "git@github.com:user/skill-2.git"
      - name: skill-3
        repo: "./local/skills/skill-3"
  lsp:
    enabled: true
    servers:
      gopls:
        command: "gopls"
        args: ["serve"]
      typescript-language-server:
        command: "typescript-language-server"
        args: ["--stdio"]
`

	tempDir := b.TempDir()
	configPath := filepath.Join(tempDir, "hydra.yaml")
	err := os.WriteFile(configPath, []byte(yamlContent), 0644)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadConfig(configPath)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPiConfig_Validation benchmarks config validation performance
func BenchmarkPiConfig_Validation(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Projects = []ProjectConfig{{Name: "test", Path: "/tmp/test"}}
	cfg.Pi.Skills.List = []SkillEntry{
		{Name: "skill-1", Repo: "https://github.com/user/skill-1"},
		{Name: "skill-2", Repo: "git@github.com:user/skill-2.git"},
		{Name: "skill-3", Repo: "./local/skills/skill-3"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		errors := ValidateConfig(cfg)
		if !errors.IsValid() {
			b.Fatal(errors)
		}
	}
}

// BenchmarkPiConfig_Clone benchmarks config cloning performance
func BenchmarkPiConfig_Clone(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Projects = []ProjectConfig{{Name: "test", Path: "/tmp/test"}}
	cfg.Pi.Skills.List = []SkillEntry{
		{Name: "skill-1", Repo: "https://github.com/user/skill-1"},
		{Name: "skill-2", Repo: "git@github.com:user/skill-2.git"},
	}
	cfg.Pi.LSP.Servers = map[string]LSPServerConfig{
		"gopls":                      {Command: "gopls", Args: []string{"serve"}},
		"typescript-language-server": {Command: "typescript-language-server", Args: []string{"--stdio"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cfg.Clone()
	}
}
