package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkRunSetup benchmarks the setup performance
func BenchmarkRunSetup(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tempDir := b.TempDir()
		sm := NewSetupManager(tempDir, tempDir)
		b.StartTimer()

		_, err := sm.RunSetup()
		if err != nil {
			b.Fatalf("RunSetup failed: %v", err)
		}
	}
}

// BenchmarkRunSetupCached benchmarks setup with cache hit
func BenchmarkRunSetupCached(b *testing.B) {
	tempDir := b.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	// Run setup once to populate cache
	_, err := sm.RunSetup()
	if err != nil {
		b.Fatalf("Initial RunSetup failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sm.RunSetup()
		if err != nil {
			b.Fatalf("RunSetup failed: %v", err)
		}
	}
}

// BenchmarkCheckAndInstallLSP benchmarks LSP checking
func BenchmarkCheckAndInstallLSP(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tempDir := b.TempDir()
		sm := NewSetupManager(tempDir, tempDir)
		b.StartTimer()

		err := sm.CheckAndInstallLSP()
		// We expect errors in test environment, just measure the performance
		_ = err
	}
}

// BenchmarkCheckLSPInstallation benchmarks LSP installation checking
func BenchmarkCheckLSPInstallation(b *testing.B) {
	sm := NewSetupManager(b.TempDir(), b.TempDir())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.CheckLSPInstallation()
	}
}

// BenchmarkIsLSPInstalled benchmarks checking if all LSP packages are installed
func BenchmarkIsLSPInstalled(b *testing.B) {
	sm := NewSetupManager(b.TempDir(), b.TempDir())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.IsLSPInstalled()
	}
}

// BenchmarkGetInstalledLSPPackages benchmarks getting installed LSP packages
func BenchmarkGetInstalledLSPPackages(b *testing.B) {
	sm := NewSetupManager(b.TempDir(), b.TempDir())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.GetInstalledLSPPackages()
	}
}

// BenchmarkGetMissingLSPPackages benchmarks getting missing LSP packages
func BenchmarkGetMissingLSPPackages(b *testing.B) {
	sm := NewSetupManager(b.TempDir(), b.TempDir())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.GetMissingLSPPackages()
	}
}

// BenchmarkCheckAndInstallSkills benchmarks skills installation
func BenchmarkCheckAndInstallSkills(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tempDir := b.TempDir()
		sm := NewSetupManager(tempDir, tempDir)
		b.StartTimer()

		err := sm.CheckAndInstallSkills()
		// We expect errors in test environment, just measure the performance
		_ = err
	}
}

// BenchmarkVerifySkills benchmarks skills verification
func BenchmarkVerifySkills(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tempDir := b.TempDir()
		sm := NewSetupManager(tempDir, tempDir)
		b.StartTimer()

		_, _ = sm.VerifySkills()
	}
}

// BenchmarkBuildSkillFlags benchmarks building skill flags
func BenchmarkBuildSkillFlags(b *testing.B) {
	tempDir := b.TempDir()
	createTestSkillsForBenchmark(b, tempDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildSkillFlags(tempDir)
		if err != nil {
			b.Fatalf("BuildSkillFlags failed: %v", err)
		}
	}
}

// BenchmarkBuildSkillFlagsWithOptions benchmarks building skill flags with options
func BenchmarkBuildSkillFlagsWithOptions(b *testing.B) {
	tempDir := b.TempDir()
	createTestSkillsForBenchmark(b, tempDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildSkillFlagsWithOptions(tempDir, false)
		if err != nil {
			b.Fatalf("BuildSkillFlagsWithOptions failed: %v", err)
		}
	}
}

// BenchmarkAreSkillsInstalled benchmarks checking if skills are installed
func BenchmarkAreSkillsInstalled(b *testing.B) {
	tempDir := b.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	// Create tracking file
	trackingFile := filepath.Join(tempDir, "skills-installed.json")
	os.WriteFile(trackingFile, []byte(`{"skills_installed": true}`), 0644)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.AreSkillsInstalled()
	}
}

// BenchmarkGetLastSetupResult benchmarks retrieving last setup result
func BenchmarkGetLastSetupResult(b *testing.B) {
	tempDir := b.TempDir()
	sm := NewSetupManager(tempDir, tempDir)

	// Create tracking file
	result := &SetupResult{
		PiInstalled:     true,
		LSPInstalled:    true,
		SkillsInstalled: true,
		Timestamp:       time.Now(),
	}
	sm.saveResult(result)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sm.GetLastSetupResult()
		if err != nil {
			b.Fatalf("GetLastSetupResult failed: %v", err)
		}
	}
}

// createTestSkillsForBenchmark creates test skills for benchmarking
func createTestSkillsForBenchmark(b *testing.B, tempDir string) {
	b.Helper()
	skillsDir := filepath.Join(tempDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		b.Fatalf("Failed to create skills dir: %v", err)
	}

	for _, skill := range DefaultSkills {
		skillPath := filepath.Join(skillsDir, skill.Name)
		if err := os.MkdirAll(skillPath, 0755); err != nil {
			b.Fatalf("Failed to create skill dir %s: %v", skill.Name, err)
		}
		skillMDPath := filepath.Join(skillPath, "SKILL.md")
		if err := os.WriteFile(skillMDPath, []byte("# "+skill.Name), 0644); err != nil {
			b.Fatalf("Failed to create SKILL.md for %s: %v", skill.Name, err)
		}
	}
}

// Example_performanceTargets shows the expected timing improvements
func Example_performanceTargets() {
	fmt.Println("Performance Targets:")
	fmt.Println("- Initial setup (no cache): < 30 seconds")
	fmt.Println("- Subsequent runs (cached): < 5 seconds")
	fmt.Println("- LSP checking: < 100ms (parallel)")
	fmt.Println("- Skills verification: < 50ms (parallel)")
	fmt.Println("- BuildSkillFlags: < 10ms (parallel)")
	// Output: Performance Targets:
	// - Initial setup (no cache): < 30 seconds
	// - Subsequent runs (cached): < 5 seconds
	// - LSP checking: < 100ms (parallel)
	// - Skills verification: < 50ms (parallel)
	// - BuildSkillFlags: < 10ms (parallel)
}
