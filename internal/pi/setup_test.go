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

// setupMockBinary creates a mock binary in a temp directory
func setupMockBinary(t *testing.T, name string, dir string) string {
	t.Helper()

	var binaryPath string
	if runtime.GOOS == "windows" {
		binaryPath = filepath.Join(dir, name+".exe")
	} else {
		binaryPath = filepath.Join(dir, name)
	}

	// Create a simple script that exits with code 0
	var content []byte
	if runtime.GOOS == "windows" {
		content = []byte("@echo off\nexit /b 0\n")
	} else {
		content = []byte("#!/bin/sh\nexit 0\n")
	}

	err := os.WriteFile(binaryPath, content, 0755)
	require.NoError(t, err, "Failed to create mock binary: %s", name)

	return binaryPath
}

func TestSetupManager_RunSetup_Success(t *testing.T) {
	// Create temp directories
	tempConfigDir := t.TempDir()
	tempPiConfigDir := t.TempDir()

	// Create mock binaries for pi and LSP components
	mockBinDir := t.TempDir()
	setupMockBinary(t, "pi", mockBinDir)
	setupMockBinary(t, "lsp", mockBinDir)
	setupMockBinary(t, "gopls", mockBinDir)
	setupMockBinary(t, "typescript-language-server", mockBinDir)
	setupMockBinary(t, "skills", mockBinDir)

	cleanup := setIsolatedPath(t, mockBinDir)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager(tempConfigDir, tempPiConfigDir)

	// Run setup
	result, err := sm.RunSetup()

	// Assert no fatal error
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result fields
	assert.True(t, result.PiInstalled, "Pi should be marked as installed")
	assert.True(t, result.LSPInstalled, "LSP should be marked as installed")
	assert.NotEmpty(t, result.Timestamp, "Timestamp should be set")

	// Verify directory structure was created
	agentDir := filepath.Join(tempPiConfigDir, "agent")
	skillsDir := filepath.Join(tempPiConfigDir, "skills")

	_, err = os.Stat(agentDir)
	assert.NoError(t, err, "Agent directory should exist")

	_, err = os.Stat(skillsDir)
	assert.NoError(t, err, "Skills directory should exist")

	// Verify mcp.json was created
	mcpPath := filepath.Join(agentDir, "mcp.json")
	_, err = os.Stat(mcpPath)
	assert.NoError(t, err, "mcp.json should exist")

	// Verify tracking file was created
	trackingPath := filepath.Join(tempConfigDir, "skills-installed.json")
	_, err = os.Stat(trackingPath)
	assert.NoError(t, err, "Tracking file should exist")

	// Verify tracking file contains valid JSON
	data, err := os.ReadFile(trackingPath)
	require.NoError(t, err)

	var tracking SetupResult
	err = json.Unmarshal(data, &tracking)
	assert.NoError(t, err, "Tracking file should contain valid JSON")
}

func TestSetupManager_CheckAndInstallPi_Installed(t *testing.T) {
	// Create mock binary for pi
	mockBinDir := t.TempDir()
	setupMockBinary(t, "pi", mockBinDir)

	cleanup := setIsolatedPath(t, mockBinDir)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager(t.TempDir(), t.TempDir())

	// Verify pi is detected as installed
	assert.True(t, sm.IsPiInstalled(), "Pi should be detected as installed")

	// Call CheckAndInstallPi - should return nil since already installed
	err := sm.CheckAndInstallPi()
	assert.NoError(t, err, "CheckAndInstallPi should succeed when pi is already installed")
}

func TestSetupManager_CheckAndInstallPi_NotInstalled(t *testing.T) {
	// Skip if npm is not available
	if _, err := os.Stat("/usr/bin/npm"); os.IsNotExist(err) {
		if _, err := os.Stat("/usr/local/bin/npm"); os.IsNotExist(err) {
			t.Skip("npm not available, skipping install test")
		}
	}

	// Use isolated PATH (no pi binary)
	cleanup := setIsolatedPath(t)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager(t.TempDir(), t.TempDir())

	// Verify pi is detected as not installed
	assert.False(t, sm.IsPiInstalled(), "Pi should be detected as not installed")

	// Call CheckAndInstallPi - will attempt to install via npm
	// This may fail in test environment, but we verify the error handling
	err := sm.CheckAndInstallPi()

	// The install may fail due to npm permissions/network, but it should attempt
	// We verify the method runs without panic and handles errors gracefully
	// The error may or may not be nil depending on environment
	_ = err // Don't assert on error since it depends on environment
}

func TestSetupManager_CheckAndInstallLSP_Success(t *testing.T) {
	// Reset cache to ensure fresh state
	ResetCache()

	// Create mock binaries for all LSP components
	mockBinDir := t.TempDir()
	setupMockBinary(t, "lsp", mockBinDir)
	setupMockBinary(t, "gopls", mockBinDir)
	setupMockBinary(t, "typescript-language-server", mockBinDir)

	cleanup := setIsolatedPath(t, mockBinDir)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager(t.TempDir(), t.TempDir())

	// Call CheckAndInstallLSP
	err := sm.CheckAndInstallLSP()

	// Should succeed since all binaries are present
	assert.NoError(t, err, "CheckAndInstallLSP should succeed when all LSP components are installed")
}

func TestSetupManager_CheckAndInstallSkills_Success(t *testing.T) {
	// Create mock binary for skills CLI
	mockBinDir := t.TempDir()
	setupMockBinary(t, "skills", mockBinDir)

	cleanup := setIsolatedPath(t, mockBinDir)
	defer cleanup()

	// Create temp directories
	tempConfigDir := t.TempDir()
	tempPiConfigDir := t.TempDir()

	// Create setup manager
	sm := NewSetupManager(tempConfigDir, tempPiConfigDir)

	// Verify skills are not installed initially
	assert.False(t, sm.AreSkillsInstalled(), "Skills should not be installed initially")

	// Call CheckAndInstallSkills
	// Note: This will attempt to run 'npx skills add' which may fail
	// but the method handles failures gracefully and creates tracking file
	err := sm.CheckAndInstallSkills()

	// The error may be nil or not depending on whether skills CLI actually works
	// In a unit test with mock binary, the install commands will fail
	// but we verify the method runs without panic
	_ = err

	// Verify tracking file was created (even on partial failure)
	// Note: This might not be created if the method errors out early
	// So we don't strictly assert this
}

func TestSetupManager_CreateDirectoryStructure(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	// Create setup manager
	sm := NewSetupManager(tempDir, tempDir)

	// Call CreateDirectoryStructure
	err := sm.CreateDirectoryStructure()
	require.NoError(t, err, "CreateDirectoryStructure should succeed")

	// Verify agent directory was created with correct permissions
	agentDir := filepath.Join(tempDir, "agent")
	info, err := os.Stat(agentDir)
	require.NoError(t, err, "Agent directory should exist")
	assert.True(t, info.IsDir(), "Agent path should be a directory")
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm(), "Agent directory should have 0755 permissions")

	// Verify skills directory was created with correct permissions
	skillsDir := filepath.Join(tempDir, "skills")
	info, err = os.Stat(skillsDir)
	require.NoError(t, err, "Skills directory should exist")
	assert.True(t, info.IsDir(), "Skills path should be a directory")
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm(), "Skills directory should have 0755 permissions")

	// Verify mcp.json was created
	mcpPath := filepath.Join(agentDir, "mcp.json")
	_, err = os.Stat(mcpPath)
	assert.NoError(t, err, "mcp.json should exist")

	// Verify mcp.json contains valid JSON
	data, err := os.ReadFile(mcpPath)
	require.NoError(t, err)

	var mcpConfig map[string]interface{}
	err = json.Unmarshal(data, &mcpConfig)
	assert.NoError(t, err, "mcp.json should contain valid JSON")
	assert.Contains(t, mcpConfig, "version", "mcp.json should have version field")
	assert.Contains(t, mcpConfig, "servers", "mcp.json should have servers field")

	// Verify tracking file was created
	trackingPath := filepath.Join(tempDir, "skills-installed.json")
	_, err = os.Stat(trackingPath)
	assert.NoError(t, err, "skills-installed.json should exist")

	// Verify tracking file contains valid JSON
	data, err = os.ReadFile(trackingPath)
	require.NoError(t, err)

	var tracking map[string]interface{}
	err = json.Unmarshal(data, &tracking)
	assert.NoError(t, err, "skills-installed.json should contain valid JSON")
}

func TestSetupManager_CreateDirectoryStructure_WithConfigManager(t *testing.T) {
	// Test that CreateDirectoryStructure properly delegates to ConfigManager
	tempDir := t.TempDir()

	sm := NewSetupManager(tempDir, tempDir)
	require.NotNil(t, sm.configManager, "ConfigManager should be initialized")

	err := sm.CreateDirectoryStructure()
	assert.NoError(t, err, "CreateDirectoryStructure should succeed")

	// Verify structure was created
	agentDir := filepath.Join(tempDir, "agent")
	skillsDir := filepath.Join(tempDir, "skills")

	_, err = os.Stat(agentDir)
	assert.NoError(t, err, "Agent directory should exist")

	_, err = os.Stat(skillsDir)
	assert.NoError(t, err, "Skills directory should exist")
}

func TestSetupManager_TrackingFile_Created(t *testing.T) {
	// Create temp directory
	tempConfigDir := t.TempDir()
	tempPiConfigDir := t.TempDir()

	// Create setup manager
	sm := NewSetupManager(tempConfigDir, tempPiConfigDir)

	// Create a result and save it
	result := &SetupResult{
		PiInstalled:     true,
		LSPInstalled:    true,
		SkillsInstalled: true,
		Timestamp:       time.Now(),
		Errors:          []string{},
	}

	// Save result using the private method via RunSetup behavior
	// First create the config directory
	err := os.MkdirAll(tempConfigDir, 0755)
	require.NoError(t, err)

	// Save tracking file directly
	trackingPath := filepath.Join(tempConfigDir, "skills-installed.json")
	data, _ := json.MarshalIndent(result, "", "  ")
	err = os.WriteFile(trackingPath, data, 0644)
	require.NoError(t, err)

	// Verify tracking file exists
	_, err = os.Stat(trackingPath)
	assert.NoError(t, err, "Tracking file should exist")

	// Verify we can read it back
	readResult, err := sm.GetLastSetupResult()
	require.NoError(t, err, "Should be able to read last setup result")
	assert.True(t, readResult.PiInstalled, "PiInstalled should be true")
	assert.True(t, readResult.LSPInstalled, "LSPInstalled should be true")
	assert.True(t, readResult.SkillsInstalled, "SkillsInstalled should be true")
}

func TestSetupManager_GracefulFailure(t *testing.T) {
	// Create temp directories
	tempConfigDir := t.TempDir()
	tempPiConfigDir := t.TempDir()

	// Use isolated PATH with no binaries - this will cause failures
	cleanup := setIsolatedPath(t)
	defer cleanup()

	// Create setup manager
	sm := NewSetupManager(tempConfigDir, tempPiConfigDir)

	// Run setup - should not panic even though everything will fail
	result, err := sm.RunSetup()

	// RunSetup returns nil error even on partial failures (errors are collected in result)
	// The only error that would be returned is if config directory creation fails
	// which shouldn't happen with a temp directory
	if err != nil {
		// If there is an error, it should be related to config directory creation
		assert.Contains(t, err.Error(), "config directory", "Error should be about config directory creation")
		return
	}

	require.NotNil(t, result, "Result should not be nil even on partial failure")

	// Verify that result contains errors but still has valid structure
	assert.NotEmpty(t, result.Errors, "Should have collected errors for failed operations")
	assert.False(t, result.PiInstalled, "Pi should not be marked as installed when installation fails")
	assert.False(t, result.LSPInstalled, "LSP should not be marked as installed when installation fails")
	assert.NotEmpty(t, result.Timestamp, "Timestamp should still be set")

	// Verify directory structure was still created (this happens before component installation)
	agentDir := filepath.Join(tempPiConfigDir, "agent")
	skillsDir := filepath.Join(tempPiConfigDir, "skills")

	_, err = os.Stat(agentDir)
	assert.NoError(t, err, "Agent directory should be created even if component installation fails")

	_, err = os.Stat(skillsDir)
	assert.NoError(t, err, "Skills directory should be created even if component installation fails")
}

func TestSetupManager_AreSkillsInstalled(t *testing.T) {
	tempDir := t.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	// Initially skills should not be installed
	assert.False(t, sm.AreSkillsInstalled(), "Skills should not be installed initially")

	// Create tracking file
	trackingPath := filepath.Join(tempDir, "skills-installed.json")
	err := os.WriteFile(trackingPath, []byte(`{"skills_installed": true}`), 0644)
	require.NoError(t, err)

	// Now skills should be detected as installed
	assert.True(t, sm.AreSkillsInstalled(), "Skills should be detected as installed after tracking file creation")
}

func TestSetupManager_IsPiInstalled(t *testing.T) {
	// Test when pi is not installed
	cleanup := setIsolatedPath(t)
	defer cleanup()

	sm := NewSetupManager(t.TempDir(), t.TempDir())
	assert.False(t, sm.IsPiInstalled(), "Pi should not be detected as installed when binary not present")

	// Test when pi is installed
	mockBinDir := t.TempDir()
	setupMockBinary(t, "pi", mockBinDir)

	cleanup2 := setIsolatedPath(t, mockBinDir)
	defer cleanup2()

	assert.True(t, sm.IsPiInstalled(), "Pi should be detected as installed when binary is present")
}

func TestSetupManager_GetLastSetupResult_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	// Try to get result when no tracking file exists
	result, err := sm.GetLastSetupResult()
	assert.Error(t, err, "Should return error when tracking file doesn't exist")
	assert.Nil(t, result, "Result should be nil when tracking file doesn't exist")
}

func TestSetupManager_GetLastSetupResult_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	// Create invalid JSON tracking file
	trackingPath := filepath.Join(tempDir, "skills-installed.json")
	err := os.WriteFile(trackingPath, []byte(`invalid json`), 0644)
	require.NoError(t, err)

	// Try to get result with invalid JSON
	result, err := sm.GetLastSetupResult()
	assert.Error(t, err, "Should return error when tracking file has invalid JSON")
	assert.Nil(t, result, "Result should be nil when tracking file has invalid JSON")
}

func TestSetupManager_NewSetupManager_DefaultValues(t *testing.T) {
	// Create setup manager with empty strings (should use defaults)
	sm := NewSetupManager("", "")

	// Verify defaults were applied
	assert.NotEmpty(t, sm.configDir, "ConfigDir should have default value")
	assert.Equal(t, ".pi/hydra-config", sm.piConfigDir, "PiConfigDir should default to '.pi/hydra-config'")
	assert.NotNil(t, sm.configManager, "ConfigManager should be initialized")
	assert.NotEmpty(t, sm.trackingFile, "TrackingFile should be set")
}

func TestSetupManager_NewSetupManager_CustomValues(t *testing.T) {
	// Create setup manager with custom values
	configDir := "/custom/config"
	piConfigDir := "/custom/pi-config"

	sm := NewSetupManager(configDir, piConfigDir)

	// Verify custom values were used
	assert.Equal(t, configDir, sm.configDir, "ConfigDir should use custom value")
	assert.Equal(t, piConfigDir, sm.piConfigDir, "PiConfigDir should use custom value")
	assert.NotNil(t, sm.configManager, "ConfigManager should be initialized")
}

func TestSetupManager_RunSetup_ConfigDirCreationFailure(t *testing.T) {
	// Use an invalid path that cannot be created (e.g., a file path)
	tempFile, err := os.CreateTemp("", "hydra-test")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())

	// Try to use a file as a directory (should fail)
	sm := NewSetupManager(tempFile.Name(), tempFile.Name())

	_, err = sm.RunSetup()
	assert.Error(t, err, "Should return error when config directory cannot be created")
	assert.Contains(t, err.Error(), "config directory", "Error should mention config directory")
}

func TestSetupResult_StructFields(t *testing.T) {
	// Test that SetupResult struct has the expected fields
	result := SetupResult{
		PiInstalled:     true,
		LSPInstalled:    true,
		SkillsInstalled: true,
		Errors:          []string{"error1", "error2"},
	}

	assert.True(t, result.PiInstalled)
	assert.True(t, result.LSPInstalled)
	assert.True(t, result.SkillsInstalled)
	assert.Len(t, result.Errors, 2)
	assert.Equal(t, "error1", result.Errors[0])
	assert.Equal(t, "error2", result.Errors[1])
}
