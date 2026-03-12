package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateDirectoryStructure(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create config manager
	cm := NewConfigManager(tempDir)

	// Test creating directory structure
	err := cm.CreateDirectoryStructure()
	if err != nil {
		t.Fatalf("CreateDirectoryStructure failed: %v", err)
	}

	// Verify agent directory exists
	agentDir := cm.GetAgentDir()
	info, err := os.Stat(agentDir)
	if err != nil {
		t.Errorf("Agent directory not created: %v", err)
	} else if !info.IsDir() {
		t.Error("Agent path exists but is not a directory")
	} else if info.Mode().Perm() != 0755 {
		t.Errorf("Agent directory has wrong permissions: got %o, want 0755", info.Mode().Perm())
	}

	// Verify skills directory exists
	skillsDir := cm.GetSkillsDir()
	info, err = os.Stat(skillsDir)
	if err != nil {
		t.Errorf("Skills directory not created: %v", err)
	} else if !info.IsDir() {
		t.Error("Skills path exists but is not a directory")
	} else if info.Mode().Perm() != 0755 {
		t.Errorf("Skills directory has wrong permissions: got %o, want 0755", info.Mode().Perm())
	}

	// Verify mcp.json exists
	mcpPath := cm.GetMCPConfigPath()
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Errorf("mcp.json not created: %v", err)
	} else {
		// Verify it's valid JSON
		var mcpConfig map[string]interface{}
		if err := json.Unmarshal(data, &mcpConfig); err != nil {
			t.Errorf("mcp.json is not valid JSON: %v", err)
		}
		// Verify it has the expected structure
		if _, ok := mcpConfig["version"]; !ok {
			t.Error("mcp.json missing 'version' field")
		}
		if _, ok := mcpConfig["servers"]; !ok {
			t.Error("mcp.json missing 'servers' field")
		}
	}

	// Verify tracking file exists
	trackingPath := cm.GetTrackingFilePath()
	data, err = os.ReadFile(trackingPath)
	if err != nil {
		t.Errorf("skills-installed.json not created: %v", err)
	} else {
		// Verify it's valid JSON
		var tracking map[string]interface{}
		if err := json.Unmarshal(data, &tracking); err != nil {
			t.Errorf("skills-installed.json is not valid JSON: %v", err)
		}
		// Verify it has the expected structure
		if _, ok := tracking["version"]; !ok {
			t.Error("skills-installed.json missing 'version' field")
		}
		if _, ok := tracking["installed"]; !ok {
			t.Error("skills-installed.json missing 'installed' field")
		}
	}
}

func TestCreateDirectoryStructure_AlreadyExists(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create config manager
	cm := NewConfigManager(tempDir)

	// First creation
	err := cm.CreateDirectoryStructure()
	if err != nil {
		t.Fatalf("First CreateDirectoryStructure failed: %v", err)
	}

	// Second creation should not fail
	err = cm.CreateDirectoryStructure()
	if err != nil {
		t.Errorf("Second CreateDirectoryStructure failed: %v", err)
	}
}

func TestEnsureDirectory(t *testing.T) {
	tempDir := t.TempDir()
	testDir := filepath.Join(tempDir, "test", "nested", "dir")

	// Create directory
	err := EnsureDirectory(testDir, 0755)
	if err != nil {
		t.Fatalf("EnsureDirectory failed: %v", err)
	}

	// Verify directory exists
	info, err := os.Stat(testDir)
	if err != nil {
		t.Errorf("Directory not created: %v", err)
	} else if !info.IsDir() {
		t.Error("Path exists but is not a directory")
	}

	// Creating again should not fail
	err = EnsureDirectory(testDir, 0755)
	if err != nil {
		t.Errorf("EnsureDirectory on existing directory failed: %v", err)
	}
}

func TestEnsureFile(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test", "file.txt")
	content := []byte(`{"test": true}`)

	// Create file
	err := EnsureFile(testFile, content, 0644)
	if err != nil {
		t.Fatalf("EnsureFile failed: %v", err)
	}

	// Verify file exists and has correct content
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Errorf("File not created: %v", err)
	} else if string(data) != string(content) {
		t.Errorf("File content mismatch: got %s, want %s", string(data), string(content))
	}

	// Verify permissions
	info, err := os.Stat(testFile)
	if err != nil {
		t.Errorf("Failed to stat file: %v", err)
	} else if info.Mode().Perm() != 0644 {
		t.Errorf("File has wrong permissions: got %o, want 0644", info.Mode().Perm())
	}

	// Creating again should not overwrite existing file
	newContent := []byte(`{"test": false}`)
	err = EnsureFile(testFile, newContent, 0644)
	if err != nil {
		t.Errorf("EnsureFile on existing file failed: %v", err)
	}

	// Verify content wasn't changed
	data, err = os.ReadFile(testFile)
	if err != nil {
		t.Errorf("Failed to read file: %v", err)
	} else if string(data) != string(content) {
		t.Errorf("File was overwritten when it shouldn't be: got %s, want %s", string(data), string(content))
	}
}

func TestGenerateMCPConfig(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create config manager
	cm := NewConfigManager(tempDir)

	// Generate MCP config
	err := cm.GenerateMCPConfig()
	if err != nil {
		t.Fatalf("GenerateMCPConfig failed: %v", err)
	}

	// Verify mcp.json exists
	mcpPath := cm.GetMCPConfigPath()
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("mcp.json not created: %v", err)
	}

	// Verify it's valid JSON
	var mcpConfig MCPConfig
	if err := json.Unmarshal(data, &mcpConfig); err != nil {
		t.Fatalf("mcp.json is not valid JSON: %v", err)
	}

	// Verify version
	if mcpConfig.Version != "1.0" {
		t.Errorf("Expected version '1.0', got '%s'", mcpConfig.Version)
	}

	// Verify servers map exists
	if mcpConfig.Servers == nil {
		t.Fatal("mcp.json missing 'servers' map")
	}

	// Verify gopls configuration
	goServer, ok := mcpConfig.Servers["go"]
	if !ok {
		t.Error("mcp.json missing 'go' server configuration")
	} else {
		if goServer.Command != "gopls" {
			t.Errorf("Expected go server command 'gopls', got '%s'", goServer.Command)
		}
		if !goServer.Enabled {
			t.Error("Expected go server to be enabled")
		}
		expectedArgs := []string{"serve"}
		if len(goServer.Args) != len(expectedArgs) || (len(expectedArgs) > 0 && goServer.Args[0] != expectedArgs[0]) {
			t.Errorf("Expected go server args %v, got %v", expectedArgs, goServer.Args)
		}
	}

	// Verify typescript-language-server configuration
	tsServer, ok := mcpConfig.Servers["typescript"]
	if !ok {
		t.Error("mcp.json missing 'typescript' server configuration")
	} else {
		if tsServer.Command != "typescript-language-server" {
			t.Errorf("Expected typescript server command 'typescript-language-server', got '%s'", tsServer.Command)
		}
		if !tsServer.Enabled {
			t.Error("Expected typescript server to be enabled")
		}
		expectedArgs := []string{"--stdio"}
		if len(tsServer.Args) != len(expectedArgs) || (len(expectedArgs) > 0 && tsServer.Args[0] != expectedArgs[0]) {
			t.Errorf("Expected typescript server args %v, got %v", expectedArgs, tsServer.Args)
		}
	}
}

func TestGenerateMCPConfig_ValidatesJSON(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create config manager
	cm := NewConfigManager(tempDir)

	// Generate MCP config
	err := cm.GenerateMCPConfig()
	if err != nil {
		t.Fatalf("GenerateMCPConfig failed: %v", err)
	}

	// Read and validate JSON structure
	mcpPath := cm.GetMCPConfigPath()
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}

	// Validate with generic map to ensure structure is correct
	var genericConfig map[string]interface{}
	if err := json.Unmarshal(data, &genericConfig); err != nil {
		t.Errorf("Generated JSON is not valid: %v", err)
	}

	// Verify top-level fields
	if version, ok := genericConfig["version"].(string); !ok || version != "1.0" {
		t.Error("Invalid or missing 'version' field")
	}

	servers, ok := genericConfig["servers"].(map[string]interface{})
	if !ok {
		t.Fatal("Invalid or missing 'servers' field")
	}

	// Verify go server
	if goServer, ok := servers["go"].(map[string]interface{}); ok {
		if cmd, ok := goServer["command"].(string); !ok || cmd != "gopls" {
			t.Error("Invalid go server command")
		}
		if enabled, ok := goServer["enabled"].(bool); !ok || !enabled {
			t.Error("Go server should be enabled")
		}
	} else {
		t.Error("Missing go server configuration")
	}

	// Verify typescript server
	if tsServer, ok := servers["typescript"].(map[string]interface{}); ok {
		if cmd, ok := tsServer["command"].(string); !ok || cmd != "typescript-language-server" {
			t.Error("Invalid typescript server command")
		}
		if enabled, ok := tsServer["enabled"].(bool); !ok || !enabled {
			t.Error("Typescript server should be enabled")
		}
	} else {
		t.Error("Missing typescript server configuration")
	}
}

func TestGenerateMCPConfig_PrettyJSON(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create config manager
	cm := NewConfigManager(tempDir)

	// Generate MCP config
	err := cm.GenerateMCPConfig()
	if err != nil {
		t.Fatalf("GenerateMCPConfig failed: %v", err)
	}

	// Read the file
	mcpPath := cm.GetMCPConfigPath()
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}

	// Verify it's pretty-printed (should contain newlines and indentation)
	content := string(data)
	if !contains(content, "\n") {
		t.Error("Expected pretty-printed JSON with newlines")
	}
	if !contains(content, "  ") {
		t.Error("Expected pretty-printed JSON with indentation")
	}
}

// Helper function for string contains
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
