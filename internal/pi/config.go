// Package pi provides automatic setup and configuration for Pi agent ecosystem
package pi

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// MCPServerConfig represents a single MCP server configuration
type MCPServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Enabled bool     `json:"enabled"`
}

// MCPConfig represents the complete MCP configuration structure
type MCPConfig struct {
	Version string                     `json:"version"`
	Servers map[string]MCPServerConfig `json:"servers"`
}

// ConfigManager handles pi-config directory structure creation and management
type ConfigManager struct {
	baseDir      string
	agentDir     string
	skillsDir    string
	mcpConfig    string
	trackingFile string
}

// NewConfigManager creates a new config manager with the specified base directory
func NewConfigManager(baseDir string) *ConfigManager {
	return &ConfigManager{
		baseDir:      baseDir,
		agentDir:     filepath.Join(baseDir, "agent"),
		skillsDir:    filepath.Join(baseDir, "skills"),
		mcpConfig:    filepath.Join(baseDir, "agent", "mcp.json"),
		trackingFile: filepath.Join(baseDir, "skills-installed.json"),
	}
}

// EnsureDirectory creates a directory if it doesn't exist
// Returns nil if directory already exists or was created successfully
func EnsureDirectory(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}
	return nil
}

// EnsureFile creates a file with the given content if it doesn't exist
// If the file exists, it returns nil without modification
func EnsureFile(path string, content []byte, perm os.FileMode) error {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		// File exists, do nothing
		return nil
	} else if !os.IsNotExist(err) {
		// Some other error occurred
		return fmt.Errorf("failed to check file %s: %w", path, err)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := EnsureDirectory(dir, 0755); err != nil {
		return err
	}

	// Create the file
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("failed to create file %s: %w", path, err)
	}

	return nil
}

// CreateDirectoryStructure creates the complete pi-config directory structure
// This includes:
// - pi-config/agent/ directory (0755 permissions)
// - pi-config/skills/ directory (0755 permissions)
// - pi-config/agent/mcp.json with LSP server configuration
// - pi-config/skills-installed.json tracking file
func (cm *ConfigManager) CreateDirectoryStructure() error {
	// Create agent directory
	if err := EnsureDirectory(cm.agentDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	// Create skills directory
	if err := EnsureDirectory(cm.skillsDir, 0755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	// Create mcp.json with basic LSP server configuration
	mcpConfig := map[string]interface{}{
		"version": "1.0",
		"servers": map[string]interface{}{
			"lsp": map[string]interface{}{
				"enabled": true,
				"command": "pi-lsp",
				"args":    []string{},
			},
		},
		"capabilities": map[string]interface{}{
			"textDocumentSync": 1,
			"hoverProvider":    true,
			"completionProvider": map[string]interface{}{
				"triggerCharacters": []string{".", ":", ">"},
			},
		},
	}

	mcpData, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mcp config: %w", err)
	}

	if err := EnsureFile(cm.mcpConfig, mcpData, 0644); err != nil {
		return fmt.Errorf("failed to create mcp.json: %w", err)
	}

	// Create skills-installed.json tracking file
	trackingData := map[string]interface{}{
		"version":   "1.0",
		"installed": []string{},
		"timestamp": "",
		"metadata": map[string]interface{}{
			"lastCheck":     "",
			"autoUpdate":    false,
			"updateChannel": "stable",
		},
	}

	trackingJSON, err := json.MarshalIndent(trackingData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tracking data: %w", err)
	}

	if err := EnsureFile(cm.trackingFile, trackingJSON, 0644); err != nil {
		return fmt.Errorf("failed to create skills-installed.json: %w", err)
	}

	return nil
}

// GetBaseDir returns the base directory path
func (cm *ConfigManager) GetBaseDir() string {
	return cm.baseDir
}

// GetAgentDir returns the agent directory path
func (cm *ConfigManager) GetAgentDir() string {
	return cm.agentDir
}

// GetSkillsDir returns the skills directory path
func (cm *ConfigManager) GetSkillsDir() string {
	return cm.skillsDir
}

// GetMCPConfigPath returns the mcp.json file path
func (cm *ConfigManager) GetMCPConfigPath() string {
	return cm.mcpConfig
}

// GetTrackingFilePath returns the skills-installed.json file path
func (cm *ConfigManager) GetTrackingFilePath() string {
	return cm.trackingFile
}

// GenerateMCPConfig creates and writes the MCP configuration file with LSP server settings
// It generates a configuration including gopls and typescript-language-server configurations,
// validates the JSON syntax before writing, and saves it to the agent directory.
func (cm *ConfigManager) GenerateMCPConfig() error {
	// Create the MCP configuration structure with enhanced LSP capabilities
	config := map[string]interface{}{
		"version": "1.0",
		"servers": map[string]interface{}{
			"go": map[string]interface{}{
				"command": "gopls",
				"args":    []string{"serve", "-rpc.trace"},
				"enabled": true,
				"rootUri": "file://${workspaceFolder}",
				"settings": map[string]interface{}{
					"gopls": map[string]interface{}{
						"formatting.gofumpt": false,
						"ui.diagnostic.annotations": map[string]bool{
							"bounds": true,
							"escape": true,
							"inline": true,
							"nil":    true,
						},
					},
				},
			},
			"typescript": map[string]interface{}{
				"command": "typescript-language-server",
				"args":    []string{"--stdio"},
				"enabled": true,
				"rootUri": "file://${workspaceFolder}",
				"settings": map[string]interface{}{
					"typescript": map[string]interface{}{
						"format.enable":                 true,
						"suggest.completeFunctionCalls": true,
					},
				},
			},
		},
		"capabilities": map[string]interface{}{
			"textDocumentSync": 1,
			"hoverProvider":    true,
			"definitionProvider": map[string]bool{
				"enabled": true,
			},
			"referencesProvider": map[string]bool{
				"enabled": true,
			},
			"documentSymbolProvider": map[string]bool{
				"enabled": true,
			},
			"completionProvider": map[string]interface{}{
				"triggerCharacters": []string{".", ":", ">"},
				"resolveProvider":   true,
			},
		},
		"lspCommands": map[string]interface{}{
			"definition": map[string]string{
				"description": "Get definition location for symbol at position",
				"usage":       "lsp definition <file> <line>:<column>",
			},
			"reference": map[string]string{
				"description": "Find all references to symbol at position",
				"usage":       "lsp reference <file> <line>:<column>",
			},
			"outline": map[string]string{
				"description": "Get document outline (symbols)",
				"usage":       "lsp outline <file>",
			},
		},
	}

	// Marshal to JSON with indentation for pretty formatting
	jsonData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	// Validate JSON by unmarshaling it
	var validationTarget map[string]interface{}
	if err := json.Unmarshal(jsonData, &validationTarget); err != nil {
		return fmt.Errorf("generated JSON failed validation: %w", err)
	}

	// Ensure the parent directory exists
	if err := EnsureDirectory(cm.agentDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	// Write the configuration file
	if err := os.WriteFile(cm.mcpConfig, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	log.Printf("[MCP] Generated mcp.json at %s", cm.mcpConfig)
	return nil
}
