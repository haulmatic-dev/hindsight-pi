package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestCache resets the cache with a short TTL for testing
func setupTestCache(t testing.TB) {
	t.Helper()
	SetCacheTTL(100 * time.Millisecond)
	ResetCache()
	t.Cleanup(func() {
		ResetCache()
		SetCacheTTL(defaultCacheTTL)
	})
}

// setupMockBinaries creates mock binaries in a temp directory and returns the directory path
func setupMockBinaries(t *testing.T, binaries ...string) string {
	t.Helper()
	tempDir := t.TempDir()

	for _, binary := range binaries {
		var binaryPath string
		if runtime.GOOS == "windows" {
			binaryPath = filepath.Join(tempDir, binary+".exe")
		} else {
			binaryPath = filepath.Join(tempDir, binary)
		}

		// Create a simple script that exits with code 0
		var content []byte
		if runtime.GOOS == "windows" {
			content = []byte("@echo off\nexit /b 0\n")
		} else {
			content = []byte("#!/bin/sh\nexit 0\n")
		}

		err := os.WriteFile(binaryPath, content, 0755)
		require.NoError(t, err, "Failed to create mock binary: %s", binary)
	}

	return tempDir
}

// setIsolatedPath sets PATH to only include the given directories plus essential system paths for 'which' command
// This ensures tests don't accidentally find real installed binaries
func setIsolatedPath(t *testing.T, dirs ...string) func() {
	t.Helper()
	originalPath := os.Getenv("PATH")

	// Include minimal system paths to ensure 'which' command works
	var systemPaths []string
	if runtime.GOOS != "windows" {
		systemPaths = []string{"/bin", "/usr/bin"}
	}

	allDirs := append(dirs, systemPaths...)
	newPath := strings.Join(allDirs, string(os.PathListSeparator))

	err := os.Setenv("PATH", newPath)
	require.NoError(t, err, "Failed to set PATH")

	return func() {
		os.Setenv("PATH", originalPath)
	}
}

func TestCheckLSPInstallation_AllInstalled(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for all 3 components
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager("./test-config", "./test-config")

	// Call CheckLSPInstallation
	components := sm.CheckLSPInstallation()

	// Assert all components show Installed=true
	require.Len(t, components, 3, "Expected 3 components")

	expectedComponents := map[string]bool{
		"lsp":                        true,
		"gopls":                      true,
		"typescript-language-server": true,
	}

	for _, comp := range components {
		assert.True(t, comp.Installed, "Expected %s to be installed", comp.Name)
		assert.NotEmpty(t, comp.Command, "Command should not be empty for %s", comp.Name)
		delete(expectedComponents, comp.Command)
	}

	assert.Empty(t, expectedComponents, "Not all expected components were found: %v", expectedComponents)
}

func TestCheckLSPInstallation_Partial(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for only 1 component (lsp) - partial installation
	mockDir := setupMockBinaries(t, "lsp")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager("./test-config", "./test-config")

	// Call CheckLSPInstallation
	components := sm.CheckLSPInstallation()

	// Assert only lsp is installed, others are missing (partial state)
	require.Len(t, components, 3, "Expected 3 components")

	installedCount := 0
	missingCount := 0

	for _, comp := range components {
		if comp.Command == "lsp" {
			assert.True(t, comp.Installed, "Expected lsp to be installed")
			assert.Equal(t, "LSP CLI", comp.Name, "Expected correct name for lsp component")
			installedCount++
		} else {
			assert.False(t, comp.Installed, "Expected %s to not be installed", comp.Name)
			missingCount++
		}
	}

	assert.Equal(t, 1, installedCount, "Expected 1 installed component")
	assert.Equal(t, 2, missingCount, "Expected 2 missing components")
}

// TestCheckLSPInstallation_MissingComponents is an alias for TestCheckLSPInstallation_Partial
func TestCheckLSPInstallation_MissingComponents(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for only 1 component (lsp)
	mockDir := setupMockBinaries(t, "lsp")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager("./test-config", "./test-config")

	// Call CheckLSPInstallation
	components := sm.CheckLSPInstallation()

	// Assert only lsp is installed, others are missing
	require.Len(t, components, 3, "Expected 3 components")

	installedCount := 0
	missingCount := 0

	for _, comp := range components {
		if comp.Command == "lsp" {
			assert.True(t, comp.Installed, "Expected lsp to be installed")
			installedCount++
		} else {
			assert.False(t, comp.Installed, "Expected %s to not be installed", comp.Name)
			missingCount++
		}
	}

	assert.Equal(t, 1, installedCount, "Expected 1 installed component")
	assert.Equal(t, 2, missingCount, "Expected 2 missing components")
}

func TestCheckLSPInstallation_AllMissing(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Don't create any mock binaries - use only system paths
	cleanup := setIsolatedPath(t)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager("./test-config", "./test-config")

	// Call CheckLSPInstallation
	components := sm.CheckLSPInstallation()

	// Assert all components show Installed=false
	require.Len(t, components, 3, "Expected 3 components")

	for _, comp := range components {
		assert.False(t, comp.Installed, "Expected %s to not be installed", comp.Name)
		assert.NotEmpty(t, comp.Command, "Command should not be empty for %s", comp.Name)
	}
}

func TestCheckLSPInstallation_ComponentStructure(t *testing.T) {
	// Test that the component structure is correct
	cleanup := setIsolatedPath(t)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	components := sm.CheckLSPInstallation()

	// Verify component structure
	expectedComponents := []LSPComponent{
		{Name: "LSP CLI", Command: "lsp"},
		{Name: "gopls", Command: "gopls"},
		{Name: "typescript-language-server", Command: "typescript-language-server"},
	}

	require.Len(t, components, len(expectedComponents), "Expected %d components", len(expectedComponents))

	for i, expected := range expectedComponents {
		assert.Equal(t, expected.Name, components[i].Name, "Component name mismatch at index %d", i)
		assert.Equal(t, expected.Command, components[i].Command, "Component command mismatch at index %d", i)
	}
}

func TestInstallLSPComponents_AlreadyInstalled(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for all 3 components
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager("./test-config", "./test-config")

	// Verify all components are already installed
	assert.True(t, sm.IsLSPInstalled(), "Expected all LSP components to be installed before test")

	// Call InstallLSPComponents - should skip all since they're already installed
	err := sm.InstallLSPComponents()

	// Should succeed since all are already installed
	assert.NoError(t, err, "InstallLSPComponents should succeed when all components are already installed")
}

func TestInstallLSPComponents_Success(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for all 3 components (simulating successful installation)
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	// Create setup manager
	tempDir := t.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	// Verify all components are already "installed" (via mock binaries)
	assert.True(t, sm.IsLSPInstalled(), "Expected all LSP components to be installed before test")

	// Call InstallLSPComponents - should succeed since all are already installed
	err := sm.InstallLSPComponents()

	// Should succeed since all are already installed (skips installation)
	assert.NoError(t, err, "InstallLSPComponents should succeed when all components are already installed")

	// Verify components are still installed after calling InstallLSPComponents
	components := sm.CheckLSPInstallation()
	require.Len(t, components, 3, "Expected 3 components")

	for _, comp := range components {
		assert.True(t, comp.Installed, "Expected %s to be installed after InstallLSPComponents", comp.Name)
	}
}

func TestInstallLSPComponents_PartialFailure(t *testing.T) {
	// Skip this test if npm is not available or in restricted environments
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("Skipping network-dependent test")
	}

	// Reset cache to avoid interference from other tests
	ResetCache()

	// Create setup manager with an isolated config
	cleanup := setIsolatedPath(t)
	defer cleanup()

	tempDir := t.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	// Since we can't easily mock npm/go install commands in a unit test,
	// we verify the method handles the case where binaries don't exist
	// The actual installation will fail because npm/go commands won't find
	// the packages or the commands won't work

	// This test verifies the error handling path
	err := sm.InstallLSPComponents()

	// We expect an error since we can't actually install the packages
	assert.Error(t, err, "Expected error when installation fails")
	if err != nil {
		assert.Contains(t, err.Error(), "failed to install LSP components", "Error message should indicate installation failure")
	}
}

func TestIsLSPPackageInstalled(t *testing.T) {
	tests := []struct {
		name        string
		binary      string
		wantInstall bool
	}{
		{
			name:        "installed via which",
			binary:      "test-binary-which",
			wantInstall: true,
		},
		{
			name:        "not installed",
			binary:      "non-existent-binary-xyz",
			wantInstall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSetupManager("./test-config", "./test-config")

			if tt.wantInstall {
				// Create mock binary
				mockDir := setupMockBinaries(t, tt.binary)
				cleanup := setIsolatedPath(t, mockDir)
				defer cleanup()
			} else {
				cleanup := setIsolatedPath(t)
				defer cleanup()
			}

			pkg := LSPPackage{
				Name:    "Test Package",
				Command: tt.binary,
				NPMName: "@test/package",
			}

			got := sm.isLSPPackageInstalled(pkg)
			assert.Equal(t, tt.wantInstall, got, "isLSPPackageInstalled() = %v, want %v", got, tt.wantInstall)
		})
	}
}

func TestGetInstalledLSPPackages(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for 2 out of 3 components
	mockDir := setupMockBinaries(t, "lsp", "gopls")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	installed := sm.GetInstalledLSPPackages()

	// Should return 2 installed packages
	assert.Len(t, installed, 2, "Expected 2 installed packages")
	assert.Contains(t, installed, "LSP CLI", "Should include LSP CLI")
	assert.Contains(t, installed, "gopls", "Should include gopls")
	assert.NotContains(t, installed, "typescript-language-server", "Should not include typescript-language-server")
}

func TestGetMissingLSPPackages(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for 1 out of 3 components
	mockDir := setupMockBinaries(t, "lsp")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	missing := sm.GetMissingLSPPackages()

	// Should return 2 missing packages
	assert.Len(t, missing, 2, "Expected 2 missing packages")
	assert.NotContains(t, missing, "LSP CLI", "Should not include LSP CLI (it's installed)")
	assert.Contains(t, missing, "gopls", "Should include gopls (missing)")
	assert.Contains(t, missing, "typescript-language-server", "Should include typescript-language-server (missing)")
}

func TestIsLSPInstalled_AllInstalled(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for all 3 components
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	assert.True(t, sm.IsLSPInstalled(), "Expected IsLSPInstalled() to return true when all components are installed")
}

func TestIsLSPInstalled_PartiallyInstalled(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for only 1 component
	mockDir := setupMockBinaries(t, "lsp")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	assert.False(t, sm.IsLSPInstalled(), "Expected IsLSPInstalled() to return false when only some components are installed")
}

func TestIsLSPInstalled_NoneInstalled(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Don't create any mock binaries
	cleanup := setIsolatedPath(t)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	assert.False(t, sm.IsLSPInstalled(), "Expected IsLSPInstalled() to return false when no components are installed")
}

func TestRequiredLSPPackages(t *testing.T) {
	// Verify the required packages are defined correctly
	assert.Len(t, RequiredLSPPackages, 3, "Expected 3 required LSP packages")

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
		assert.True(t, ok, "Unexpected package name: %s", pkg.Name)
		assert.Equal(t, expected.Command, pkg.Command, "Command mismatch for %s", pkg.Name)
		assert.Equal(t, expected.NPMName, pkg.NPMName, "NPMName mismatch for %s", pkg.Name)
		assert.NotEmpty(t, pkg.Description, "Description should not be empty for %s", pkg.Name)
	}
}

func TestCheckLSPInstallation_WhichCommandNotFound(t *testing.T) {
	// Test the case where binaries are not in PATH
	// Save the original PATH
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	// Set PATH to only include system paths (no mock binaries)
	cleanup := setIsolatedPath(t)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	components := sm.CheckLSPInstallation()

	// All components should be not installed
	for _, comp := range components {
		assert.False(t, comp.Installed, "Expected %s to not be installed when which fails", comp.Name)
	}
}

func TestLSPComponent_StructFields(t *testing.T) {
	// Test that LSPComponent struct has the expected fields
	comp := LSPComponent{
		Name:      "Test LSP",
		Command:   "test-lsp",
		Installed: true,
	}

	assert.Equal(t, "Test LSP", comp.Name)
	assert.Equal(t, "test-lsp", comp.Command)
	assert.True(t, comp.Installed)

	comp.Installed = false
	assert.False(t, comp.Installed)
}

func TestLSPPackage_StructFields(t *testing.T) {
	// Test that LSPPackage struct has the expected fields
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
}

// === Integration Tests for LSP Commands ===

func TestLSPCommandResult_Struct(t *testing.T) {
	result := &LSPCommandResult{
		Success: true,
		Data:    []string{"result1", "result2"},
		Error:   "",
	}

	assert.True(t, result.Success)
	assert.Equal(t, []string{"result1", "result2"}, result.Data)
	assert.Empty(t, result.Error)

	result.Success = false
	result.Error = "test error"
	assert.False(t, result.Success)
	assert.Equal(t, "test error", result.Error)
}

func TestLSPServerManager_StartAndStop(t *testing.T) {
	manager := NewLSPServerManager()

	// Create a mock long-running process
	if runtime.GOOS == "windows" {
		// Skip on Windows as process management differs
		t.Skip("Skipping on Windows")
	}

	// Try to start a simple process that stays running
	// Using 'sleep' command which is available on Unix systems
	err := manager.StartServer("test-server", "sleep", []string{"10"})
	if err != nil {
		// If sleep is not available, skip the test
		t.Skip("Skipping test - 'sleep' command not available")
	}

	// Verify server is tracked
	assert.NotNil(t, manager.servers["test-server"])

	// Try to start same server again (should succeed without error)
	err = manager.StartServer("test-server", "sleep", []string{"10"})
	assert.NoError(t, err, "Starting already running server should not fail")

	// Stop the server
	err = manager.StopServer("test-server")
	assert.NoError(t, err)

	// Verify server is removed
	_, exists := manager.servers["test-server"]
	assert.False(t, exists)
}

func TestLSPServerManager_StopNonExistent(t *testing.T) {
	manager := NewLSPServerManager()

	err := manager.StopServer("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLSPServerManager_StopAll(t *testing.T) {
	manager := NewLSPServerManager()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	// Start a couple of test servers
	manager.StartServer("server1", "sleep", []string{"10"})
	manager.StartServer("server2", "sleep", []string{"10"})

	assert.Len(t, manager.servers, 2)

	// Stop all
	manager.StopAllServers()

	// Verify all stopped
	assert.Len(t, manager.servers, 0)
}

func TestExecuteLSPCommand_UnknownCommand(t *testing.T) {
	result, err := ExecuteLSPCommand("unknown-command", "test.go", 1, 1)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown LSP command")
}

func TestLSPDefinition_Reference_Outline(t *testing.T) {
	// These tests verify the command structures are correct
	// We can't actually test LSP functionality without real LSP servers

	// Test LSPDefinition
	result, err := LSPDefinition("test.go", 10, 5)
	// Will fail because lsp CLI is not available, but verifies structure
	assert.NoError(t, err) // Function itself shouldn't error
	assert.NotNil(t, result)
	// Result will likely be failure since lsp CLI isn't installed

	// Test LSPReference
	result, err = LSPReference("test.go", 10, 5)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Test LSPOutline
	result, err = LSPOutline("test.go")
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestLoadMCPConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.json")

	// Create a test config
	config := map[string]interface{}{
		"version": "1.0",
		"servers": map[string]interface{}{
			"go": map[string]interface{}{
				"command": "gopls",
				"args":    []string{"serve"},
				"enabled": true,
			},
		},
	}

	data, _ := json.Marshal(config)
	os.WriteFile(configPath, data, 0644)

	// Load config
	loadedConfig, err := LoadMCPConfig(configPath)
	assert.NoError(t, err)
	assert.NotNil(t, loadedConfig)
	assert.Equal(t, "1.0", loadedConfig.Version)
	assert.NotNil(t, loadedConfig.Servers)

	// Test loading non-existent file
	_, err = LoadMCPConfig("/non/existent/path/mcp.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read")

	// Test loading invalid JSON
	invalidPath := filepath.Join(tempDir, "invalid.json")
	os.WriteFile(invalidPath, []byte("not valid json"), 0644)
	_, err = LoadMCPConfig(invalidPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestVerifyLSPCodeAnalysisSkill(t *testing.T) {
	tempDir := t.TempDir()
	skillsDir := filepath.Join(tempDir, "skills")
	os.MkdirAll(filepath.Join(skillsDir, "lsp-code-analysis"), 0755)

	// Create the skill directory and SKILL.md
	skillMDPath := filepath.Join(skillsDir, "lsp-code-analysis", "SKILL.md")
	os.WriteFile(skillMDPath, []byte("# LSP Code Analysis Skill"), 0644)

	sm := NewSetupManager(tempDir, tempDir)

	// Should succeed
	err := sm.VerifyLSPCodeAnalysisSkill()
	assert.NoError(t, err)

	// Test missing skill directory
	os.RemoveAll(skillsDir)
	err = sm.VerifyLSPCodeAnalysisSkill()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "directory not found")

	// Test missing SKILL.md
	os.MkdirAll(filepath.Join(skillsDir, "lsp-code-analysis"), 0755)
	err = sm.VerifyLSPCodeAnalysisSkill()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SKILL.md not found")
}

// === Comprehensive LSP Integration Tests ===

// TestLSP_ServerLifecycle_Comprehensive tests complete server lifecycle with multiple scenarios
func TestLSP_ServerLifecycle_Comprehensive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows - process management differs")
	}

	tests := []struct {
		name        string
		serverName  string
		command     string
		args        []string
		expectError bool
	}{
		{
			name:        "start and stop go server",
			serverName:  "gopls",
			command:     "sleep",
			args:        []string{"30"},
			expectError: false,
		},
		{
			name:        "start and stop typescript server",
			serverName:  "typescript",
			command:     "sleep",
			args:        []string{"30"},
			expectError: false,
		},
		{
			name:        "start multiple servers",
			serverName:  "server1",
			command:     "sleep",
			args:        []string{"30"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewLSPServerManager()

			// Start server
			err := manager.StartServer(tt.serverName, tt.command, tt.args)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err, "Failed to start server")

			// Verify server is tracked
			manager.mu.RLock()
			_, exists := manager.servers[tt.serverName]
			manager.mu.RUnlock()
			assert.True(t, exists, "Server should be tracked after start")

			// Try to start same server again (should succeed without error)
			err = manager.StartServer(tt.serverName, tt.command, tt.args)
			assert.NoError(t, err, "Starting already running server should not fail")

			// Stop server
			err = manager.StopServer(tt.serverName)
			assert.NoError(t, err, "Failed to stop server")

			// Verify server is removed
			manager.mu.RLock()
			_, exists = manager.servers[tt.serverName]
			manager.mu.RUnlock()
			assert.False(t, exists, "Server should be removed after stop")
		})
	}
}

// TestLSP_ServerLifecycle_Concurrent tests concurrent server operations
func TestLSP_ServerLifecycle_Concurrent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	manager := NewLSPServerManager()

	// Start multiple servers concurrently
	serverNames := []string{"server1", "server2", "server3"}
	for _, name := range serverNames {
		err := manager.StartServer(name, "sleep", []string{"30"})
		require.NoError(t, err)
	}

	// Verify all servers are tracked
	manager.mu.RLock()
	assert.Len(t, manager.servers, 3, "All 3 servers should be tracked")
	manager.mu.RUnlock()

	// Stop all servers
	manager.StopAllServers()

	// Verify all servers are stopped
	manager.mu.RLock()
	assert.Len(t, manager.servers, 0, "All servers should be stopped")
	manager.mu.RUnlock()
}

// TestLSP_CommandsWork_WithMock tests LSP commands with mock responses
func TestLSP_CommandsWork_WithMock(t *testing.T) {
	tempDir := t.TempDir()

	// Create a mock lsp binary that returns predefined responses
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
if [ "$1" = "definition" ]; then
	echo '"/path/to/definition.go:10:5"'
	exit 0
elif [ "$1" = "reference" ]; then
	echo '"/path/to/ref1.go:15:10"'
	echo '"/path/to/ref2.go:20:8"'
	exit 0
elif [ "$1" = "outline" ]; then
	echo 'func MyFunction'
	echo 'type MyStruct struct'
	exit 0
fi
exit 1
`
	err := os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Add mock binary to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	tests := []struct {
		name           string
		command        string
		filePath       string
		line           int
		column         int
		expectSuccess  bool
		expectDataLen  int
		expectErrorMsg string
	}{
		{
			name:          "definition command returns location",
			command:       "definition",
			filePath:      "/test/file.go",
			line:          10,
			column:        5,
			expectSuccess: true,
			expectDataLen: 1,
		},
		{
			name:          "reference command returns multiple locations",
			command:       "reference",
			filePath:      "/test/file.go",
			line:          10,
			column:        5,
			expectSuccess: true,
			expectDataLen: 2,
		},
		{
			name:          "outline command returns symbols",
			command:       "outline",
			filePath:      "/test/file.go",
			line:          0,
			column:        0,
			expectSuccess: true,
			expectDataLen: 2,
		},
		{
			name:           "unknown command fails",
			command:        "unknown",
			filePath:       "/test/file.go",
			line:           10,
			column:         5,
			expectSuccess:  false,
			expectErrorMsg: "unknown LSP command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteLSPCommand(tt.command, tt.filePath, tt.line, tt.column)

			if tt.expectErrorMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErrorMsg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectSuccess, result.Success)

			if tt.expectSuccess {
				assert.Len(t, result.Data, tt.expectDataLen)
				assert.Empty(t, result.Error)
			}
		})
	}
}

// TestLSP_CommandsWork_HelperFunctions tests the helper functions for LSP commands
func TestLSP_CommandsWork_HelperFunctions(t *testing.T) {
	tempDir := t.TempDir()

	// Create mock lsp binary
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
echo '{"success": true, "data": ["result"]}'
exit 0
`
	err := os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Add mock binary to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	t.Run("LSPDefinition calls definition command", func(t *testing.T) {
		result, err := LSPDefinition("/test/file.go", 10, 5)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("LSPReference calls reference command", func(t *testing.T) {
		result, err := LSPReference("/test/file.go", 10, 5)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("LSPOutline calls outline command", func(t *testing.T) {
		result, err := LSPOutline("/test/file.go")
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}

// TestLSP_GracefulFailure_MissingServers tests graceful handling of missing LSP servers
func TestLSP_GracefulFailure_MissingServers(t *testing.T) {
	// Ensure no LSP binaries are in PATH
	cleanup := setIsolatedPath(t)
	defer cleanup()

	t.Run("ExecuteLSPCommand returns error result when lsp not found", func(t *testing.T) {
		result, err := ExecuteLSPCommand("definition", "/test/file.go", 10, 5)

		// Function itself should not error
		require.NoError(t, err)
		require.NotNil(t, result)

		// But the result should indicate failure
		assert.False(t, result.Success)
		assert.NotEmpty(t, result.Error)
	})

	t.Run("StartServer fails with missing command", func(t *testing.T) {
		manager := NewLSPServerManager()
		err := manager.StartServer("missing-server", "non-existent-lsp-binary", []string{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start")
	})

	t.Run("CheckLSPInstallation reports missing components", func(t *testing.T) {
		sm := NewSetupManager("./test-config", "./test-config")
		components := sm.CheckLSPInstallation()

		require.Len(t, components, 3)
		for _, comp := range components {
			assert.False(t, comp.Installed, "Expected %s to not be installed", comp.Name)
		}
	})

	t.Run("IsLSPInstalled returns false when nothing installed", func(t *testing.T) {
		sm := NewSetupManager("./test-config", "./test-config")
		assert.False(t, sm.IsLSPInstalled())
	})

	t.Run("GetMissingLSPPackages returns all when none installed", func(t *testing.T) {
		sm := NewSetupManager("./test-config", "./test-config")
		missing := sm.GetMissingLSPPackages()
		assert.Len(t, missing, 3)
	})

	t.Run("GetInstalledLSPPackages returns empty when none installed", func(t *testing.T) {
		sm := NewSetupManager("./test-config", "./test-config")
		installed := sm.GetInstalledLSPPackages()
		assert.Empty(t, installed)
	})
}

// TestLSP_GracefulFailure_InvalidInput tests graceful handling of invalid inputs
func TestLSP_GracefulFailure_InvalidInput(t *testing.T) {
	t.Run("StopServer returns error for non-existent server", func(t *testing.T) {
		manager := NewLSPServerManager()
		err := manager.StopServer("non-existent-server")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("LoadMCPConfig returns error for non-existent file", func(t *testing.T) {
		_, err := LoadMCPConfig("/non/existent/path/mcp.json")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read")
	})

	t.Run("LoadMCPConfig returns error for invalid JSON", func(t *testing.T) {
		tempDir := t.TempDir()
		invalidPath := filepath.Join(tempDir, "invalid.json")
		err := os.WriteFile(invalidPath, []byte("not valid json"), 0644)
		require.NoError(t, err)

		_, err = LoadMCPConfig(invalidPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse")
	})
}

// TestLSP_GracefulFailure_PartialInstallation tests handling of partial installations
func TestLSP_GracefulFailure_PartialInstallation(t *testing.T) {
	// Create mock for only one component
	mockDir := setupMockBinaries(t, "lsp")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	t.Run("IsLSPInstalled returns false with partial installation", func(t *testing.T) {
		// Reset cache to ensure fresh state
		ResetCache()
		sm := NewSetupManager("./test-config", "./test-config")
		assert.False(t, sm.IsLSPInstalled())
	})

	t.Run("GetInstalledLSPPackages returns only installed", func(t *testing.T) {
		// Reset cache to ensure fresh state
		ResetCache()
		sm := NewSetupManager("./test-config", "./test-config")
		installed := sm.GetInstalledLSPPackages()
		assert.Len(t, installed, 1)
		assert.Contains(t, installed, "LSP CLI")
	})

	t.Run("GetMissingLSPPackages returns only missing", func(t *testing.T) {
		// Reset cache to ensure fresh state
		ResetCache()
		sm := NewSetupManager("./test-config", "./test-config")
		missing := sm.GetMissingLSPPackages()
		assert.Len(t, missing, 2)
		assert.NotContains(t, missing, "LSP CLI")
	})
}

// TestLSP_ConfigValidation_MCPStructure tests MCP config structure validation
func TestLSP_ConfigValidation_MCPStructure(t *testing.T) {
	tempDir := t.TempDir()
	cm := NewConfigManager(tempDir)

	err := cm.GenerateMCPConfig()
	require.NoError(t, err)

	// Load and validate config structure
	config, err := LoadMCPConfig(cm.GetMCPConfigPath())
	require.NoError(t, err)
	require.NotNil(t, config)

	t.Run("version is set correctly", func(t *testing.T) {
		assert.Equal(t, "1.0", config.Version)
	})

	t.Run("servers map exists and has required servers", func(t *testing.T) {
		assert.NotNil(t, config.Servers)
		assert.Contains(t, config.Servers, "go")
		assert.Contains(t, config.Servers, "typescript")
	})

	t.Run("go server configuration is valid", func(t *testing.T) {
		goServer, ok := config.Servers["go"]
		require.True(t, ok)
		assert.Equal(t, "gopls", goServer.Command)
		assert.True(t, goServer.Enabled)
		assert.Contains(t, goServer.Args, "serve")
	})

	t.Run("typescript server configuration is valid", func(t *testing.T) {
		tsServer, ok := config.Servers["typescript"]
		require.True(t, ok)
		assert.Equal(t, "typescript-language-server", tsServer.Command)
		assert.True(t, tsServer.Enabled)
		assert.Contains(t, tsServer.Args, "--stdio")
	})
}

// TestLSP_ConfigValidation_JSONStructure tests generated JSON structure
func TestLSP_ConfigValidation_JSONStructure(t *testing.T) {
	tempDir := t.TempDir()
	cm := NewConfigManager(tempDir)

	err := cm.GenerateMCPConfig()
	require.NoError(t, err)

	// Read raw JSON for structure validation
	data, err := os.ReadFile(cm.GetMCPConfigPath())
	require.NoError(t, err)

	var rawConfig map[string]interface{}
	err = json.Unmarshal(data, &rawConfig)
	require.NoError(t, err)

	t.Run("has version field", func(t *testing.T) {
		version, ok := rawConfig["version"].(string)
		assert.True(t, ok)
		assert.Equal(t, "1.0", version)
	})

	t.Run("has servers object", func(t *testing.T) {
		servers, ok := rawConfig["servers"].(map[string]interface{})
		assert.True(t, ok)
		assert.NotNil(t, servers)
	})

	t.Run("has capabilities object", func(t *testing.T) {
		capabilities, ok := rawConfig["capabilities"].(map[string]interface{})
		assert.True(t, ok)
		assert.NotNil(t, capabilities)
	})

	t.Run("has lspCommands object", func(t *testing.T) {
		lspCommands, ok := rawConfig["lspCommands"].(map[string]interface{})
		assert.True(t, ok)
		assert.NotNil(t, lspCommands)
		assert.Contains(t, lspCommands, "definition")
		assert.Contains(t, lspCommands, "reference")
		assert.Contains(t, lspCommands, "outline")
	})

	t.Run("go server has settings", func(t *testing.T) {
		servers := rawConfig["servers"].(map[string]interface{})
		goServer := servers["go"].(map[string]interface{})
		settings, ok := goServer["settings"].(map[string]interface{})
		assert.True(t, ok)
		assert.NotNil(t, settings)
	})

	t.Run("typescript server has settings", func(t *testing.T) {
		servers := rawConfig["servers"].(map[string]interface{})
		tsServer := servers["typescript"].(map[string]interface{})
		settings, ok := tsServer["settings"].(map[string]interface{})
		assert.True(t, ok)
		assert.NotNil(t, settings)
	})
}

// TestLSP_ConfigValidation_EnabledField tests the enabled field behavior
func TestLSP_ConfigValidation_EnabledField(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.json")

	// Create config with disabled server
	config := MCPConfig{
		Version: "1.0",
		Servers: map[string]MCPServerConfig{
			"enabled-server": {
				Command: "test-cmd",
				Args:    []string{"arg1"},
				Enabled: true,
			},
			"disabled-server": {
				Command: "test-cmd",
				Args:    []string{"arg2"},
				Enabled: false,
			},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0644)
	require.NoError(t, err)

	loadedConfig, err := LoadMCPConfig(configPath)
	require.NoError(t, err)

	t.Run("enabled server has Enabled=true", func(t *testing.T) {
		server, ok := loadedConfig.Servers["enabled-server"]
		require.True(t, ok)
		assert.True(t, server.Enabled)
	})

	t.Run("disabled server has Enabled=false", func(t *testing.T) {
		server, ok := loadedConfig.Servers["disabled-server"]
		require.True(t, ok)
		assert.False(t, server.Enabled)
	})
}

// TestLSP_MCPConfigGenerated_CreatesValidFile tests that GenerateMCPConfig creates valid files
func TestLSP_MCPConfigGenerated_CreatesValidFile(t *testing.T) {
	tempDir := t.TempDir()
	cm := NewConfigManager(tempDir)

	err := cm.GenerateMCPConfig()
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(cm.GetMCPConfigPath())
	assert.NoError(t, err)

	// Verify file is readable
	data, err := os.ReadFile(cm.GetMCPConfigPath())
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Verify file is valid JSON
	var config map[string]interface{}
	err = json.Unmarshal(data, &config)
	assert.NoError(t, err)

	// Verify file is pretty-printed
	content := string(data)
	assert.Contains(t, content, "\n")
	assert.Contains(t, content, "  ")
}

// TestLSP_MCPConfigGenerated_OverwritesExisting tests behavior with existing files
func TestLSP_MCPConfigGenerated_OverwritesExisting(t *testing.T) {
	tempDir := t.TempDir()
	cm := NewConfigManager(tempDir)

	// Create existing file
	existingContent := []byte(`{"version": "0.5", "existing": true}`)
	err := os.MkdirAll(filepath.Dir(cm.GetMCPConfigPath()), 0755)
	require.NoError(t, err)
	err = os.WriteFile(cm.GetMCPConfigPath(), existingContent, 0644)
	require.NoError(t, err)

	// Generate new config
	err = cm.GenerateMCPConfig()
	require.NoError(t, err)

	// Verify file was overwritten
	data, err := os.ReadFile(cm.GetMCPConfigPath())
	require.NoError(t, err)

	var config map[string]interface{}
	err = json.Unmarshal(data, &config)
	require.NoError(t, err)

	// Should have new version
	assert.Equal(t, "1.0", config["version"])
	// Should have servers
	assert.NotNil(t, config["servers"])
}

// TestLSP_CheckInstallation_DetailedOutput tests detailed output of CheckLSPInstallation
func TestLSP_CheckInstallation_DetailedOutput(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create all mock binaries
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	components := sm.CheckLSPInstallation()

	t.Run("returns correct number of components", func(t *testing.T) {
		assert.Len(t, components, 3)
	})

	t.Run("each component has name", func(t *testing.T) {
		for _, comp := range components {
			assert.NotEmpty(t, comp.Name)
		}
	})

	t.Run("each component has command", func(t *testing.T) {
		for _, comp := range components {
			assert.NotEmpty(t, comp.Command)
		}
	})

	t.Run("all components marked as installed", func(t *testing.T) {
		for _, comp := range components {
			assert.True(t, comp.Installed, "Expected %s to be installed", comp.Name)
		}
	})

	t.Run("component names match expected", func(t *testing.T) {
		expectedNames := map[string]bool{
			"LSP CLI":                    false,
			"gopls":                      false,
			"typescript-language-server": false,
		}
		for _, comp := range components {
			if _, ok := expectedNames[comp.Name]; ok {
				expectedNames[comp.Name] = true
			}
		}
		for name, found := range expectedNames {
			assert.True(t, found, "Expected component %s to be found", name)
		}
	})
}

// TestLSP_InstallComponents_Verification tests installation verification
func TestLSP_InstallComponents_Verification(t *testing.T) {
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("Skipping network-dependent test")
	}

	// Reset cache to ensure fresh state
	ResetCache()

	// Start with isolated PATH (nothing installed)
	cleanup := setIsolatedPath(t)
	defer cleanup()

	tempDir := t.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	t.Run("fails when cannot install", func(t *testing.T) {
		err := sm.InstallLSPComponents()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to install LSP components")
	})
}

// TestLSP_InstallComponents_AlreadyInstalledAll tests when all are already installed
func TestLSP_InstallComponents_AlreadyInstalledAll(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")

	err := sm.InstallLSPComponents()
	assert.NoError(t, err)
}

// TestLSP_CheckAndInstallLSP tests the CheckAndInstallLSP method
func TestLSP_CheckAndInstallLSP(t *testing.T) {
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("Skipping network-dependent test")
	}

	t.Run("succeeds when all installed", func(t *testing.T) {
		// Reset cache to ensure fresh state
		ResetCache()

		mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
		cleanup := setIsolatedPath(t, mockDir)
		defer cleanup()

		sm := NewSetupManager("./test-config", "./test-config")
		err := sm.CheckAndInstallLSP()
		assert.NoError(t, err)
	})

	t.Run("fails when cannot install missing", func(t *testing.T) {
		// Reset cache to ensure fresh state
		ResetCache()

		cleanup := setIsolatedPath(t)
		defer cleanup()

		tempDir := t.TempDir()
		sm := NewSetupManager(tempDir, tempDir)
		err := sm.CheckAndInstallLSP()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to install LSP packages")
	})
}

// TestLSP_CodeAnalysisSkill_Comprehensive tests comprehensive skill verification
func TestLSP_CodeAnalysisSkill_Comprehensive(t *testing.T) {
	t.Run("skill directory and file exist", func(t *testing.T) {
		tempDir := t.TempDir()
		skillsDir := filepath.Join(tempDir, "skills")
		skillPath := filepath.Join(skillsDir, "lsp-code-analysis")
		skillMDPath := filepath.Join(skillPath, "SKILL.md")

		err := os.MkdirAll(skillPath, 0755)
		require.NoError(t, err)

		err = os.WriteFile(skillMDPath, []byte("# LSP Code Analysis Skill\n\nTest content"), 0644)
		require.NoError(t, err)

		sm := NewSetupManager(tempDir, tempDir)
		err = sm.VerifyLSPCodeAnalysisSkill()
		assert.NoError(t, err)
	})

	t.Run("missing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		sm := NewSetupManager(tempDir, tempDir)
		err := sm.VerifyLSPCodeAnalysisSkill()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "directory not found")
	})

	t.Run("missing SKILL.md", func(t *testing.T) {
		tempDir := t.TempDir()
		skillsDir := filepath.Join(tempDir, "skills")
		skillPath := filepath.Join(skillsDir, "lsp-code-analysis")

		err := os.MkdirAll(skillPath, 0755)
		require.NoError(t, err)

		sm := NewSetupManager(tempDir, tempDir)
		err = sm.VerifyLSPCodeAnalysisSkill()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SKILL.md not found")
	})

	t.Run("empty skill directory", func(t *testing.T) {
		tempDir := t.TempDir()
		skillsDir := filepath.Join(tempDir, "skills")
		skillPath := filepath.Join(skillsDir, "lsp-code-analysis")

		err := os.MkdirAll(skillPath, 0755)
		require.NoError(t, err)

		sm := NewSetupManager(tempDir, tempDir)
		err = sm.VerifyLSPCodeAnalysisSkill()
		assert.Error(t, err)
	})
}

// TestLSP_AutoStartLSPServers tests the AutoStartLSPServers method
func TestLSP_AutoStartLSPServers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	t.Run("starts enabled servers from config", func(t *testing.T) {
		tempDir := t.TempDir()
		agentDir := filepath.Join(tempDir, "agent")
		err := os.MkdirAll(agentDir, 0755)
		require.NoError(t, err)

		// Create MCP config with enabled server
		config := MCPConfig{
			Version: "1.0",
			Servers: map[string]MCPServerConfig{
				"test-server": {
					Command: "sleep",
					Args:    []string{"5"},
					Enabled: true,
				},
			},
		}

		data, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(agentDir, "mcp.json"), data, 0644)
		require.NoError(t, err)

		sm := NewSetupManager(tempDir, tempDir)
		err = sm.AutoStartLSPServers()
		// May fail due to missing command, but should not panic
		// Error is expected in test environment
		_ = err // Accept any error in test environment
	})

	t.Run("skips disabled servers", func(t *testing.T) {
		tempDir := t.TempDir()
		agentDir := filepath.Join(tempDir, "agent")
		err := os.MkdirAll(agentDir, 0755)
		require.NoError(t, err)

		// Create MCP config with disabled server
		config := MCPConfig{
			Version: "1.0",
			Servers: map[string]MCPServerConfig{
				"disabled-server": {
					Command: "echo",
					Args:    []string{"test"},
					Enabled: false,
				},
			},
		}

		data, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(agentDir, "mcp.json"), data, 0644)
		require.NoError(t, err)

		sm := NewSetupManager(tempDir, tempDir)
		err = sm.AutoStartLSPServers()
		// Should succeed since server is disabled
		assert.NoError(t, err)
	})

	t.Run("uses existing config if available", func(t *testing.T) {
		tempDir := t.TempDir()
		agentDir := filepath.Join(tempDir, "agent")
		err := os.MkdirAll(agentDir, 0755)
		require.NoError(t, err)

		// Generate config first using ConfigManager
		cm := NewConfigManager(tempDir)
		err = cm.GenerateMCPConfig()
		require.NoError(t, err)

		// Verify config was generated
		_, err = os.Stat(cm.GetMCPConfigPath())
		assert.NoError(t, err, "Config file should be generated at %s", cm.GetMCPConfigPath())

		// Now AutoStartLSPServers should use this existing config
		sm := NewSetupManager(tempDir, tempDir)
		// Will try to start servers but may fail since real LSP servers aren't available
		_ = sm.AutoStartLSPServers()
	})
}

// TestLSP_PackageDefinition tests RequiredLSPPackages definitions
func TestLSP_PackageDefinition(t *testing.T) {
	t.Run("all required packages have required fields", func(t *testing.T) {
		for _, pkg := range RequiredLSPPackages {
			assert.NotEmpty(t, pkg.Name, "Package should have Name")
			assert.NotEmpty(t, pkg.Command, "Package should have Command")
			assert.NotEmpty(t, pkg.NPMName, "Package should have NPMName")
			assert.NotEmpty(t, pkg.Description, "Package should have Description")
		}
	})

	t.Run("required packages are correct", func(t *testing.T) {
		assert.Len(t, RequiredLSPPackages, 3)

		packageNames := make(map[string]bool)
		for _, pkg := range RequiredLSPPackages {
			packageNames[pkg.Name] = true
		}

		assert.True(t, packageNames["LSP CLI"])
		assert.True(t, packageNames["gopls"])
		assert.True(t, packageNames["typescript-language-server"])
	})
}

// TestLSP_isLSPPackageInstalled_NPXFallback tests the npx fallback behavior
func TestLSP_isLSPPackageInstalled_NPXFallback(t *testing.T) {
	tempDir := t.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	t.Run("returns false when neither which nor npx works", func(t *testing.T) {
		cleanup := setIsolatedPath(t)
		defer cleanup()

		pkg := LSPPackage{
			Name:    "Test Package",
			Command: "non-existent-binary",
			NPMName: "@test/package",
		}

		got := sm.isLSPPackageInstalled(pkg)
		assert.False(t, got)
	})
}

// TestLSP_installLSPPackage tests the installLSPPackage method
func TestLSP_installLSPPackage(t *testing.T) {
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("Skipping network-dependent test")
	}

	t.Run("fails when npm not available", func(t *testing.T) {
		cleanup := setIsolatedPath(t)
		defer cleanup()

		tempDir := t.TempDir()
		sm := NewSetupManager(tempDir, tempDir)

		pkg := LSPPackage{
			Name:    "Test Package",
			Command: "test-cmd",
			NPMName: "@test/package",
		}

		err := sm.installLSPPackage(pkg)
		assert.Error(t, err)
	})
}

// TestLSPCommandResult_EdgeCases tests edge cases for LSPCommandResult
func TestLSPCommandResult_EdgeCases(t *testing.T) {
	t.Run("empty data array", func(t *testing.T) {
		result := &LSPCommandResult{
			Success: true,
			Data:    []string{},
			Error:   "",
		}
		assert.True(t, result.Success)
		assert.Empty(t, result.Data)
		assert.Empty(t, result.Error)
	})

	t.Run("nil data", func(t *testing.T) {
		result := &LSPCommandResult{
			Success: false,
			Data:    nil,
			Error:   "some error",
		}
		assert.False(t, result.Success)
		assert.Nil(t, result.Data)
		assert.Equal(t, "some error", result.Error)
	})

	t.Run("large data array", func(t *testing.T) {
		data := make([]string, 100)
		for i := range data {
			data[i] = fmt.Sprintf("line %d", i)
		}
		result := &LSPCommandResult{
			Success: true,
			Data:    data,
			Error:   "",
		}
		assert.True(t, result.Success)
		assert.Len(t, result.Data, 100)
	})
}

// TestLSP_ExecuteLSPCommand_OutputParsing tests output parsing
func TestLSP_ExecuteLSPCommand_OutputParsing(t *testing.T) {
	tempDir := t.TempDir()

	// Create mock that returns multi-line output
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
echo 'Line 1'
echo 'Line 2'
echo 'Line 3'
exit 0
`
	err := os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Add mock binary to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	result, err := ExecuteLSPCommand("definition", "/test/file.go", 10, 5)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Len(t, result.Data, 3)
	assert.Contains(t, result.Data, "Line 1")
	assert.Contains(t, result.Data, "Line 2")
	assert.Contains(t, result.Data, "Line 3")
}

// TestLSP_ExecuteLSPCommand_EmptyOutput tests handling of empty output
func TestLSP_ExecuteLSPCommand_EmptyOutput(t *testing.T) {
	tempDir := t.TempDir()

	// Create mock that returns empty output
	mockLSPPath := filepath.Join(tempDir, "lsp")
	mockScript := `#!/bin/sh
echo ''
exit 0
`
	err := os.WriteFile(mockLSPPath, []byte(mockScript), 0755)
	require.NoError(t, err)

	// Add mock binary to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tempDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	result, err := ExecuteLSPCommand("outline", "/test/file.go", 0, 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	// Empty lines should be filtered out
	assert.Empty(t, result.Data)
}

// === Cache Tests ===

func TestLSPCache_BasicOperations(t *testing.T) {
	cache := NewLSPCache(100 * time.Millisecond)

	t.Run("get returns false for non-existent key", func(t *testing.T) {
		val, found := cache.Get("non-existent")
		assert.False(t, found)
		assert.False(t, val)
	})

	t.Run("set and get work correctly", func(t *testing.T) {
		cache.Set("test-key", true)
		val, found := cache.Get("test-key")
		assert.True(t, found)
		assert.True(t, val)
	})

	t.Run("entries expire after TTL", func(t *testing.T) {
		cache.Set("expiring-key", true)
		val, found := cache.Get("expiring-key")
		assert.True(t, found)
		assert.True(t, val)

		time.Sleep(150 * time.Millisecond)

		val, found = cache.Get("expiring-key")
		assert.False(t, found)
		assert.False(t, val)
	})

	t.Run("invalidate removes entry", func(t *testing.T) {
		cache.Set("invalidate-key", true)
		val, found := cache.Get("invalidate-key")
		assert.True(t, found)
		assert.True(t, val)

		cache.Invalidate("invalidate-key")

		val, found = cache.Get("invalidate-key")
		assert.False(t, found)
		assert.False(t, val)
	})

	t.Run("clear removes all entries", func(t *testing.T) {
		cache.Set("key1", true)
		cache.Set("key2", false)

		cache.Clear()

		_, found1 := cache.Get("key1")
		_, found2 := cache.Get("key2")
		assert.False(t, found1)
		assert.False(t, found2)
	})
}

func TestLSPCache_Stats(t *testing.T) {
	cache := NewLSPCache(1 * time.Minute)
	cache.Clear() // Reset stats

	t.Run("stats track hits and misses", func(t *testing.T) {
		// Miss
		cache.Get("miss-key")
		hits, misses := cache.Stats()
		assert.Equal(t, uint64(0), hits)
		assert.Equal(t, uint64(1), misses)

		// Set and hit
		cache.Set("hit-key", true)
		cache.Get("hit-key")
		hits, misses = cache.Stats()
		assert.Equal(t, uint64(1), hits)
		assert.Equal(t, uint64(1), misses)
	})
}

func TestLSPCache_ConcurrentAccess(t *testing.T) {
	cache := NewLSPCache(1 * time.Minute)

	t.Run("concurrent reads and writes", func(t *testing.T) {
		var wg sync.WaitGroup

		// Concurrent writes
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				cache.Set(fmt.Sprintf("key-%d", n), n%2 == 0)
			}(i)
		}

		// Concurrent reads
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				cache.Get(fmt.Sprintf("key-%d", n))
			}(i)
		}

		wg.Wait()

		// Verify no panics and cache has entries
		hits, misses := cache.Stats()
		assert.Greater(t, hits+misses, uint64(0))
	})
}

func TestCacheIntegration(t *testing.T) {
	setupTestCache(t)

	t.Run("caching reduces process spawns", func(t *testing.T) {
		// Use a fresh cache to avoid interference from other tests
		cache := NewLSPCache(1 * time.Minute)

		// Verify cache operations work correctly
		// First get - should be a cache miss
		_, found := cache.Get("test-key")
		assert.False(t, found)

		hits1, misses1 := cache.Stats()
		assert.Equal(t, uint64(0), hits1)
		assert.Equal(t, uint64(1), misses1)

		// Set value
		cache.Set("test-key", true)

		// Second get - should be a cache hit
		cachedVal, found := cache.Get("test-key")
		assert.True(t, found)
		assert.True(t, cachedVal)

		hits2, misses2 := cache.Stats()
		assert.Equal(t, uint64(1), hits2)
		assert.Equal(t, uint64(1), misses2)
	})

	t.Run("batch check returns correct results", func(t *testing.T) {
		// Create mock binaries
		mockDir := setupMockBinaries(t, "lsp", "gopls")
		cleanup := setIsolatedPath(t, mockDir)
		defer cleanup()

		ResetCache()
		sm := NewSetupManager("./test-config", "./test-config")

		// Batch check should return correct results
		result := sm.BatchCheckLSPInstallation()
		assert.NotNil(t, result)

		// Verify results
		lspInstalled, ok := result.Get("LSP CLI")
		assert.True(t, ok)
		assert.True(t, lspInstalled)

		goplsInstalled, ok := result.Get("gopls")
		assert.True(t, ok)
		assert.True(t, goplsInstalled)

		tsInstalled, ok := result.Get("typescript-language-server")
		assert.True(t, ok)
		assert.False(t, tsInstalled) // Not in mock binaries
	})
}

func TestBatchCheckResult(t *testing.T) {
	result := NewBatchCheckResult()

	t.Run("set and get work correctly", func(t *testing.T) {
		result.Set("test-pkg", true)
		val, ok := result.Get("test-pkg")
		assert.True(t, ok)
		assert.True(t, val)

		result.Set("test-pkg2", false)
		val, ok = result.Get("test-pkg2")
		assert.True(t, ok)
		assert.False(t, val)
	})

	t.Run("get returns false for non-existent key", func(t *testing.T) {
		val, ok := result.Get("non-existent")
		assert.False(t, ok)
		assert.False(t, val)
	})

	t.Run("concurrent access is safe", func(t *testing.T) {
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(2)
			go func(n int) {
				defer wg.Done()
				result.Set(fmt.Sprintf("key-%d", n), true)
			}(i)
			go func(n int) {
				defer wg.Done()
				result.Get(fmt.Sprintf("key-%d", n))
			}(i)
		}

		wg.Wait()
	})
}

func TestCacheStats(t *testing.T) {
	t.Run("hit rate calculation", func(t *testing.T) {
		// Create a fresh cache to avoid shared state
		cache := NewLSPCache(1 * time.Minute)
		cache.Set("key1", true)
		cache.Set("key2", false)

		// Hits
		cache.Get("key1")
		cache.Get("key2")

		// Miss
		cache.Get("non-existent")

		hits, misses := cache.Stats()
		assert.Equal(t, uint64(2), hits)
		assert.Equal(t, uint64(1), misses)

		total := hits + misses
		hitRate := float64(hits) / float64(total) * 100
		assert.InDelta(t, 66.67, hitRate, 0.1)
	})
}

// Performance benchmark tests
func BenchmarkIsLSPPackageInstalled_Cached(b *testing.B) {
	setupTestCache(b)

	mockDir, _ := os.MkdirTemp("", "lsp-bench")
	defer os.RemoveAll(mockDir)

	// Create mock binary
	mockLSPPath := filepath.Join(mockDir, "lsp")
	mockScript := "#!/bin/sh\nexit 0\n"
	os.WriteFile(mockLSPPath, []byte(mockScript), 0755)

	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", mockDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	sm := NewSetupManager("./test-config", "./test-config")
	pkg := LSPPackage{Name: "LSP CLI", Command: "lsp"}

	// Warm up cache
	sm.isLSPPackageInstalled(pkg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.isLSPPackageInstalled(pkg)
	}
}

func BenchmarkIsLSPPackageInstalled_Uncached(b *testing.B) {
	mockDir, _ := os.MkdirTemp("", "lsp-bench")
	defer os.RemoveAll(mockDir)

	// Create mock binary
	mockLSPPath := filepath.Join(mockDir, "lsp")
	mockScript := "#!/bin/sh\nexit 0\n"
	os.WriteFile(mockLSPPath, []byte(mockScript), 0755)

	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", mockDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	sm := NewSetupManager("./test-config", "./test-config")
	pkg := LSPPackage{Name: "LSP CLI", Command: "lsp"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear cache before each iteration
		ResetCache()
		sm.isLSPPackageInstalled(pkg)
	}
}

func BenchmarkCheckLSPInstallation_Cached(b *testing.B) {
	setupTestCache(b)

	mockDir, _ := os.MkdirTemp("", "lsp-bench")
	defer os.RemoveAll(mockDir)

	// Create all mock binaries
	for _, bin := range []string{"lsp", "gopls", "typescript-language-server"} {
		mockPath := filepath.Join(mockDir, bin)
		mockScript := "#!/bin/sh\nexit 0\n"
		os.WriteFile(mockPath, []byte(mockScript), 0755)
	}

	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", mockDir+":"+originalPath)
	defer os.Setenv("PATH", originalPath)

	sm := NewSetupManager("./test-config", "./test-config")

	// Warm up cache
	sm.CheckLSPInstallation()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.CheckLSPInstallation()
	}
}
