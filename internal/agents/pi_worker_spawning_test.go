package agents

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/pi"
	"github.com/haulmatic-dev/hydra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockPiConnector is a test helper that captures command construction without executing
// It wraps PiConnector and provides hooks to inspect the built command
type MockPiConnector struct {
	*PiConnector
	lastBuiltArgs []string
	lastEnvVars   map[string]string
	configDir     string
}

// NewMockPiConnector creates a mock connector for testing command construction
func NewMockPiConnector(config PiConfig) *MockPiConnector {
	return &MockPiConnector{
		PiConnector: NewPiConnector(config, log.Default()),
		configDir:   config.ConfigDir,
		lastEnvVars: make(map[string]string),
	}
}

// CaptureCommandBuild simulates the command building logic from Execute()
// without actually running the command, allowing us to verify construction
func (m *MockPiConnector) CaptureCommandBuild(session *AgentSession) (*pi.SkillFlagsResult, []string, map[string]string, error) {
	args := []string{
		"--mode", "rpc",
		"--no-session",
	}

	if m.provider != "" {
		args = append(args, "--provider", m.provider)
	}

	if m.model != "" {
		args = append(args, "--model", m.model)
	}

	var skillResult *pi.SkillFlagsResult
	var skillErr error

	// Add skill flags if configDir is set (mirrors Execute() logic)
	if m.configDir != "" {
		skillResult, skillErr = pi.BuildSkillFlags(m.configDir)
		if skillErr != nil {
			// In real Execute(), this just logs a warning
			log.Printf("[Test] Warning: failed to build skill flags: %v", skillErr)
		} else {
			args = append(args, skillResult.Flags...)
		}
	}

	// Build environment variables (mirrors Execute() logic)
	envVars := make(map[string]string)

	// Set PI_CODING_AGENT_DIR if configDir is set
	if m.configDir != "" {
		agentDir := filepath.Join(m.configDir, "agent")
		absAgentDir, err := filepath.Abs(agentDir)
		if err == nil {
			envVars["PI_CODING_AGENT_DIR"] = absAgentDir
		} else {
			envVars["PI_CODING_AGENT_DIR"] = agentDir
		}
	}

	// Set HYDRA_BEAD_TITLE and HYDRA_BEAD_DESCRIPTION from session metadata
	if title, ok := session.metadata["bead_title"].(string); ok && title != "" {
		envVars["HYDRA_BEAD_TITLE"] = title
	}

	if description, ok := session.metadata["bead_description"].(string); ok && description != "" {
		envVars["HYDRA_BEAD_DESCRIPTION"] = description
	}

	m.lastBuiltArgs = args
	m.lastEnvVars = envVars

	return skillResult, args, envVars, skillErr
}

// createAllTestSkills creates a directory + SKILL.md for every skill in
// pi.DefaultSkills. Iterating the source of truth keeps this helper from going
// stale as the default skill set changes.
func createAllTestSkills(t testing.TB, configDir string) {
	skillsDir := filepath.Join(configDir, "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0755))

	for _, skill := range pi.DefaultSkills {
		skillPath := filepath.Join(skillsDir, skill.Name)
		require.NoError(t, os.MkdirAll(skillPath, 0755))
		skillMDPath := filepath.Join(skillPath, "SKILL.md")
		require.NoError(t, os.WriteFile(skillMDPath, []byte("# "+skill.Name+"\n\nTest skill content"), 0644))
	}
}

// createPartialTestSkills creates only some skills to test missing skill handling
func createPartialTestSkills(t testing.TB, configDir string, skillsToCreate []string) {
	skillsDir := filepath.Join(configDir, "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0755))

	for _, skill := range skillsToCreate {
		skillPath := filepath.Join(skillsDir, skill)
		require.NoError(t, os.MkdirAll(skillPath, 0755))
		skillMDPath := filepath.Join(skillPath, "SKILL.md")
		require.NoError(t, os.WriteFile(skillMDPath, []byte("# "+skill), 0644))
	}
}

// TestWorkerSpawning_AllSkillsPassed verifies all 13 skills are included in command flags
func TestWorkerSpawning_AllSkillsPassed(t *testing.T) {
	tempDir := t.TempDir()
	createAllTestSkills(t, tempDir)

	// Create agent subdirectory
	agentDir := filepath.Join(tempDir, "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0755))

	config := PiConfig{
		Provider:  "anthropic",
		Model:     "claude-3-5-sonnet",
		ConfigDir: tempDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:          "test-bead-001",
		Title:       "Test Bead",
		Description: "Test description",
	}

	opts := AgentOptions{ProjectPath: tempDir}
	ctx := context.Background()

	// Start session
	session, err := connector.Start(ctx, bead, opts)
	require.NoError(t, err)
	require.NotNil(t, session)

	// Capture command build
	skillResult, args, _, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err)
	require.NotNil(t, skillResult)

	// Verify all 13 skills are found
	assert.Equal(t, len(pi.DefaultSkills), len(skillResult.Found), "Expected every default skill to be found")
	assert.Equal(t, 0, len(skillResult.Missing), "Expected 0 missing skills")
	assert.False(t, skillResult.HasMissingSkills(), "Should not have missing skills")

	// Verify 26 flags (13 skills × 2: --skill + path)
	assert.Equal(t, len(pi.DefaultSkills)*2, len(skillResult.Flags), "Expected 2 flag elements per default skill")

	// Verify args includes all skill flags
	// Base args: --mode, rpc, --no-session, --provider, anthropic, --model, claude-3-5-sonnet
	// Plus 26 skill flags = 33 total
	assert.True(t, len(args) >= len(pi.DefaultSkills)*2, "Args should include skill flags")

	// Count --skill flags
	skillFlagCount := 0
	for _, arg := range args {
		if arg == "--skill" {
			skillFlagCount++
		}
	}
	assert.Equal(t, len(pi.DefaultSkills), skillFlagCount, "Expected one --skill flag per default skill")

	// Verify all expected skills are present
	expectedSkills := []string{
		"angular-component",
		"angular-signals",
		"angular-testing",
		"code-review",
		"golang-patterns",
		"golang-pro",
		"golang-testing",
		"implement-design",
		"lsp-code-analysis",
		"systematic-debugging",
		"test-driven-development",
		"typescript-advanced-types",
		"vitest",
	}

	for _, expectedSkill := range expectedSkills {
		found := false
		for _, flag := range skillResult.Flags {
			if strings.Contains(flag, expectedSkill) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected skill '%s' to be in flags", expectedSkill)
	}
}

// TestWorkerSpawning_EnvVarsSet verifies PI_CODING_AGENT_DIR and other env vars are set correctly
func TestWorkerSpawning_EnvVarsSet(t *testing.T) {
	tempDir := t.TempDir()
	createAllTestSkills(t, tempDir)

	agentDir := filepath.Join(tempDir, "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0755))

	config := PiConfig{
		Provider:  "anthropic",
		Model:     "claude-3-5-sonnet",
		ConfigDir: tempDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:          "test-bead-002",
		Title:       "Test Bead With Title",
		Description: "Test bead description",
	}

	opts := AgentOptions{ProjectPath: tempDir}
	ctx := context.Background()

	session, err := connector.Start(ctx, bead, opts)
	require.NoError(t, err)
	require.NotNil(t, session)

	_, _, envVars, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err)

	// Verify PI_CODING_AGENT_DIR is set
	agentDirEnv, exists := envVars["PI_CODING_AGENT_DIR"]
	assert.True(t, exists, "PI_CODING_AGENT_DIR should be set")
	assert.NotEmpty(t, agentDirEnv, "PI_CODING_AGENT_DIR should not be empty")

	// Verify it's an absolute path
	assert.True(t, filepath.IsAbs(agentDirEnv), "PI_CODING_AGENT_DIR should be absolute path")

	// Verify it points to the agent subdirectory
	expectedSuffix := filepath.Join(tempDir, "agent")
	assert.True(t, strings.HasSuffix(agentDirEnv, expectedSuffix),
		"PI_CODING_AGENT_DIR should end with '%s', got: %s", expectedSuffix, agentDirEnv)

	// Verify HYDRA_BEAD_TITLE is set
	titleEnv, exists := envVars["HYDRA_BEAD_TITLE"]
	assert.True(t, exists, "HYDRA_BEAD_TITLE should be set")
	assert.Equal(t, "Test Bead With Title", titleEnv)

	// Verify HYDRA_BEAD_DESCRIPTION is set
	descEnv, exists := envVars["HYDRA_BEAD_DESCRIPTION"]
	assert.True(t, exists, "HYDRA_BEAD_DESCRIPTION should be set")
	assert.Equal(t, "Test bead description", descEnv)
}

// TestWorkerSpawning_MissingSkills verifies graceful handling when skills are missing
func TestWorkerSpawning_MissingSkills(t *testing.T) {
	tempDir := t.TempDir()

	// Create only 5 skills, leaving 11 missing
	partialSkills := []string{
		"golang-pro",
		"golang-testing",
		"code-review",
		"systematic-debugging",
		"test-driven-development",
	}
	createPartialTestSkills(t, tempDir, partialSkills)

	agentDir := filepath.Join(tempDir, "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0755))

	config := PiConfig{
		ConfigDir: tempDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:    "test-bead-003",
		Title: "Test Bead",
	}

	opts := AgentOptions{ProjectPath: tempDir}
	ctx := context.Background()

	session, err := connector.Start(ctx, bead, opts)
	require.NoError(t, err)
	require.NotNil(t, session)

	skillResult, args, _, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err) // Should not error, just report warnings
	require.NotNil(t, skillResult)

	// Verify only 5 skills are found
	assert.Equal(t, 5, len(skillResult.Found), "Expected 5 found skills")
	assert.Equal(t, len(pi.DefaultSkills)-5, len(skillResult.Missing), "Expected every default skill beyond the 5 created to be missing")
	assert.True(t, skillResult.HasMissingSkills(), "Should have missing skills")
	assert.True(t, skillResult.HasWarnings(), "Should have warnings")

	// Verify 10 flags (5 skills × 2: --skill + path)
	assert.Equal(t, 10, len(skillResult.Flags), "Expected 10 skill flags for 5 skills")

	// Verify missing skills are tracked
	expectedMissing := []string{
		"angular-component",
		"angular-signals",
		"angular-testing",
		"golang-patterns",
		"implement-design",
		"lsp-code-analysis",
		"typescript-advanced-types",
		"vitest",
	}

	for _, missingSkill := range expectedMissing {
		assert.Contains(t, skillResult.Missing, missingSkill,
			"Expected '%s' to be in missing skills", missingSkill)
	}

	// Verify command still includes the available skills
	skillFlagCount := 0
	for _, arg := range args {
		if arg == "--skill" {
			skillFlagCount++
		}
	}
	assert.Equal(t, 5, skillFlagCount, "Expected 5 --skill flags for available skills")

	// Verify warnings include missing skills
	assert.Equal(t, len(pi.DefaultSkills)-5, len(skillResult.Warnings), "Expected one warning per missing default skill")
}

// TestWorkerSpawning_FailFast verifies fail-fast behavior when enabled
func TestWorkerSpawning_FailFast(t *testing.T) {
	tempDir := t.TempDir()

	// Create only 3 skills
	partialSkills := []string{
		"golang-pro",
		"code-review",
		"systematic-debugging",
	}
	createPartialTestSkills(t, tempDir, partialSkills)

	// Test with failOnMissing = false (default behavior)
	result1, err := pi.BuildSkillFlagsWithOptions(tempDir, false)
	require.NoError(t, err, "Should not error with failOnMissing=false")
	assert.Equal(t, 3, len(result1.Found))
	assert.Equal(t, len(pi.DefaultSkills)-3, len(result1.Missing))

	// Test with failOnMissing = true
	result2, err := pi.BuildSkillFlagsWithOptions(tempDir, true)
	require.Error(t, err, "Should error with failOnMissing=true when skills are missing")
	assert.Contains(t, err.Error(), "skill(s) missing")
	assert.Contains(t, err.Error(), "failOnMissing is enabled")
	require.NotNil(t, result2)

	// Result should still contain information about what was found/missing
	assert.Equal(t, 3, len(result2.Found))
	assert.Equal(t, len(pi.DefaultSkills)-3, len(result2.Missing))
	assert.True(t, result2.FailOnMissing)
}

// TestWorkerSpawning_PathConstruction verifies correct path format in skill flags
func TestWorkerSpawning_PathConstruction(t *testing.T) {
	tempDir := t.TempDir()
	createAllTestSkills(t, tempDir)

	config := PiConfig{
		ConfigDir: tempDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:    "test-bead-004",
		Title: "Test Bead",
	}

	opts := AgentOptions{ProjectPath: tempDir}
	ctx := context.Background()

	session, err := connector.Start(ctx, bead, opts)
	require.NoError(t, err)

	skillResult, _, _, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err)
	require.NotNil(t, skillResult)

	// Get absolute path for comparison
	absTempDir, err := filepath.Abs(tempDir)
	require.NoError(t, err)

	// Verify each skill flag has correct structure
	for i := 0; i < len(skillResult.Flags); i += 2 {
		// Even indices should be "--skill"
		assert.Equal(t, "--skill", skillResult.Flags[i], "Expected '--skill' at index %d", i)

		// Odd indices should be the path
		path := skillResult.Flags[i+1]

		// Path should be absolute
		assert.True(t, filepath.IsAbs(path), "Path should be absolute: %s", path)

		// Path should contain the skills directory
		assert.True(t, strings.Contains(path, "skills"), "Path should contain 'skills': %s", path)

		// Path should start with the config directory
		assert.True(t, strings.HasPrefix(path, absTempDir),
			"Path should start with config dir. Got: %s, Expected prefix: %s", path, absTempDir)

		// Verify path structure: <config-dir>/skills/<skill-name>
		expectedPrefix := filepath.Join(absTempDir, "skills")
		assert.True(t, strings.HasPrefix(path, expectedPrefix),
			"Path should have structure '<config-dir>/skills/<skill-name>'. Got: %s", path)

		// Verify skill name is the last component
		skillName := filepath.Base(path)
		assert.NotEmpty(t, skillName)
		assert.NotEqual(t, "skills", skillName, "Skill name should not be 'skills'")

		// Verify the skill directory actually exists
		assert.DirExists(t, path, "Skill directory should exist: %s", path)

		// Verify SKILL.md exists in the directory
		skillMDPath := filepath.Join(path, "SKILL.md")
		assert.FileExists(t, skillMDPath, "SKILL.md should exist in skill directory")
	}
}

// TestWorkerSpawning_SkillOrder verifies consistent alphabetical ordering
func TestWorkerSpawning_SkillOrder(t *testing.T) {
	tempDir := t.TempDir()
	createAllTestSkills(t, tempDir)

	config := PiConfig{
		ConfigDir: tempDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:    "test-bead-005",
		Title: "Test Bead",
	}

	opts := AgentOptions{ProjectPath: tempDir}
	ctx := context.Background()

	session, err := connector.Start(ctx, bead, opts)
	require.NoError(t, err)

	// Build skill flags multiple times to verify consistency
	skillResult1, _, _, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err)

	skillResult2, _, _, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err)

	// Extract skill names from paths
	extractSkillNames := func(result *pi.SkillFlagsResult) []string {
		names := make([]string, 0)
		for i := 1; i < len(result.Flags); i += 2 {
			path := result.Flags[i]
			skillName := filepath.Base(path)
			names = append(names, skillName)
		}
		return names
	}

	names1 := extractSkillNames(skillResult1)
	names2 := extractSkillNames(skillResult2)

	// Verify consistency across multiple calls
	assert.Equal(t, names1, names2, "Skill order should be consistent across calls")

	// Verify alphabetical order
	sortedNames := make([]string, len(names1))
	copy(sortedNames, names1)
	sort.Strings(sortedNames)
	assert.Equal(t, sortedNames, names1, "Skills should be in alphabetical order")

	// Verify first and last skills
	assert.Equal(t, "angular-component", names1[0], "First skill should be angular-component")
	assert.Equal(t, "vitest", names1[len(names1)-1], "Last skill should be vitest")
}

// TestWorkerSpawning_InvalidConfigDir verifies error handling for invalid config directories
func TestWorkerSpawning_InvalidConfigDir(t *testing.T) {
	// Test with non-existent directory
	nonExistentDir := "/nonexistent/path/that/does/not/exist"

	config := PiConfig{
		ConfigDir: nonExistentDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:    "test-bead-006",
		Title: "Test Bead",
	}

	opts := AgentOptions{ProjectPath: "/tmp"}
	ctx := context.Background()

	session, err := connector.Start(ctx, bead, opts)
	require.NoError(t, err)
	require.NotNil(t, session)

	skillResult, args, envVars, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err) // Should not error, just report missing skills
	require.NotNil(t, skillResult)

	// All skills should be missing
	assert.Equal(t, len(pi.DefaultSkills), len(skillResult.Missing), "Expected every default skill to be missing")
	assert.Equal(t, 0, len(skillResult.Found), "Expected 0 found skills")
	assert.True(t, skillResult.HasMissingSkills())
	assert.True(t, skillResult.HasWarnings())

	// No skill flags should be generated
	assert.Equal(t, 0, len(skillResult.Flags), "Expected 0 skill flags for non-existent directory")

	// Args should only contain base flags, no --skill flags
	skillFlagCount := 0
	for _, arg := range args {
		if arg == "--skill" {
			skillFlagCount++
		}
	}
	assert.Equal(t, 0, skillFlagCount, "Expected no --skill flags")

	// PI_CODING_AGENT_DIR should still be set (based on configDir)
	agentDirEnv, exists := envVars["PI_CODING_AGENT_DIR"]
	assert.True(t, exists, "PI_CODING_AGENT_DIR should still be set even for invalid config")
	assert.NotEmpty(t, agentDirEnv)

	// Test with empty configDir
	configEmpty := PiConfig{
		ConfigDir: "",
	}
	connectorEmpty := NewMockPiConnector(configEmpty)

	sessionEmpty, err := connectorEmpty.Start(ctx, bead, opts)
	require.NoError(t, err)

	skillResultEmpty, argsEmpty, envVarsEmpty, err := connectorEmpty.CaptureCommandBuild(sessionEmpty)
	require.NoError(t, err)

	// With empty configDir, no skills should be loaded
	assert.Nil(t, skillResultEmpty, "Should not build skill flags when configDir is empty")

	// Args should only contain base flags
	assert.True(t, len(argsEmpty) < 10, "Args should be minimal without skills")

	// PI_CODING_AGENT_DIR should not be set
	_, exists = envVarsEmpty["PI_CODING_AGENT_DIR"]
	assert.False(t, exists, "PI_CODING_AGENT_DIR should not be set when configDir is empty")
}

// TestWorkerSpawning_WithProviderAndModel verifies command includes provider and model flags
func TestWorkerSpawning_WithProviderAndModel(t *testing.T) {
	tempDir := t.TempDir()
	createAllTestSkills(t, tempDir)

	tests := []struct {
		name           string
		provider       string
		model          string
		expectProvider bool
		expectModel    bool
	}{
		{
			name:           "with provider and model",
			provider:       "anthropic",
			model:          "claude-3-5-sonnet",
			expectProvider: true,
			expectModel:    true,
		},
		{
			name:           "with provider only",
			provider:       "openai",
			expectProvider: true,
			expectModel:    false,
		},
		{
			name:           "with model only",
			model:          "gpt-4",
			expectProvider: false,
			expectModel:    true,
		},
		{
			name:           "without provider or model",
			expectProvider: false,
			expectModel:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := PiConfig{
				Provider:  tt.provider,
				Model:     tt.model,
				ConfigDir: tempDir,
			}

			connector := NewMockPiConnector(config)

			bead := types.Bead{
				ID:    types.BeadID("test-bead-" + tt.name),
				Title: "Test",
			}

			opts := AgentOptions{ProjectPath: tempDir}
			ctx := context.Background()

			session, err := connector.Start(ctx, bead, opts)
			require.NoError(t, err)

			_, args, _, err := connector.CaptureCommandBuild(session)
			require.NoError(t, err)

			// Check for --provider flag
			providerFound := false
			providerValue := ""
			for i, arg := range args {
				if arg == "--provider" && i+1 < len(args) {
					providerFound = true
					providerValue = args[i+1]
				}
			}

			if tt.expectProvider {
				assert.True(t, providerFound, "Expected --provider flag")
				assert.Equal(t, tt.provider, providerValue)
			} else {
				assert.False(t, providerFound, "Expected no --provider flag")
			}

			// Check for --model flag
			modelFound := false
			modelValue := ""
			for i, arg := range args {
				if arg == "--model" && i+1 < len(args) {
					modelFound = true
					modelValue = args[i+1]
				}
			}

			if tt.expectModel {
				assert.True(t, modelFound, "Expected --model flag")
				assert.Equal(t, tt.model, modelValue)
			} else {
				assert.False(t, modelFound, "Expected no --model flag")
			}

			// Verify base flags always present
			assert.Contains(t, args, "--mode")
			assert.Contains(t, args, "rpc")
			assert.Contains(t, args, "--no-session")
		})
	}
}

// TestWorkerSpawning_SessionMetadata verifies session metadata is properly propagated
func TestWorkerSpawning_SessionMetadata(t *testing.T) {
	tempDir := t.TempDir()
	createAllTestSkills(t, tempDir)

	config := PiConfig{
		Provider:  "anthropic",
		Model:     "claude-3-5-sonnet",
		ConfigDir: tempDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:          "test-bead-007",
		Title:       "My Test Bead Title",
		Description: "My Test Bead Description",
	}

	projectPath := "/path/to/project"
	opts := AgentOptions{ProjectPath: projectPath}
	ctx := context.Background()

	session, err := connector.Start(ctx, bead, opts)
	require.NoError(t, err)
	require.NotNil(t, session)

	// Verify session metadata
	assert.Equal(t, "hydra-test-bead-007", session.ID)
	assert.Equal(t, types.BeadID("test-bead-007"), session.BeadID)
	assert.Equal(t, projectPath, session.metadata["project"])
	assert.Equal(t, "My Test Bead Title", session.metadata["bead_title"])
	assert.Equal(t, "My Test Bead Description", session.metadata["bead_description"])
	assert.Equal(t, "anthropic", session.metadata["provider"])
	assert.Equal(t, "claude-3-5-sonnet", session.metadata["model"])

	// Verify env vars derived from metadata
	_, _, envVars, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err)

	assert.Equal(t, "My Test Bead Title", envVars["HYDRA_BEAD_TITLE"])
	assert.Equal(t, "My Test Bead Description", envVars["HYDRA_BEAD_DESCRIPTION"])
}

// TestWorkerSpawning_IntegrationStyle performs an integration-style test
// verifying the full flow without actually executing commands
func TestWorkerSpawning_IntegrationStyle(t *testing.T) {
	tempDir := t.TempDir()
	createAllTestSkills(t, tempDir)

	agentDir := filepath.Join(tempDir, "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0755))

	// Simulate full configuration
	config := PiConfig{
		Provider:      "anthropic",
		Model:         "claude-3-5-sonnet",
		ThinkingLevel: "high",
		Timeout:       "10m",
		ApiKeyName:    "ANTHROPIC_API_KEY",
		ConfigDir:     tempDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:          "integration-test-bead",
		Title:       "Integration Test Bead",
		Description: "Full integration test",
	}

	opts := AgentOptions{ProjectPath: "/workspace/project"}
	ctx := context.Background()

	// Start session
	session, err := connector.Start(ctx, bead, opts)
	require.NoError(t, err)
	require.NotNil(t, session)

	// Capture what would be executed
	skillResult, args, envVars, err := connector.CaptureCommandBuild(session)
	require.NoError(t, err)
	require.NotNil(t, skillResult)

	// Verify complete command structure
	t.Logf("Generated args: %v", args)
	t.Logf("Generated env vars: %v", envVars)

	// Verify base command structure
	assert.Contains(t, args, "--mode")
	assert.Contains(t, args, "rpc")
	assert.Contains(t, args, "--no-session")
	assert.Contains(t, args, "--provider")
	assert.Contains(t, args, "anthropic")
	assert.Contains(t, args, "--model")
	assert.Contains(t, args, "claude-3-5-sonnet")

	// Verify all skills included
	assert.Equal(t, len(pi.DefaultSkills), len(skillResult.Found))
	assert.Equal(t, 0, len(skillResult.Missing))

	// Count --skill occurrences
	skillCount := 0
	for _, arg := range args {
		if arg == "--skill" {
			skillCount++
		}
	}
	assert.Equal(t, len(pi.DefaultSkills), skillCount)

	// Verify environment variables
	assert.Contains(t, envVars, "PI_CODING_AGENT_DIR")
	assert.Contains(t, envVars, "HYDRA_BEAD_TITLE")
	assert.Contains(t, envVars, "HYDRA_BEAD_DESCRIPTION")

	// Verify paths are absolute
	for _, envVar := range []string{"PI_CODING_AGENT_DIR"} {
		assert.True(t, filepath.IsAbs(envVars[envVar]),
			"%s should be absolute: %s", envVar, envVars[envVar])
	}

	// Log summary
	t.Logf("✅ Integration test passed")
	t.Logf("   - Session ID: %s", session.ID)
	t.Logf("   - Skills passed: %d/%d", len(skillResult.Found), len(pi.DefaultSkills))
	t.Logf("   - Command args count: %d", len(args))
	t.Logf("   - Environment variables: %d", len(envVars))
}

// BenchmarkWorkerSpawning_CommandConstruction benchmarks the command construction
func BenchmarkWorkerSpawning_CommandConstruction(b *testing.B) {
	tempDir := b.TempDir()
	createAllTestSkills(b, tempDir)

	config := PiConfig{
		ConfigDir: tempDir,
	}

	connector := NewMockPiConnector(config)

	bead := types.Bead{
		ID:    "bench-bead",
		Title: "Benchmark Bead",
	}

	opts := AgentOptions{ProjectPath: tempDir}
	ctx := context.Background()

	session, err := connector.Start(ctx, bead, opts)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = connector.CaptureCommandBuild(session)
	}
}

// Example test demonstrating usage
func Example() {
	// This example demonstrates the expected usage pattern
	fmt.Println("TestWorkerSpawning_AllSkillsPassed - Verifies all 13 skills in flags")
	fmt.Println("TestWorkerSpawning_EnvVarsSet - Verifies PI_CODING_AGENT_DIR set")
	fmt.Println("TestWorkerSpawning_MissingSkills - Handles missing skills gracefully")
	fmt.Println("TestWorkerSpawning_FailFast - Optional fail-fast behavior")
	fmt.Println("TestWorkerSpawning_PathConstruction - Correct path format")
	fmt.Println("TestWorkerSpawning_SkillOrder - Consistent ordering")
	fmt.Println("TestWorkerSpawning_InvalidConfigDir - Error handling")
	// Output:
	// TestWorkerSpawning_AllSkillsPassed - Verifies all 13 skills in flags
	// TestWorkerSpawning_EnvVarsSet - Verifies PI_CODING_AGENT_DIR set
	// TestWorkerSpawning_MissingSkills - Handles missing skills gracefully
	// TestWorkerSpawning_FailFast - Optional fail-fast behavior
	// TestWorkerSpawning_PathConstruction - Correct path format
	// TestWorkerSpawning_SkillOrder - Consistent ordering
	// TestWorkerSpawning_InvalidConfigDir - Error handling
}
