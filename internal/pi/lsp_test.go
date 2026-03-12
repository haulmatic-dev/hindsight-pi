package pi

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestCheckLSPInstallation_MissingComponents(t *testing.T) {
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

func TestInstallLSPComponents_PartialFailure(t *testing.T) {
	// Skip this test if npm is not available or in restricted environments
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("Skipping network-dependent test")
	}

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
	assert.Contains(t, err.Error(), "failed to install LSP components", "Error message should indicate installation failure")
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
	// Create mock binaries for all 3 components
	mockDir := setupMockBinaries(t, "lsp", "gopls", "typescript-language-server")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	assert.True(t, sm.IsLSPInstalled(), "Expected IsLSPInstalled() to return true when all components are installed")
}

func TestIsLSPInstalled_PartiallyInstalled(t *testing.T) {
	// Create mock binaries for only 1 component
	mockDir := setupMockBinaries(t, "lsp")
	cleanup := setIsolatedPath(t, mockDir)
	defer cleanup()

	sm := NewSetupManager("./test-config", "./test-config")
	assert.False(t, sm.IsLSPInstalled(), "Expected IsLSPInstalled() to return false when only some components are installed")
}

func TestIsLSPInstalled_NoneInstalled(t *testing.T) {
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
