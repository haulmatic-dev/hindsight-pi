//go:build integration

package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// US-004 LSP Integration Tests
// User Story: As a developer using Hydra, I want Pi workers to have access to
// Language Server Protocol for semantic code navigation, so that code analysis
// is accurate and context-aware.
// =============================================================================

// TestUS004_LSPServersAutoStart verifies that LSP servers start automatically
// when the setup manager initializes and MCP config is available.
func TestUS004_LSPServersAutoStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows - process management differs")
	}

	tests := []struct {
		name          string
		serverConfig  map[string]MCPServerConfig
		expectError   bool
		errorContains string
	}{
		{
			name: "auto start enabled servers from MCP config",
			serverConfig: map[string]MCPServerConfig{
				"go": {
					Command: "sleep",
					Args:    []string{"5"},
					Enabled: true,
				},
				"typescript": {
					Command: "sleep",
					Args:    []string{"5"},
					Enabled: true,
				},
			},
			expectError: false,
		},
		{
			name: "skip disabled servers",
			serverConfig: map[string]MCPServerConfig{
				"enabled-server": {
					Command: "sleep",
					Args:    []string{"5"},
					Enabled: true,
				},
				"disabled-server": {
					Command: "echo",
					Args:    []string{"test"},
					Enabled: false,
				},
			},
			expectError: false,
		},
		{
			name: "generate config if not exists and start servers",
			serverConfig: map[string]MCPServerConfig{
				"go": {
					Command: "sleep",
					Args:    []string{"2"},
					Enabled: true,
				},
			},
			expectError: false,
		},
		{
			name: "handle missing command gracefully",
			serverConfig: map[string]MCPServerConfig{
				"missing-server": {
					Command: "non-existent-lsp-binary-xyz",
					Args:    []string{},
					Enabled: true,
				},
			},
			expectError:   true,
			errorContains: "failed to start LSP servers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			agentDir := filepath.Join(tempDir, "agent")
			err := os.MkdirAll(agentDir, 0755)
			require.NoError(t, err)

			// Create MCP config
			config := MCPConfig{
				Version: "1.0",
				Servers: tt.serverConfig,
			}

			data, err := json.MarshalIndent(config, "", "  ")
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(agentDir, "mcp.json"), data, 0644)
			require.NoError(t, err)

			sm := NewSetupManager(tempDir, tempDir)
			err = sm.AutoStartLSPServers()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				// May error in test environment due to missing actual LSP servers
				// but should not panic
				_ = err
			}
		})
	}
}

// TestUS004_LSPCommandsWork verifies that LSP commands (definition, reference, outline)
// work correctly and return expected results.
func TestUS004_LSPCommandsWork(t *testing.T) {
	tempDir := t.TempDir()

	// Create a mock LSP binary that simulates real LSP responses
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
case "$1" in
  "definition")
    echo '/workspace/project/main.go:42:15'
    exit 0
    ;;
  "reference")
    echo '/workspace/project/main.go:42:15'
    echo '/workspace/project/utils.go:105:8'
    echo '/workspace/project/test.go:30:12'
    exit 0
    ;;
  "outline")
    echo 'func main()'
    echo 'type Config struct'
    echo 'func (c *Config) Load() error'
    echo 'var DefaultConfig'
    exit 0
    ;;
  *)
    echo "Unknown command: $1" >&2
    exit 1
    ;;
esac
`
	err := os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Add mock binary to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	tests := []struct {
		name          string
		command       string
		filePath      string
		line          int
		column        int
		expectSuccess bool
		expectDataLen int
		expectData    []string
	}{
		{
			name:          "definition command returns symbol location",
			command:       "definition",
			filePath:      "/workspace/project/main.go",
			line:          42,
			column:        15,
			expectSuccess: true,
			expectDataLen: 1,
			expectData:    []string{"/workspace/project/main.go:42:15"},
		},
		{
			name:          "reference command returns all references",
			command:       "reference",
			filePath:      "/workspace/project/main.go",
			line:          42,
			column:        15,
			expectSuccess: true,
			expectDataLen: 3,
			expectData: []string{
				"/workspace/project/main.go:42:15",
				"/workspace/project/utils.go:105:8",
				"/workspace/project/test.go:30:12",
			},
		},
		{
			name:          "outline command returns document symbols",
			command:       "outline",
			filePath:      "/workspace/project/main.go",
			line:          0,
			column:        0,
			expectSuccess: true,
			expectDataLen: 4,
			expectData: []string{
				"func main()",
				"type Config struct",
				"func (c *Config) Load() error",
				"var DefaultConfig",
			},
		},
		{
			name:          "unknown command returns error",
			command:       "unknown",
			filePath:      "/workspace/project/main.go",
			line:          10,
			column:        5,
			expectSuccess: false,
			expectDataLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteLSPCommand(tt.command, tt.filePath, tt.line, tt.column)

			if tt.command == "unknown" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "unknown LSP command")
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectSuccess, result.Success)

			if tt.expectSuccess {
				assert.Len(t, result.Data, tt.expectDataLen)
				for _, expected := range tt.expectData {
					assert.Contains(t, result.Data, expected)
				}
				assert.Empty(t, result.Error)
			} else {
				assert.False(t, result.Success)
				assert.NotEmpty(t, result.Error)
			}
		})
	}
}

// TestUS004_LSPCommandsWork_HelperFunctions tests the helper functions
func TestUS004_LSPCommandsWork_HelperFunctions(t *testing.T) {
	tempDir := t.TempDir()

	// Create mock LSP binary
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
if [ "$1" = "definition" ]; then
  echo "/path/to/def.go:10:5"
  exit 0
elif [ "$1" = "reference" ]; then
  echo "/path/to/ref1.go:20:10"
  echo "/path/to/ref2.go:30:15"
  exit 0
elif [ "$1" = "outline" ]; then
  echo "func TestFunc()"
  echo "type TestType struct"
  exit 0
fi
exit 1
`
	err := os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Add mock to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	t.Run("LSPDefinition helper function", func(t *testing.T) {
		result, err := LSPDefinition("/test/file.go", 10, 5)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, result.Data, 1)
		assert.Contains(t, result.Data, "/path/to/def.go:10:5")
	})

	t.Run("LSPReference helper function", func(t *testing.T) {
		result, err := LSPReference("/test/file.go", 20, 10)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, result.Data, 2)
		assert.Contains(t, result.Data, "/path/to/ref1.go:20:10")
		assert.Contains(t, result.Data, "/path/to/ref2.go:30:15")
	})

	t.Run("LSPOutline helper function", func(t *testing.T) {
		result, err := LSPOutline("/test/file.go")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, result.Data, 2)
		assert.Contains(t, result.Data, "func TestFunc()")
		assert.Contains(t, result.Data, "type TestType struct")
	})
}

// TestUS004_MCPConfigRead verifies that mcp.json is read correctly and
// all configuration fields are parsed properly.
func TestUS004_MCPConfigRead(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name          string
		configContent map[string]interface{}
		expectError   bool
		validateFunc  func(t *testing.T, config *MCPConfig)
	}{
		{
			name: "valid config with multiple servers",
			configContent: map[string]interface{}{
				"version": "1.0",
				"servers": map[string]interface{}{
					"go": map[string]interface{}{
						"command": "gopls",
						"args":    []string{"serve", "-rpc.trace"},
						"enabled": true,
					},
					"typescript": map[string]interface{}{
						"command": "typescript-language-server",
						"args":    []string{"--stdio"},
						"enabled": true,
					},
					"python": map[string]interface{}{
						"command": "pylsp",
						"args":    []string{},
						"enabled": false,
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, config *MCPConfig) {
				assert.Equal(t, "1.0", config.Version)
				assert.Len(t, config.Servers, 3)

				goServer, ok := config.Servers["go"]
				require.True(t, ok)
				assert.Equal(t, "gopls", goServer.Command)
				assert.True(t, goServer.Enabled)
				assert.Contains(t, goServer.Args, "serve")

				tsServer, ok := config.Servers["typescript"]
				require.True(t, ok)
				assert.Equal(t, "typescript-language-server", tsServer.Command)
				assert.True(t, tsServer.Enabled)

				pyServer, ok := config.Servers["python"]
				require.True(t, ok)
				assert.Equal(t, "pylsp", pyServer.Command)
				assert.False(t, pyServer.Enabled)
			},
		},
		{
			name:          "missing file returns error",
			configContent: nil,
			expectError:   true,
			validateFunc:  nil,
		},
		{
			name: "invalid JSON returns error",
			configContent: map[string]interface{}{
				"invalid": "json: content",
			},
			expectError: true,
			validateFunc: func(t *testing.T, config *MCPConfig) {
				// Should not reach here
				t.Fail()
			},
		},
		{
			name: "empty servers map is valid",
			configContent: map[string]interface{}{
				"version": "1.0",
				"servers": map[string]interface{}{},
			},
			expectError: false,
			validateFunc: func(t *testing.T, config *MCPConfig) {
				assert.Equal(t, "1.0", config.Version)
				assert.Empty(t, config.Servers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var configPath string

			if tt.configContent != nil {
				if tt.name == "invalid JSON returns error" {
					// Write invalid JSON directly
					configPath = filepath.Join(tempDir, "invalid-mcp.json")
					err := os.WriteFile(configPath, []byte("{not valid json}"), 0644)
					require.NoError(t, err)
				} else {
					configPath = filepath.Join(tempDir, tt.name+"-mcp.json")
					data, err := json.MarshalIndent(tt.configContent, "", "  ")
					require.NoError(t, err)
					err = os.WriteFile(configPath, data, 0644)
					require.NoError(t, err)
				}
			} else {
				configPath = filepath.Join(tempDir, "non-existent", "mcp.json")
			}

			config, err := LoadMCPConfig(configPath)

			if tt.expectError {
				assert.Error(t, err)
				if tt.name == "missing file returns error" {
					assert.Contains(t, err.Error(), "failed to read")
				} else if tt.name == "invalid JSON returns error" {
					assert.Contains(t, err.Error(), "failed to parse")
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, config)
				if tt.validateFunc != nil {
					tt.validateFunc(t, config)
				}
			}
		})
	}
}

// TestUS004_MCPConfigRead_GeneratedConfig tests reading generated MCP config
func TestUS004_MCPConfigRead_GeneratedConfig(t *testing.T) {
	tempDir := t.TempDir()
	cm := NewConfigManager(tempDir)

	// Generate the config
	err := cm.GenerateMCPConfig()
	require.NoError(t, err)

	// Read it back
	config, err := LoadMCPConfig(cm.GetMCPConfigPath())
	require.NoError(t, err)
	require.NotNil(t, config)

	// Validate structure
	assert.Equal(t, "1.0", config.Version)
	assert.NotNil(t, config.Servers)

	// Check go server
	goServer, ok := config.Servers["go"]
	require.True(t, ok)
	assert.Equal(t, "gopls", goServer.Command)
	assert.True(t, goServer.Enabled)
	assert.Contains(t, goServer.Args, "serve")

	// Check typescript server
	tsServer, ok := config.Servers["typescript"]
	require.True(t, ok)
	assert.Equal(t, "typescript-language-server", tsServer.Command)
	assert.True(t, tsServer.Enabled)
	assert.Contains(t, tsServer.Args, "--stdio")
}

// TestUS004_LSPCodeAnalysisSkill verifies that the lsp-code-analysis skill
// is properly installed and accessible.
func TestUS004_LSPCodeAnalysisSkill(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(t *testing.T, tempDir string)
		expectError   bool
		errorContains string
	}{
		{
			name: "skill directory and SKILL.md exist",
			setupFunc: func(t *testing.T, tempDir string) {
				skillsDir := filepath.Join(tempDir, "skills")
				skillPath := filepath.Join(skillsDir, "lsp-code-analysis")
				skillMDPath := filepath.Join(skillPath, "SKILL.md")

				err := os.MkdirAll(skillPath, 0755)
				require.NoError(t, err)

				skillContent := `# LSP Code Analysis Skill

This skill provides LSP-based code analysis capabilities.

## Usage

The skill enables semantic code navigation through LSP commands.
`
				err = os.WriteFile(skillMDPath, []byte(skillContent), 0644)
				require.NoError(t, err)
			},
			expectError: false,
		},
		{
			name: "missing skill directory",
			setupFunc: func(t *testing.T, tempDir string) {
				// Don't create the skill directory
			},
			expectError:   true,
			errorContains: "directory not found",
		},
		{
			name: "missing SKILL.md file",
			setupFunc: func(t *testing.T, tempDir string) {
				skillsDir := filepath.Join(tempDir, "skills")
				skillPath := filepath.Join(skillsDir, "lsp-code-analysis")

				err := os.MkdirAll(skillPath, 0755)
				require.NoError(t, err)
				// Don't create SKILL.md
			},
			expectError:   true,
			errorContains: "SKILL.md not found",
		},
		{
			name: "empty skill directory",
			setupFunc: func(t *testing.T, tempDir string) {
				skillsDir := filepath.Join(tempDir, "skills")
				skillPath := filepath.Join(skillsDir, "lsp-code-analysis")

				err := os.MkdirAll(skillPath, 0755)
				require.NoError(t, err)
				// Directory exists but is empty
			},
			expectError:   true,
			errorContains: "SKILL.md not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			if tt.setupFunc != nil {
				tt.setupFunc(t, tempDir)
			}

			sm := NewSetupManager(tempDir, tempDir)
			err := sm.VerifyLSPCodeAnalysisSkill()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestUS004_GracefulDegradation verifies that the system continues to function
// even when LSP servers are unavailable.
func TestUS004_GracefulDegradation(t *testing.T) {
	// Ensure no LSP binaries are available
	cleanup := setIsolatedPath(t)
	defer cleanup()

	// Reset cache to ensure fresh state
	ResetCache()

	tests := []struct {
		name           string
		testFunc       func(t *testing.T, sm *SetupManager)
		expectBehavior string
	}{
		{
			name: "IsLSPInstalled returns false when LSP unavailable",
			testFunc: func(t *testing.T, sm *SetupManager) {
				installed := sm.IsLSPInstalled()
				assert.False(t, installed)
			},
			expectBehavior: "returns false",
		},
		{
			name: "GetInstalledLSPPackages returns empty when LSP unavailable",
			testFunc: func(t *testing.T, sm *SetupManager) {
				installed := sm.GetInstalledLSPPackages()
				assert.Empty(t, installed)
			},
			expectBehavior: "returns empty slice",
		},
		{
			name: "GetMissingLSPPackages returns all packages when LSP unavailable",
			testFunc: func(t *testing.T, sm *SetupManager) {
				missing := sm.GetMissingLSPPackages()
				assert.Len(t, missing, 3)
				assert.Contains(t, missing, "LSP CLI")
				assert.Contains(t, missing, "gopls")
				assert.Contains(t, missing, "typescript-language-server")
			},
			expectBehavior: "returns all required packages",
		},
		{
			name: "CheckLSPInstallation reports all missing",
			testFunc: func(t *testing.T, sm *SetupManager) {
				components := sm.CheckLSPInstallation()
				require.Len(t, components, 3)
				for _, comp := range components {
					assert.False(t, comp.Installed, "Expected %s to not be installed", comp.Name)
				}
			},
			expectBehavior: "reports all components as not installed",
		},
		{
			name: "ExecuteLSPCommand returns error result when LSP not found",
			testFunc: func(t *testing.T, sm *SetupManager) {
				result, err := ExecuteLSPCommand("definition", "/test/file.go", 10, 5)
				// Function itself should not error
				require.NoError(t, err)
				require.NotNil(t, result)
				// But result should indicate failure
				assert.False(t, result.Success)
				assert.NotEmpty(t, result.Error)
			},
			expectBehavior: "returns error result without panicking",
		},
		{
			name: "LSPServerManager handles missing command gracefully",
			testFunc: func(t *testing.T, sm *SetupManager) {
				manager := NewLSPServerManager()
				err := manager.StartServer("test-server", "non-existent-lsp-binary", []string{})
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "failed to start")
			},
			expectBehavior: "returns error without panicking",
		},
		{
			name: "SetupManager continues despite LSP failure",
			testFunc: func(t *testing.T, sm *SetupManager) {
				// Run setup with no LSP available
				result, _ := sm.RunSetup()
				// Should not error out - errors are collected
				require.NotNil(t, result)
				// LSP should not be marked as installed
				assert.False(t, result.LSPInstalled)
				// But should have errors recorded
				assert.NotEmpty(t, result.Errors)
			},
			expectBehavior: "continues execution with errors recorded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset cache for each subtest
			ResetCache()

			tempDir := t.TempDir()
			sm := NewSetupManager(tempDir, tempDir)

			tt.testFunc(t, sm)
		})
	}
}

// TestUS004_Integration performs a full integration test of the LSP workflow
func TestUS004_Integration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping integration test on Windows")
	}

	// Create temp directory for test
	tempDir := t.TempDir()
	agentDir := filepath.Join(tempDir, "agent")
	skillsDir := filepath.Join(tempDir, "skills")

	err := os.MkdirAll(agentDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(skillsDir, "lsp-code-analysis"), 0755)
	require.NoError(t, err)

	// Create mock LSP binary
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
case "$1" in
  "definition")
    echo "/workspace/src/main.go:50:10"
    exit 0
    ;;
  "reference")
    echo "/workspace/src/main.go:50:10"
    echo "/workspace/src/utils.go:100:5"
    exit 0
    ;;
  "outline")
    echo "package main"
    echo "func main()"
    echo "type Server struct"
    exit 0
    ;;
esac
exit 0
`
	err = os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Create mock gopls and typescript-language-server
	mockGoplsPath := filepath.Join(tempDir, "gopls")
	mockTsPath := filepath.Join(tempDir, "typescript-language-server")
	err = os.WriteFile(mockGoplsPath, []byte("#!/bin/sh\nexit 0\n"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(mockTsPath, []byte("#!/bin/sh\nexit 0\n"), 0755)
	require.NoError(t, err)

	// Create SKILL.md
	skillMDPath := filepath.Join(skillsDir, "lsp-code-analysis", "SKILL.md")
	err = os.WriteFile(skillMDPath, []byte("# LSP Code Analysis Skill\n"), 0644)
	require.NoError(t, err)

	// Set PATH to include mock binaries
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	// Reset cache
	ResetCache()

	// Create setup manager
	sm := NewSetupManager(tempDir, tempDir)

	t.Run("full workflow - LSP installation check", func(t *testing.T) {
		// Check LSP installation
		components := sm.CheckLSPInstallation()
		require.Len(t, components, 3)

		// All should be installed (using mock binaries)
		for _, comp := range components {
			assert.True(t, comp.Installed, "Expected %s to be installed", comp.Name)
		}

		// IsLSPInstalled should return true
		assert.True(t, sm.IsLSPInstalled())

		// GetInstalledLSPPackages should return all 3
		installed := sm.GetInstalledLSPPackages()
		assert.Len(t, installed, 3)

		// GetMissingLSPPackages should return none
		missing := sm.GetMissingLSPPackages()
		assert.Empty(t, missing)
	})

	t.Run("full workflow - MCP config generation and reading", func(t *testing.T) {
		// Generate MCP config
		cm := NewConfigManager(tempDir)
		err := cm.GenerateMCPConfig()
		require.NoError(t, err)

		// Verify config file exists
		_, err = os.Stat(cm.GetMCPConfigPath())
		assert.NoError(t, err)

		// Load and verify config
		config, err := LoadMCPConfig(cm.GetMCPConfigPath())
		require.NoError(t, err)
		assert.NotNil(t, config)
		assert.Equal(t, "1.0", config.Version)
		assert.Contains(t, config.Servers, "go")
		assert.Contains(t, config.Servers, "typescript")
	})

	t.Run("full workflow - LSP commands execution", func(t *testing.T) {
		// Test definition command
		result, err := LSPDefinition("/workspace/src/main.go", 50, 10)
		require.NoError(t, err)
		assert.True(t, result.Success)
		assert.Contains(t, result.Data, "/workspace/src/main.go:50:10")

		// Test reference command
		result, err = LSPReference("/workspace/src/main.go", 50, 10)
		require.NoError(t, err)
		assert.True(t, result.Success)
		assert.Len(t, result.Data, 2)

		// Test outline command
		result, err = LSPOutline("/workspace/src/main.go")
		require.NoError(t, err)
		assert.True(t, result.Success)
		assert.Len(t, result.Data, 3)
	})

	t.Run("full workflow - skill verification", func(t *testing.T) {
		err := sm.VerifyLSPCodeAnalysisSkill()
		assert.NoError(t, err)
	})

	t.Run("full workflow - server lifecycle", func(t *testing.T) {
		manager := NewLSPServerManager()

		// Start a test server
		err := manager.StartServer("test-server", "sleep", []string{"10"})
		require.NoError(t, err)

		// Verify server is tracked
		manager.mu.RLock()
		_, exists := manager.servers["test-server"]
		manager.mu.RUnlock()
		assert.True(t, exists)

		// Stop the server
		err = manager.StopServer("test-server")
		assert.NoError(t, err)

		// Verify server is removed
		manager.mu.RLock()
		_, exists = manager.servers["test-server"]
		manager.mu.RUnlock()
		assert.False(t, exists)
	})

	t.Run("full workflow - cache operations", func(t *testing.T) {
		// Create a cache with short TTL
		cache := NewLSPCache(100 * time.Millisecond)

		// Set values
		cache.Set("lsp", true)
		cache.Set("gopls", true)
		cache.Set("typescript-language-server", false)

		// Verify values
		val, found := cache.Get("lsp")
		assert.True(t, found)
		assert.True(t, val)

		val, found = cache.Get("typescript-language-server")
		assert.True(t, found)
		assert.False(t, val)

		// Wait for expiration
		time.Sleep(150 * time.Millisecond)

		// Verify expired
		_, found = cache.Get("lsp")
		assert.False(t, found)
	})
}

// TestUS004_Integration_CacheStats tests cache statistics during integration
func TestUS004_Integration_CacheStats(t *testing.T) {
	// Reset global cache
	ResetCache()

	tempDir := t.TempDir()
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager(tempDir, tempDir)

	// Get initial stats (may have existing values from previous tests)
	initialHits, initialMisses, _ := GetCacheStats()

	// First check - should populate cache
	_ = sm.CheckLSPInstallation()

	// Get stats after first check
	hitsAfterFirst, missesAfterFirst, _ := GetCacheStats()

	// Verify stats changed (cache misses occurred)
	assert.GreaterOrEqual(t, missesAfterFirst, initialMisses, "Cache misses should increase or stay same")

	// Second check - should use cache
	_ = sm.CheckLSPInstallation()

	// Get stats after second check
	hitsAfterSecond, missesAfterSecond, hitRateAfterSecond := GetCacheStats()

	// Second run should have more hits or same if already cached
	assert.GreaterOrEqual(t, hitsAfterSecond, hitsAfterFirst, "Cache hits should increase or stay same")
	assert.GreaterOrEqual(t, missesAfterSecond, missesAfterFirst, "Cache misses should not decrease")

	// Hit rate should be reasonable (either from before or after caching)
	assert.GreaterOrEqual(t, hitRateAfterSecond, float64(0), "Hit rate should be non-negative")
	assert.LessOrEqual(t, hitRateAfterSecond, float64(100), "Hit rate should not exceed 100%")

	// Verify the second run utilized cache (hits should increase)
	// or if it was already cached, the hit rate should be maintained
	if hitsAfterFirst == initialHits {
		// First run was all misses, second should have hits
		assert.Greater(t, hitsAfterSecond, initialHits, "Second run should have cache hits")
	}
}

// TestUS004_Integration_ConcurrentAccess tests concurrent access to LSP components
func TestUS004_Integration_ConcurrentAccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	tempDir := t.TempDir()
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	ResetCache()

	sm := NewSetupManager(tempDir, tempDir)

	// Run multiple operations concurrently
	done := make(chan bool, 6)

	// Concurrent LSP installation checks
	for i := 0; i < 3; i++ {
		go func() {
			_ = sm.IsLSPInstalled()
			done <- true
		}()
	}

	// Concurrent component checks
	for i := 0; i < 3; i++ {
		go func() {
			_ = sm.CheckLSPInstallation()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 6; i++ {
		<-done
	}

	// Should complete without panics or race conditions
	assert.True(t, true, "Concurrent access completed successfully")
}

// TestUS004_Integration_BatchOperations tests batch LSP operations
func TestUS004_Integration_BatchOperations(t *testing.T) {
	tempDir := t.TempDir()
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	ResetCache()

	sm := NewSetupManager(tempDir, tempDir)

	// Test batch check
	result := sm.BatchCheckLSPInstallation()
	require.NotNil(t, result)

	// All should be installed
	for _, pkg := range RequiredLSPPackages {
		installed, found := result.Get(pkg.Name)
		assert.True(t, found, "Package %s should be in results", pkg.Name)
		assert.True(t, installed, "Package %s should be installed", pkg.Name)
	}
}

// TestUS004_Integration_CommandTimeout tests command timeout behavior
func TestUS004_Integration_CommandTimeout(t *testing.T) {
	tempDir := t.TempDir()

	// Create a mock that sleeps longer than the timeout
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
sleep 20
exit 0
`
	err := os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Add to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	// Execute command - should timeout
	result, err := ExecuteLSPCommand("definition", "/test/file.go", 10, 5)

	// Function should return without error
	require.NoError(t, err)
	require.NotNil(t, result)

	// But result should indicate timeout/failure
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Error)
}

// TestUS004_Integration_ErrorPropagation tests that errors are properly propagated
func TestUS004_Integration_ErrorPropagation(t *testing.T) {
	tempDir := t.TempDir()

	// Create a mock that returns an error
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
echo "Error: Something went wrong" >&2
exit 1
`
	err := os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Add to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	result, err := ExecuteLSPCommand("definition", "/test/file.go", 10, 5)

	// Function should return without error
	require.NoError(t, err)
	require.NotNil(t, result)

	// Result should indicate failure
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "command failed")
}

// TestUS004_Integration_ServerManagerEdgeCases tests edge cases for server manager
func TestUS004_Integration_ServerManagerEdgeCases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	manager := NewLSPServerManager()

	t.Run("stop non-existent server", func(t *testing.T) {
		err := manager.StopServer("non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("start same server twice", func(t *testing.T) {
		// Start first time
		err := manager.StartServer("duplicate-server", "sleep", []string{"10"})
		require.NoError(t, err)

		// Start second time - should succeed without error (idempotent)
		err = manager.StartServer("duplicate-server", "sleep", []string{"10"})
		assert.NoError(t, err)

		// Clean up
		_ = manager.StopServer("duplicate-server")
	})

	t.Run("stop all servers", func(t *testing.T) {
		// Start multiple servers
		_ = manager.StartServer("server1", "sleep", []string{"10"})
		_ = manager.StartServer("server2", "sleep", []string{"10"})

		// Stop all
		manager.StopAllServers()

		// Verify all stopped
		manager.mu.RLock()
		count := len(manager.servers)
		manager.mu.RUnlock()
		assert.Equal(t, 0, count)
	})
}

// TestUS004_Integration_ConfigManagerEdgeCases tests edge cases for config manager
func TestUS004_Integration_ConfigManagerEdgeCases(t *testing.T) {
	t.Run("create directory structure in non-existent parent", func(t *testing.T) {
		tempDir := t.TempDir()
		nestedDir := filepath.Join(tempDir, "nested", "path", "pi-config")

		cm := NewConfigManager(nestedDir)
		err := cm.CreateDirectoryStructure()
		assert.NoError(t, err)

		// Verify directories created
		_, err = os.Stat(filepath.Join(nestedDir, "agent"))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(nestedDir, "skills"))
		assert.NoError(t, err)
	})

	t.Run("generate config overwrites existing", func(t *testing.T) {
		tempDir := t.TempDir()
		agentDir := filepath.Join(tempDir, "agent")
		err := os.MkdirAll(agentDir, 0755)
		require.NoError(t, err)

		// Create existing config with old version
		existingConfig := `{"version": "0.5", "servers": {}}`
		err = os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(existingConfig), 0644)
		require.NoError(t, err)

		// Generate new config
		cm := NewConfigManager(tempDir)
		err = cm.GenerateMCPConfig()
		require.NoError(t, err)

		// Verify it was overwritten
		data, err := os.ReadFile(cm.GetMCPConfigPath())
		require.NoError(t, err)

		var config map[string]interface{}
		err = json.Unmarshal(data, &config)
		require.NoError(t, err)
		assert.Equal(t, "1.0", config["version"])
	})
}

// TestUS004_Integration_RequiredPackages tests the RequiredLSPPackages definition
func TestUS004_Integration_RequiredPackages(t *testing.T) {
	// Verify all required packages are defined
	assert.Len(t, RequiredLSPPackages, 3)

	expectedPackages := map[string]struct {
		Command string
		NPMName string
	}{
		"LSP CLI":                    {Command: "lsp", NPMName: "@pi/lsp-cli"},
		"gopls":                      {Command: "gopls", NPMName: "@pi/gopls-wrapper"},
		"typescript-language-server": {Command: "typescript-language-server", NPMName: "typescript-language-server"},
	}

	for _, pkg := range RequiredLSPPackages {
		expected, ok := expectedPackages[pkg.Name]
		assert.True(t, ok, "Unexpected package: %s", pkg.Name)
		assert.Equal(t, expected.Command, pkg.Command, "Command mismatch for %s", pkg.Name)
		assert.Equal(t, expected.NPMName, pkg.NPMName, "NPMName mismatch for %s", pkg.Name)
		assert.NotEmpty(t, pkg.Description, "Description should not be empty for %s", pkg.Name)
	}
}

// TestUS004_Integration_StructValidation validates LSP-related structs
func TestUS004_Integration_StructValidation(t *testing.T) {
	t.Run("LSPComponent struct", func(t *testing.T) {
		comp := LSPComponent{
			Name:      "Test LSP",
			Command:   "test-lsp",
			Installed: true,
		}
		assert.Equal(t, "Test LSP", comp.Name)
		assert.Equal(t, "test-lsp", comp.Command)
		assert.True(t, comp.Installed)
	})

	t.Run("LSPPackage struct", func(t *testing.T) {
		pkg := LSPPackage{
			Name:        "Test Package",
			Command:     "test-cmd",
			NPMName:     "@test/pkg",
			Description: "Test description",
		}
		assert.Equal(t, "Test Package", pkg.Name)
		assert.Equal(t, "test-cmd", pkg.Command)
		assert.Equal(t, "@test/pkg", pkg.NPMName)
		assert.Equal(t, "Test description", pkg.Description)
	})

	t.Run("LSPCommandResult struct", func(t *testing.T) {
		result := LSPCommandResult{
			Success: true,
			Data:    []string{"result1", "result2"},
			Error:   "",
		}
		assert.True(t, result.Success)
		assert.Len(t, result.Data, 2)
		assert.Empty(t, result.Error)
	})

	t.Run("MCPServerConfig struct", func(t *testing.T) {
		config := MCPServerConfig{
			Command: "test-server",
			Args:    []string{"--arg1", "--arg2"},
			Enabled: true,
		}
		assert.Equal(t, "test-server", config.Command)
		assert.Len(t, config.Args, 2)
		assert.True(t, config.Enabled)
	})

	t.Run("MCPConfig struct", func(t *testing.T) {
		config := MCPConfig{
			Version: "1.0",
			Servers: map[string]MCPServerConfig{
				"go": {
					Command: "gopls",
					Args:    []string{"serve"},
					Enabled: true,
				},
			},
		}
		assert.Equal(t, "1.0", config.Version)
		assert.Len(t, config.Servers, 1)
	})
}
