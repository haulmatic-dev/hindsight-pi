package core

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkLoadConfig benchmarks config loading performance
func BenchmarkLoadConfig(b *testing.B) {
	// Create a temporary config file with realistic content
	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "hydra.yaml")

	configContent := `
overseer:
  bead_discovery_interval: 5s
  max_workers: 6
  default_retries: 3
  max_retries: 5
  worker_timeout: 30m
  retry_delay: 5s
  resource_monitoring:
    enabled: true
    max_cpu_percent: 80.0
    max_memory_percent: 85.0
  chain_detection:
    enabled: true
    interval: 10s
    timeout: 10m

projects:
  - name: project-1
    path: /tmp/project1
    type: single
  - name: project-2
    path: /tmp/project2
    type: single
  - name: project-3
    path: /tmp/project3
    type: multi
    repos:
      - name: frontend
        path: ./frontend
      - name: backend
        path: ./backend

mcp:
  enabled: true
  url: ws://127.0.0.1:8765/ws
  port: 8765
  base_url: http://127.0.0.1:8765
  reconnect_interval: 5s
  max_reconnect_attempts: 10
  heartbeat_interval: 30s
  file_reservation_ttl: 30m

api:
  enabled: true
  port: 8080

intelligence:
  cass_memory:
    enabled: true
    query_limit: 10
    history_days: 30
    timeout: 5s
  tldr:
    enabled: true
    base_url: http://127.0.0.1:8787
    timeout: 10s
    max_files: 50
    max_tokens: 100000
  gptcache:
    enabled: true
    server_url: http://127.0.0.1:8788
  settled_state:
    enabled: true
    check_interval: 30s
    inactivity_threshold: 5m
    auto_snapshot: true
  patterns:
    enabled: true
    min_confidence: 0.7
    max_patterns_per_bead: 10
    extract_code_snippets: true
  collective_unconscious:
    enabled: true
    storage:
      backend: cass_memory
      cache_ttl: 5m
    matching:
      enabled: true
      block_on_failure: true
      max_patterns_in_prompt: 3
      min_confidence_threshold: 0.5
      cache_enabled: true
      cache_ttl: 5m
      query_timeout: 5s
    review:
      enabled: true
      auto_approve_threshold: 0.75
      auto_discard_threshold: 0.4
      require_human_review: false
      max_patterns_in_review: 100
      duplicate_threshold: 0.95
      review_timeout: 24h
    feedback:
      track_outcomes: true
      collect_explicit_feedback: true
      decay_rate: 0.01
    security:
      scan_for_secrets: true
      allowed_licenses: ["MIT", "Apache-2.0", "BSD-3-Clause"]
      sanitize_patterns: true

agents:
  enabled: true
  default_agent: pi
  pi:
    enabled: true
    provider: kimi
    model: k2.5
    thinking_level: medium
    timeout: 10m

pi:
  config_dir: ./pi-config
  pi_config_dir: ./pi-config/pi
  skills:
    update_interval: 24h
    auto_update: true
    global: false
    list:
      - name: go-backend
        repo: https://github.com/example/go-backend.git
      - name: react-frontend
        repo: git@github.com:example/react-frontend.git
      - name: devops
        repo: ./skills/devops
  lsp:
    enabled: true
    servers:
      gopls:
        command: gopls
        args: ["serve"]
        install_check: "which gopls"
        install_cmd: "go install golang.org/x/tools/gopls@latest"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		b.Fatalf("Failed to write config file: %v", err)
	}

	// Reset cache before benchmark
	ResetCache()

	b.Run("first_load", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ResetCache()
			_, err := LoadConfig(configPath)
			if err != nil {
				b.Fatalf("LoadConfig failed: %v", err)
			}
		}
	})

	// Pre-load to populate cache
	_, err := LoadConfig(configPath)
	if err != nil {
		b.Fatalf("Initial load failed: %v", err)
	}

	b.Run("cached_load", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := LoadConfig(configPath)
			if err != nil {
				b.Fatalf("LoadConfig failed: %v", err)
			}
		}
	})
}

// BenchmarkValidateConfig benchmarks config validation performance
func BenchmarkValidateConfig(b *testing.B) {
	cfg := createLargeConfig()

	b.Run("typical_config", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			errors := ValidateConfig(cfg)
			if !errors.IsValid() {
				b.Fatalf("Unexpected validation errors: %v", errors)
			}
		}
	})

	b.Run("invalid_config", func(b *testing.B) {
		invalidCfg := cfg.Clone()
		invalidCfg.Pi.Skills.UpdateInterval = "invalid"
		invalidCfg.Pi.Skills.List = append(invalidCfg.Pi.Skills.List, SkillEntry{Name: "", Repo: "not-valid"})

		for i := 0; i < b.N; i++ {
			errors := ValidateConfig(invalidCfg)
			if errors.IsValid() {
				b.Fatal("Expected validation errors but got none")
			}
		}
	})
}

// BenchmarkConfigClone benchmarks config cloning performance
func BenchmarkConfigClone(b *testing.B) {
	cfg := createLargeConfig()

	b.Run("clone_large_config", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = cfg.Clone()
		}
	})
}

// BenchmarkIsValidSkillRepo benchmarks skill repo validation
func BenchmarkIsValidSkillRepo(b *testing.B) {
	testCases := []struct {
		name string
		repo string
	}{
		{"https", "https://github.com/example/skill.git"},
		{"http", "http://github.com/example/skill.git"},
		{"ssh", "git@github.com:example/skill.git"},
		{"local", "./skills/local"},
		{"invalid", "not-a-valid-url"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = isValidSkillRepoFast(tc.repo)
			}
		})
	}
}

// BenchmarkValidationErrors benchmarks error message generation
func BenchmarkValidationErrors(b *testing.B) {
	// Create a config with multiple validation errors
	cfg := DefaultConfig()
	cfg.Overseer.MaxWorkers = -1
	cfg.Overseer.BeadDiscoveryInterval = 0
	cfg.Pi.Skills.UpdateInterval = "invalid"
	cfg.Pi.Skills.List = []SkillEntry{
		{Name: "", Repo: "invalid"},
	}

	b.Run("error_string", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			errors := ValidateConfig(cfg)
			_ = errors.Error()
		}
	})
}

// createLargeConfig creates a config with many projects and skills for benchmarking
func createLargeConfig() *Config {
	cfg := DefaultConfig()

	// Add many projects
	cfg.Projects = make([]ProjectConfig, 20)
	for i := 0; i < 20; i++ {
		cfg.Projects[i] = ProjectConfig{
			Name: fmt.Sprintf("project-%d", i),
			Path: fmt.Sprintf("/tmp/project%d", i),
			Type: "single",
		}
	}

	// Add many skills
	cfg.Pi.Skills.List = make([]SkillEntry, 50)
	for i := 0; i < 50; i++ {
		cfg.Pi.Skills.List[i] = SkillEntry{
			Name: fmt.Sprintf("skill-%d", i),
			Repo: "https://github.com/example/skill.git",
		}
	}

	return cfg
}

// BenchmarkLoadConfigWithSkills benchmarks configs with many skills
func BenchmarkLoadConfigWithSkills(b *testing.B) {
	tmpDir := b.TempDir()

	// Create configs with varying numbers of skills
	skillCounts := []int{0, 10, 50, 100}

	for _, count := range skillCounts {
		configPath := filepath.Join(tmpDir, fmt.Sprintf("hydra_%d.yaml", count))
		content := generateConfigWithSkills(count)
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to write config: %v", err)
		}

		b.Run(fmt.Sprintf("skills_%d", count), func(b *testing.B) {
			ResetCache()
			for i := 0; i < b.N; i++ {
				ResetCache()
				_, err := LoadConfig(configPath)
				if err != nil {
					b.Fatalf("LoadConfig failed: %v", err)
				}
			}
		})
	}
}

// generateConfigWithSkills generates a config with specified number of skills
func generateConfigWithSkills(skillCount int) string {
	base := `
overseer:
  max_workers: 6

projects:
  - name: test-project
    path: /tmp/test
    type: single

pi:
  skills:
    update_interval: 24h
    auto_update: true
    list:
`

	for i := 0; i < skillCount; i++ {
		base += fmt.Sprintf("      - name: skill-%d\n        repo: https://github.com/example/skill-%d.git\n", i, i)
	}

	return base
}

// BenchmarkCachePerformance benchmarks cache hit/miss performance
func BenchmarkCachePerformance(b *testing.B) {
	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "hydra.yaml")

	content := `
overseer:
  max_workers: 6
projects:
  - name: test
    path: /tmp/test
    type: single
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		b.Fatalf("Failed to write config: %v", err)
	}

	// Populate cache
	ResetCache()
	_, err := LoadConfig(configPath)
	if err != nil {
		b.Fatalf("Initial load failed: %v", err)
	}

	b.Run("concurrent_reads", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, err := LoadConfig(configPath)
				if err != nil {
					b.Fatalf("LoadConfig failed: %v", err)
				}
			}
		})
	})
}

// TestLoadConfigPerformance verifies loading time is under target
func TestLoadConfigPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hydra.yaml")

	content := `
overseer:
  max_workers: 6
projects:
  - name: test
    path: /tmp/test
    type: single
pi:
  skills:
    update_interval: 24h
    auto_update: true
    list:
      - name: skill-1
        repo: https://github.com/example/skill.git
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Measure cold load (no cache)
	ResetCache()
	start := time.Now()
	_, err := LoadConfig(configPath)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Target: < 10ms for cold load
	if duration > 10*time.Millisecond {
		t.Errorf("Cold load too slow: %v (target: < 10ms)", duration)
	}
	t.Logf("Cold load duration: %v", duration)

	// Measure cached load
	start = time.Now()
	_, err = LoadConfig(configPath)
	duration = time.Since(start)

	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Target: < 100μs for cached load
	if duration > 100*time.Microsecond {
		t.Errorf("Cached load too slow: %v (target: < 100μs)", duration)
	}
	t.Logf("Cached load duration: %v", duration)
}

// TestValidateConfigPerformance verifies validation time is under target
func TestValidateConfigPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	cfg := createLargeConfig()

	// Warm up
	for i := 0; i < 100; i++ {
		ValidateConfig(cfg)
	}

	// Measure
	iterations := 1000
	start := time.Now()
	for i := 0; i < iterations; i++ {
		errors := ValidateConfig(cfg)
		if !errors.IsValid() {
			t.Fatalf("Unexpected validation errors: %v", errors)
		}
	}
	duration := time.Since(start)
	avgDuration := duration / time.Duration(iterations)

	// Target: < 5ms for typical config
	if avgDuration > 5*time.Millisecond {
		t.Errorf("Validation too slow: %v per iteration (target: < 5ms)", avgDuration)
	}
	t.Logf("Average validation duration: %v", avgDuration)
}

// TestCacheStatistics verifies cache tracking
func TestCacheStatistics(t *testing.T) {
	ResetCache()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hydra.yaml")

	content := `
overseer:
  max_workers: 6
projects:
  - name: test
    path: /tmp/test
    type: single
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// First load - should be a miss
	_, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	hits, misses, _ := globalCache.Stats()
	if hits != 0 {
		t.Errorf("Expected 0 hits after first load, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss after first load, got %d", misses)
	}

	// Second load - should be a hit
	_, err = LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	hits, misses, _ = globalCache.Stats()
	if hits != 1 {
		t.Errorf("Expected 1 hit after second load, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss after second load, got %d", misses)
	}

	// Third load - another hit
	_, err = LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	hits, misses, _ = globalCache.Stats()
	if hits != 2 {
		t.Errorf("Expected 2 hits after third load, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss after third load, got %d", misses)
	}
}
