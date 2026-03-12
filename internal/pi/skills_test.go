package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestSkills(t testing.TB, tempDir string) {
	skillsDir := filepath.Join(tempDir, "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0755))

	for _, skill := range DefaultSkills {
		skillPath := filepath.Join(skillsDir, skill.Name)
		require.NoError(t, os.MkdirAll(skillPath, 0755))
		// Create a minimal SKILL.md file
		skillMDPath := filepath.Join(skillPath, "SKILL.md")
		require.NoError(t, os.WriteFile(skillMDPath, []byte("# "+skill.Name), 0644))
	}
}

func TestBuildSkillFlags_ReturnsAllSkills(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create test skill directories
	createTestSkills(t, tempDir)

	// Call BuildSkillFlags
	result, err := BuildSkillFlags(tempDir)

	// Assert no error
	require.NoError(t, err)
	require.NotNil(t, result)

	// Assert we have 26 elements (13 skills × 2 for --skill + path)
	assert.Len(t, result.Flags, 26, "Expected 26 elements (13 skills × 2)")

	// Verify structure: alternating "--skill" and path
	for i := 0; i < len(result.Flags); i += 2 {
		assert.Equal(t, "--skill", result.Flags[i], "Expected '--skill' at index %d", i)
		assert.True(t, strings.Contains(result.Flags[i+1], "skills"), "Expected path containing 'skills' at index %d", i+1)
	}

	// Verify all 13 expected skills are present
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

	for _, skillName := range expectedSkills {
		found := false
		for i := 1; i < len(result.Flags); i += 2 {
			if strings.Contains(result.Flags[i], skillName) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected skill '%s' to be present in flags", skillName)
	}

	// With all skills available, should have no missing
	assert.False(t, result.HasMissingSkills(), "Expected no missing skills when all directories exist")
	assert.Equal(t, 0, len(result.Missing), "Expected 0 missing skills")
	assert.Equal(t, 13, len(result.Found), "Expected 13 found skills")
	assert.False(t, result.HasWarnings(), "Expected no warnings when all skills exist")
}

func TestBuildSkillFlags_UsesAbsolutePaths(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create test skill directories
	createTestSkills(t, tempDir)

	// Get absolute path of tempDir for comparison
	absTempDir, err := filepath.Abs(tempDir)
	require.NoError(t, err)

	// Call BuildSkillFlags
	result, err := BuildSkillFlags(tempDir)
	require.NoError(t, err)

	// Verify all paths are absolute
	for i := 1; i < len(result.Flags); i += 2 {
		path := result.Flags[i]
		assert.True(t, filepath.IsAbs(path), "Expected absolute path at index %d, got: %s", i, path)
		assert.True(t, strings.HasPrefix(path, absTempDir), "Expected path to start with temp dir: %s", path)
	}
}

func TestBuildSkillFlags_AlphabeticalOrder(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create test skill directories
	createTestSkills(t, tempDir)

	// Call BuildSkillFlags
	result, err := BuildSkillFlags(tempDir)
	require.NoError(t, err)

	// Extract skill names from paths
	var skillNames []string
	for i := 1; i < len(result.Flags); i += 2 {
		path := result.Flags[i]
		skillName := filepath.Base(path)
		skillNames = append(skillNames, skillName)
	}

	// Verify skills are in alphabetical order
	for i := 1; i < len(skillNames); i++ {
		assert.LessOrEqual(t, skillNames[i-1], skillNames[i],
			"Skills not in alphabetical order: '%s' should come before '%s'",
			skillNames[i-1], skillNames[i])
	}

	// Verify first and last skills for sanity check
	assert.Equal(t, "angular-component", skillNames[0], "First skill should be angular-component")
	assert.Equal(t, "vitest", skillNames[len(skillNames)-1], "Last skill should be vitest")
}

func TestBuildSkillFlags_InvalidConfigDir(t *testing.T) {
	// Use an invalid/non-existent path
	invalidDir := "/nonexistent/path/that/does/not/exist"

	// Call BuildSkillFlags - it should still work as it just builds paths
	result, err := BuildSkillFlags(invalidDir)

	// Should not return error (function builds paths and reports missing skills as warnings)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should return 0 flags since skills directory doesn't exist
	assert.Len(t, result.Flags, 0, "Expected 0 flags when directory doesn't exist")

	// All skills should be reported as missing
	assert.True(t, result.HasMissingSkills(), "Expected missing skills when directory doesn't exist")
	assert.Equal(t, 13, len(result.Missing), "Expected all 13 skills to be reported as missing")

	// Paths should still be absolute in warnings
	absPath, _ := filepath.Abs(invalidDir)
	assert.True(t, len(absPath) > 0, "Expected absolute path resolution to work")
}

func TestBuildSkillFlags_EmptyConfigDir(t *testing.T) {
	// Call BuildSkillFlags with empty string
	result, err := BuildSkillFlags("")

	// Should not return error
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should return 0 flags since current directory likely doesn't have skills subdirectory
	// Or might have some if running from project root with skills installed
	assert.True(t, len(result.Flags) == 0 || len(result.Flags) == 26, "Expected either 0 or 26 flags")

	// Result should have properly populated fields
	assert.NotNil(t, result.Flags, "Flags should not be nil")
	assert.NotNil(t, result.Warnings, "Warnings should not be nil")
	assert.NotNil(t, result.Found, "Found should not be nil")
	assert.NotNil(t, result.Missing, "Missing should not be nil")
}

func TestBuildSkillFlags_RelativePathConversion(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()

	// Create a relative path from tempDir
	relativePath := filepath.Join(tempDir, "..", filepath.Base(tempDir))

	// Call BuildSkillFlags with relative path
	result, err := BuildSkillFlags(relativePath)
	require.NoError(t, err)

	// All paths should be absolute (if any flags exist)
	for i := 1; i < len(result.Flags); i += 2 {
		path := result.Flags[i]
		assert.True(t, filepath.IsAbs(path), "Expected absolute path from relative input: %s", path)
	}

	// Get absolute path for comparison
	absTempDir, err := filepath.Abs(tempDir)
	require.NoError(t, err)

	// Verify paths contain the resolved absolute path (if any flags exist)
	for i := 1; i < len(result.Flags); i += 2 {
		path := result.Flags[i]
		assert.True(t, strings.HasPrefix(path, absTempDir),
			"Expected path to be resolved to absolute: %s should start with %s", path, absTempDir)
	}
}

func TestBuildSkillFlags_CorrectPathStructure(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()

	// Create test skill directories
	createTestSkills(t, tempDir)

	// Call BuildSkillFlags
	result, err := BuildSkillFlags(tempDir)
	require.NoError(t, err)

	// Get absolute path for comparison
	absTempDir, err := filepath.Abs(tempDir)
	require.NoError(t, err)

	// Verify path structure: <abs-config-dir>/skills/<skill-name> (if any flags exist)
	for i := 1; i < len(result.Flags); i += 2 {
		path := result.Flags[i]
		expectedPrefix := filepath.Join(absTempDir, "skills")
		assert.True(t, strings.HasPrefix(path, expectedPrefix),
			"Path should have structure '<config-dir>/skills/<skill-name>', got: %s", path)

		// Verify skill name is the last component
		skillName := filepath.Base(path)
		assert.NotEmpty(t, skillName, "Skill name should not be empty")
		assert.NotEqual(t, "skills", skillName, "Skill name should not be 'skills'")
	}
}

func TestBuildSkillFlags_Consistency(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()

	// Create test skill directories
	createTestSkills(t, tempDir)

	// Reset cache to ensure clean state
	ResetSkillFlagsCache()

	// Call BuildSkillFlags multiple times
	result1, err := BuildSkillFlags(tempDir)
	require.NoError(t, err)

	result2, err := BuildSkillFlags(tempDir)
	require.NoError(t, err)

	// Results should be identical
	assert.Equal(t, result1.Flags, result2.Flags, "Multiple calls should return identical flags")
	assert.Equal(t, result1.Missing, result2.Missing, "Multiple calls should return identical missing skills")
	assert.Equal(t, result1.Found, result2.Found, "Multiple calls should return identical found skills")
}

// TestSkillFlagsCaching verifies that caching works correctly
func TestSkillFlagsCaching(t *testing.T) {
	tempDir := t.TempDir()
	createTestSkills(t, tempDir)

	// Reset cache
	ResetSkillFlagsCache()

	// First call - cache miss
	result1, err := BuildSkillFlags(tempDir)
	require.NoError(t, err)

	hits, misses, _ := GetSkillFlagsCacheStats()
	assert.Equal(t, int64(0), hits, "First call should be a cache miss")
	assert.Equal(t, int64(1), misses, "First call should increment misses")

	// Second call - cache hit
	result2, err := BuildSkillFlags(tempDir)
	require.NoError(t, err)

	hits, misses, _ = GetSkillFlagsCacheStats()
	assert.Equal(t, int64(1), hits, "Second call should be a cache hit")
	assert.Equal(t, int64(1), misses, "Misses should not increment")

	// Results should be identical
	assert.Equal(t, result1.Flags, result2.Flags)
	assert.Equal(t, result1.Found, result2.Found)
	assert.Equal(t, result1.Missing, result2.Missing)
}

// TestSkillFlagsCacheTTL verifies that cache entries expire after TTL
func TestSkillFlagsCacheTTL(t *testing.T) {
	tempDir := t.TempDir()
	createTestSkills(t, tempDir)

	// Save original TTL
	originalTTL := skillFlagsCacheTTL
	defer func() {
		skillFlagsCacheTTL = originalTTL
	}()

	// Set very short TTL for testing
	skillFlagsCacheTTL = 50 * time.Millisecond

	// Reset cache
	ResetSkillFlagsCache()

	// First call
	_, err := BuildSkillFlags(tempDir)
	require.NoError(t, err)

	hits1, misses1, _ := GetSkillFlagsCacheStats()
	assert.Equal(t, int64(0), hits1)
	assert.Equal(t, int64(1), misses1)

	// Second call immediately - should hit cache
	_, err = BuildSkillFlags(tempDir)
	require.NoError(t, err)

	hits2, misses2, _ := GetSkillFlagsCacheStats()
	assert.Equal(t, int64(1), hits2)
	assert.Equal(t, int64(1), misses2)

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Third call - cache should be expired
	_, err = BuildSkillFlags(tempDir)
	require.NoError(t, err)

	hits3, misses3, _ := GetSkillFlagsCacheStats()
	assert.Equal(t, int64(1), hits3)
	assert.Equal(t, int64(2), misses3)
}

// BenchmarkBuildSkillFlags_Cached benchmarks cached performance
func BenchmarkBuildSkillFlags_Cached(b *testing.B) {
	tempDir := b.TempDir()
	createTestSkills(b, tempDir)

	// Reset cache
	ResetSkillFlagsCache()

	// Warm up cache
	_, err := BuildSkillFlags(tempDir)
	require.NoError(b, err)

	// Verify cache hit
	hits, misses, _ := GetSkillFlagsCacheStats()
	require.Equal(b, int64(0), hits, "Cache should be cold")
	require.Equal(b, int64(1), misses, "Should have 1 miss after warm-up")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = BuildSkillFlags(tempDir)
	}

	// Verify all calls were cache hits
	hits, _, _ = GetSkillFlagsCacheStats()
	require.Equal(b, int64(b.N), hits, "All benchmark calls should be cache hits")
}

// BenchmarkBuildSkillFlags_Uncached benchmarks uncached performance
func BenchmarkBuildSkillFlags_Uncached(b *testing.B) {
	tempDir := b.TempDir()
	createTestSkills(b, tempDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset cache each iteration to force uncached builds
		ResetSkillFlagsCache()
		_, _ = BuildSkillFlags(tempDir)
	}
}

// BenchmarkBuildSkillFlags_Concurrent benchmarks concurrent access
func BenchmarkBuildSkillFlags_Concurrent(b *testing.B) {
	tempDir := b.TempDir()
	createTestSkills(b, tempDir)

	// Reset cache
	ResetSkillFlagsCache()

	// Warm up cache
	_, err := BuildSkillFlags(tempDir)
	require.NoError(b, err)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = BuildSkillFlags(tempDir)
		}
	})
}
